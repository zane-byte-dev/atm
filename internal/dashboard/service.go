package dashboard

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
	workapp "github.com/zane-byte-dev/atm/internal/work"
)

const (
	queryWorkers = 3
	hourlyDays   = 2
)

type sectionSet struct {
	work  bool
	stats bool
}

// LiveStatusProvider isolates process and transcript discovery from the
// dashboard use case. The service owns when it runs and how it joins the result;
// the adapter owns the operating-system-specific discovery implementation.
type LiveStatusProvider func(context.Context, string) (LiveStatus, error)

type queryProvider func(context.Context, time.Time, string, sectionSet, bool) (queryResult, error)
type todoProvider func() (*store.TodoFile, error)
type currentProvider func(context.Context, application.Call, string) (*CurrentSession, error)

// Service orchestrates one coherent dashboard read. Its dependencies are ports
// rather than Cobra callbacks, which keeps validation, concurrency and failure
// classification identical for CLI and IPC.
type Service struct {
	now         func() time.Time
	live        LiveStatusProvider
	loadTodos   todoProvider
	loadCurrent currentProvider
	query       queryProvider
}

func NewService(live LiveStatusProvider) Service {
	return Service{
		now:         time.Now,
		live:        live,
		loadTodos:   store.LoadTodosReadOnly,
		loadCurrent: loadCurrentSession,
		query:       queryStore,
	}
}

// BuildSnapshot returns one versioned aggregate for the requested sections.
func (service Service) BuildSnapshot(
	ctx context.Context,
	call application.Call,
	input Request,
) (Snapshot, error) {
	if ctx == nil {
		err := application.NewError(application.CodeInvalidArgument, "dashboard context is required")
		err.Details = map[string]any{"field": "context"}
		return Snapshot{}, err
	}
	if err := call.Validate(); err != nil {
		return Snapshot{}, err
	}
	sections, err := parseSections(input.Sections)
	if err != nil {
		return Snapshot{}, err
	}
	agent := strings.TrimSpace(input.Agent)
	if agent != "" {
		agent = config.NormalizeAgent(agent)
		if agent == "" {
			appErr := application.NewError(application.CodeInvalidArgument, "unknown dashboard agent")
			appErr.Details = map[string]any{"field": "agent"}
			return Snapshot{}, appErr
		}
	}
	now := service.now().In(config.Loc)

	type liveOutcome struct {
		value LiveStatus
		err   error
	}
	liveDone := make(chan liveOutcome, 1)
	if sections.work {
		if service.live == nil {
			return Snapshot{}, unavailable("load live status", errors.New("live status provider is not configured"))
		}
		go func() {
			value, liveErr := service.live(ctx, agent)
			liveDone <- liveOutcome{value: value, err: liveErr}
		}()
	} else {
		liveDone <- liveOutcome{}
	}

	todos := &store.TodoFile{}
	if sections.work {
		todos, err = service.loadTodos()
		if err != nil {
			return Snapshot{}, unavailable("load dashboard work", err)
		}
	}
	var current *CurrentSession
	if sections.work && strings.TrimSpace(input.SessionID) != "" {
		current, err = service.loadCurrent(ctx, call, strings.TrimSpace(input.SessionID))
		if err != nil {
			return Snapshot{}, preserveApplicationError("load current session", err)
		}
	}

	queried, err := service.query(ctx, now, agent, sections, input.Sync)
	if err != nil {
		return Snapshot{}, preserveApplicationError("query dashboard", err)
	}
	live := <-liveDone
	if live.err != nil {
		return Snapshot{}, unavailable("load live status", live.err)
	}
	live.value.Sessions = nonNil(live.value.Sessions)
	live.value.Bindings = nonNil(live.value.Bindings)
	if queried.ranges == nil {
		queried.ranges = map[string]Range{}
	}

	return Snapshot{
		SchemaVersion:    contract.DashboardSchemaVersion,
		GeneratedAt:      now.Format(time.RFC3339),
		Work:             buildWork(todos, now),
		Todos:            nonNil(todos.Items),
		DayStats:         nonNil(queried.dayStats),
		HourStats:        nonNil(queried.hourStats),
		ModelDayStats:    nonNil(queried.modelDayStats),
		ModelHourStats:   nonNil(queried.modelHourStats),
		ProjectDayStats:  nonNil(queried.projectDayStats),
		ProjectHourStats: nonNil(queried.projectHourStats),
		TodoCompletions:  nonNil(queried.todoCompletions),
		Ranges:           queried.ranges,
		LiveStatus:       live.value,
		CurrentSession:   current,
		IndexHealth:      queried.indexHealth,
	}, nil
}

func parseSections(names []string) (sectionSet, error) {
	if len(names) == 0 {
		return sectionSet{work: true, stats: true}, nil
	}
	var selected sectionSet
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		switch name {
		case "work":
			selected.work = true
		case "stats":
			selected.stats = true
		default:
			err := application.NewError(application.CodeInvalidArgument, "unknown dashboard section")
			err.Details = map[string]any{"field": "sections", "value": raw, "allowed": []string{"work", "stats"}}
			return selected, err
		}
	}
	return selected, nil
}

func loadCurrentSession(
	ctx context.Context,
	call application.Call,
	sessionID string,
) (*CurrentSession, error) {
	if sessionID == "" {
		return nil, nil
	}
	call.Actor.SessionID = sessionID
	result, err := workapp.Default.Current(ctx, call, workapp.CurrentInput{})
	if err != nil {
		return nil, err
	}
	current := &CurrentSession{
		Bound:     result.Bound,
		State:     string(result.State),
		SessionID: result.SessionID,
	}
	if result.Context != nil {
		binding := result.Context.Binding
		current.Binding = &binding
		current.Todo = result.Context.Todo
	}
	return current, nil
}

func preserveApplicationError(action string, err error) error {
	var appErr *application.Error
	if errors.As(err, &appErr) {
		return err
	}
	return unavailable(action, err)
}

func unavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action+" failed", cause)
	err.Retryable = true
	return err
}

type queryResult struct {
	dayStats         []store.DayStatsResult
	hourStats        []store.DayStatsResult
	modelDayStats    []store.ModelDayStatsResult
	modelHourStats   []store.ModelDayStatsResult
	projectDayStats  []store.ProjectDayStatsResult
	projectHourStats []store.ProjectDayStatsResult
	todoCompletions  []store.TodoCompletion
	ranges           map[string]Range
	indexHealth      IndexHealth
}

func queryStore(
	ctx context.Context,
	now time.Time,
	agent string,
	sections sectionSet,
	syncBeforeRead bool,
) (queryResult, error) {
	if err := ctx.Err(); err != nil {
		return queryResult{}, err
	}
	db, err := openDB(syncBeforeRead)
	if err != nil {
		return queryResult{}, unavailable("open dashboard database", err)
	}
	defer db.Close()
	if syncBeforeRead {
		if _, err := store.SyncAll(db); err != nil {
			return queryResult{}, unavailable("sync dashboard sessions", err)
		}
	}

	result := queryResult{ranges: map[string]Range{}}
	parts := make([]rangeParts, len(config.MetricsRanges))
	group := newQueryGroup()
	if sections.work {
		group.Go(func() (queryErr error) {
			result.indexHealth, queryErr = readIndexHealth(db)
			return
		})
	}
	if sections.stats {
		group.Go(func() (queryErr error) {
			result.dayStats, queryErr = dayStats(db, now, 30, agent, false)
			return
		})
		group.Go(func() (queryErr error) {
			result.hourStats, queryErr = dayStats(db, now, hourlyDays, agent, true)
			return
		})
		group.Go(func() (queryErr error) {
			result.modelDayStats, queryErr = modelStatsByTime(db, now, 30, agent, false)
			return
		})
		group.Go(func() (queryErr error) {
			result.modelHourStats, queryErr = modelStatsByTime(db, now, hourlyDays, agent, true)
			return
		})
		group.Go(func() (queryErr error) {
			result.projectDayStats, queryErr = projectStatsByTime(db, now, 30, agent, false)
			return
		})
		group.Go(func() (queryErr error) {
			result.projectHourStats, queryErr = projectStatsByTime(db, now, hourlyDays, agent, true)
			return
		})
		group.Go(func() (queryErr error) {
			result.todoCompletions, queryErr = store.GetTodoCompletions(db)
			return
		})
		for index, name := range config.MetricsRanges {
			submitRange(group, db, now, name, agent, &parts[index])
		}
	}
	if err := group.Wait(); err != nil {
		return queryResult{}, unavailable("read dashboard data", err)
	}
	if sections.stats {
		for index, name := range config.MetricsRanges {
			result.ranges[string(name)] = parts[index].build()
		}
	}
	return result, nil
}

func openDB(syncBeforeRead bool) (*sql.DB, error) {
	if syncBeforeRead {
		return store.Open()
	}
	return store.OpenReadOnly()
}

func readIndexHealth(db *sql.DB) (IndexHealth, error) {
	health, err := store.ReadSyncHealth(db, store.SyncScopeAll, time.Now(), store.DefaultSyncStaleAfter)
	if err != nil {
		return IndexHealth{}, err
	}
	return IndexHealth{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Index: Index{
			Path:             config.AtmDB,
			Exists:           true,
			SchemaVersion:    health.SchemaVersion,
			IndexedSessions:  health.IndexedSessions,
			RetainedSessions: health.RetainedSessions,
		},
		Sync: SyncState{
			Scope:             health.Scope,
			Status:            health.Status,
			RunStatus:         health.RunStatus,
			LastAttemptAt:     health.LastAttemptAt,
			LastSuccessAt:     health.LastSuccessAt,
			AgeSeconds:        health.AgeSeconds,
			StaleAfterSeconds: health.StaleAfterSeconds,
			LastError:         health.LastError,
			LastSyncedFiles:   health.LastSyncedFiles,
		},
	}, nil
}

type queryGroup struct {
	slots chan struct{}
	wait  sync.WaitGroup
	mu    sync.Mutex
	err   error
}

func newQueryGroup() *queryGroup {
	return &queryGroup{slots: make(chan struct{}, queryWorkers)}
}

func (group *queryGroup) Go(fn func() error) {
	group.wait.Add(1)
	go func() {
		defer group.wait.Done()
		group.slots <- struct{}{}
		defer func() { <-group.slots }()
		if err := fn(); err != nil {
			group.mu.Lock()
			if group.err == nil {
				group.err = err
			}
			group.mu.Unlock()
		}
	}()
}

func (group *queryGroup) Wait() error {
	group.wait.Wait()
	group.mu.Lock()
	defer group.mu.Unlock()
	return group.err
}

func bounds(now time.Time, days int) (int64, int64) {
	if days < 1 {
		days = 1
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, config.Loc).
		AddDate(0, 0, -(days - 1))
	return start.Unix(), now.Unix()
}

func dayStats(db *sql.DB, now time.Time, days int, agent string, hourly bool) ([]store.DayStatsResult, error) {
	start, end := bounds(now, days)
	if hourly {
		return store.GetHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetDayStats(db, start, end, agent, config.Loc)
}

func modelStatsByTime(db *sql.DB, now time.Time, days int, agent string, hourly bool) ([]store.ModelDayStatsResult, error) {
	start, end := bounds(now, days)
	if hourly {
		return store.GetModelHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetModelDayStats(db, start, end, agent, config.Loc)
}

func projectStatsByTime(db *sql.DB, now time.Time, days int, agent string, hourly bool) ([]store.ProjectDayStatsResult, error) {
	start, end := bounds(now, days)
	if hourly {
		return store.GetProjectHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetProjectDayStats(db, start, end, agent, config.Loc)
}

type rangeParts struct {
	startTime time.Time
	endTime   time.Time
	models    []store.ModelStatsResult
	skills    []store.SkillStatsResult
	projects  []store.StatsResult
	sessions  []store.ListResult
	speed     store.SpeedReport
}

func submitRange(group *queryGroup, db *sql.DB, now time.Time, name config.MetricsRange, agent string, into *rangeParts) {
	into.startTime, into.endTime = name.Bounds(now)
	start, end := into.startTime.Unix(), into.endTime.Unix()
	group.Go(func() (err error) { into.models, err = store.GetModelStats(db, start, end, agent); return })
	group.Go(func() (err error) { into.skills, err = store.GetSkillStats(db, start, end, agent); return })
	group.Go(func() (err error) { into.projects, err = store.GetStats(db, start, end, agent); return })
	group.Go(func() (err error) { into.sessions, err = store.ListSessions(db, start, end, agent, ""); return })
	group.Go(func() (err error) { into.speed, err = store.GetSpeedStats(db, start, end, agent); return })
}

func (parts rangeParts) build() Range {
	sessions := make([]RangeSession, 0, len(parts.sessions))
	for _, session := range parts.sessions {
		createdAt := session.CreatedAt
		if session.CreatedTS > 0 {
			createdAt = time.Unix(session.CreatedTS, 0).In(config.Loc).Format(time.RFC3339)
		}
		lastAt := ""
		if session.LastTS > 0 {
			lastAt = time.Unix(session.LastTS, 0).In(config.Loc).Format(time.RFC3339)
		}
		sessions = append(sessions, RangeSession{
			ID: session.FullID, ShortID: session.ShortID, Agent: session.Agent,
			Project: session.Project, CreatedAt: createdAt, LastAt: lastAt,
			QCount: session.QCount, LocalUserTurnCount: session.QCount, Summary: session.Summary,
			FirstQ:   truncateLine(parser.VisibleUserText(session.FirstQ), 200),
			ResumeID: session.ResumeID, RootSessionID: session.RootSessionID,
			ParentSessionID: session.ParentSessionID, AgentPath: session.AgentPath,
			AgentNickname: session.AgentNickname, SubagentDepth: session.SubagentDepth,
			IsSubagent: session.IsSubagent, ParserVersion: session.ParserVersion,
			ContentState: session.ContentState, ResultStatus: session.ResultStatus,
			LatestProgress: session.LatestProgress, FinalResult: session.FinalResult,
		})
	}
	speed := parts.speed
	speed.Models = nonNil(speed.Models)
	speed.Turns = nonNil(speed.Turns)
	quality := buildStatsQuality(parts.projects, parts.models, speed)
	return Range{
		StartDate:  parts.startTime.Format("2006-01-02"),
		EndDate:    parts.endTime.AddDate(0, 0, -1).Format("2006-01-02"),
		ModelStats: nonNil(parts.models), Sessions: nonNil(sessions),
		SkillStats: nonNil(parts.skills), ProjectStats: nonNil(parts.projects), Speed: speed,
		Quality: quality,
	}
}

func buildStatsQuality(projects []store.StatsResult, models []store.ModelStatsResult, speed store.SpeedReport) StatsQuality {
	quality := StatsQuality{PricingSources: []string{}}
	activeAgents := map[string]struct{}{}
	tokenAgents := map[string]struct{}{}
	for _, row := range projects {
		quality.ActiveSessions += row.Sessions
		quality.TokenSessions += row.TokenSessions
		quality.Requests += row.Requests
		quality.DetailedRequests += row.DetailedRequests
		quality.AggregateRequests += row.AggregateRequests
		quality.CostUSD += row.CostUSD
		activeAgents[row.Agent] = struct{}{}
		if row.TokenSessions > 0 {
			tokenAgents[row.Agent] = struct{}{}
		}
	}
	quality.ActiveAgents = len(activeAgents)
	quality.TokenAgents = len(tokenAgents)
	if quality.ActiveSessions > 0 {
		quality.SessionCoveragePct = float64(quality.TokenSessions) / float64(quality.ActiveSessions) * 100
	}
	if quality.ActiveAgents > 0 {
		quality.AgentCoveragePct = float64(quality.TokenAgents) / float64(quality.ActiveAgents) * 100
	}
	if quality.Requests > 0 {
		quality.RequestCoveragePct = float64(quality.DetailedRequests) / float64(quality.Requests) * 100
	}
	sources := map[string]struct{}{}
	for _, row := range models {
		source := string(row.PricingSource)
		if source != "" {
			sources[source] = struct{}{}
		}
		if row.CostEstimated {
			quality.EstimatedCostUSD += row.CostUSD
		}
	}
	for source := range sources {
		quality.PricingSources = append(quality.PricingSources, source)
	}
	sort.Strings(quality.PricingSources)
	if quality.CostUSD > 0 {
		quality.EstimatedCostShare = quality.EstimatedCostUSD / quality.CostUSD
	}
	for _, row := range speed.Models {
		quality.SpeedRequests += row.Requests
		quality.SpeedSampledRequests += row.Sampled
	}
	quality.UntimedRequests = speed.Untimed
	quality.OutOfWindowRequests = speed.OutOfWindow
	if quality.SpeedRequests > 0 {
		quality.SpeedSamplePct = float64(quality.SpeedSampledRequests) / float64(quality.SpeedRequests) * 100
	}
	return quality
}

func buildWork(file *store.TodoFile, now time.Time) WorkView {
	today := now.Format("2006-01-02")
	view := WorkView{
		GeneratedAt: now.Format(time.RFC3339), Open: []store.Todo{}, Working: []store.Todo{},
		Waiting: []store.Todo{}, Review: []store.Todo{}, Blocked: []store.Todo{}, Due: []store.Todo{},
	}
	for _, todo := range file.Items {
		if !store.TodoIsActive(todo) {
			continue
		}
		switch todo.Status {
		case store.TodoStatusInProgress:
			view.Working = append(view.Working, todo)
			if todo.ReviewAt != "" && todo.ReviewAt <= today {
				view.Due = append(view.Due, todo)
			} else if strings.TrimSpace(todo.WakeCondition) != "" || todo.ReviewAt != "" {
				view.Waiting = append(view.Waiting, todo)
			}
		case store.TodoStatusReview:
			view.Review = append(view.Review, todo)
		default:
			view.Open = append(view.Open, todo)
		}
	}
	for _, todos := range [][]store.Todo{view.Working, view.Review, view.Blocked, view.Due, view.Waiting, view.Open} {
		sortWork(todos)
	}
	maintenance := 0
	for _, todo := range file.Items {
		if store.TodoIsActive(todo) && store.TodoHasTag(todo, store.TodoTagMaintenance) {
			maintenance++
		}
	}
	view.Summary = WorkSummary{
		Open: len(view.Open), InProgress: len(view.Working), Waiting: len(view.Waiting),
		Review: len(view.Review), Blocked: 0, Due: len(view.Due), Maintenance: maintenance,
	}
	return view
}

func sortWork(todos []store.Todo) {
	rank := map[string]int{"P0": 0, "P1": 1, "P2": 2}
	sort.SliceStable(todos, func(i, j int) bool {
		left, ok := rank[todos[i].Priority]
		if !ok {
			left = 99
		}
		right, ok := rank[todos[j].Priority]
		if !ok {
			right = 99
		}
		if left != right {
			return left < right
		}
		if todos[i].Created != todos[j].Created {
			return todos[i].Created < todos[j].Created
		}
		return todos[i].ID < todos[j].ID
	})
}

func truncateLine(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= max {
		return value
	}
	return string([]rune(value)[:max]) + "..."
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
