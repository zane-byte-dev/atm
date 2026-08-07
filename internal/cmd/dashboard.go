package cmd

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/contract"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

type dashboardSession struct {
	ID        string `json:"id"`
	ShortID   string `json:"short_id"`
	Agent     string `json:"agent"`
	Project   string `json:"project"`
	CreatedAt string `json:"created_at"`
	LastAt    string `json:"last_at,omitempty"`
	QCount    int    `json:"q_count"`
	Summary   string `json:"summary,omitempty"`
	FirstQ    string `json:"first_q,omitempty"`
}

type dashboardRange struct {
	// StartDate and EndDate are the window's first and last local calendar days,
	// both inclusive. They are sent rather than left to the app to derive: the app
	// selects day buckets out of the 30-day series, and a calendar window cannot be
	// expressed as "the trailing N entries" — yesterday is one day set back, last
	// week ends on a Sunday. Computing the boundaries twice, in two languages, is
	// how the two sides come to disagree about what "this month" covers.
	StartDate    string                   `json:"start_date"`
	EndDate      string                   `json:"end_date"`
	ModelStats   []store.ModelStatsResult `json:"model_stats"`
	Sessions     []dashboardSession       `json:"sessions"`
	SkillStats   []store.SkillStatsResult `json:"skill_stats"`
	ProjectStats []store.StatsResult      `json:"project_stats"`
	// Speed is range-scoped because percentiles cannot be re-aggregated: the app
	// can sum tokens across buckets but it cannot merge two medians, so the
	// percentiles are computed here, where the individual requests are.
	Speed store.SpeedReport `json:"speed"`
}

type dashboardCurrentSession struct {
	Bound     bool                      `json:"bound"`
	State     string                    `json:"state"`
	SessionID string                    `json:"session_id"`
	Binding   *store.TodoSessionBinding `json:"binding,omitempty"`
	Todo      *compactTodoContext       `json:"todo,omitempty"`
}

type dashboardEnvelope struct {
	SchemaVersion    int                           `json:"schema_version"`
	GeneratedAt      string                        `json:"generated_at"`
	Work             nowView                       `json:"work"`
	Todos            []store.Todo                  `json:"todos"`
	DayStats         []store.DayStatsResult        `json:"day_stats"`
	HourStats        []store.DayStatsResult        `json:"hour_stats"`
	ModelDayStats    []store.ModelDayStatsResult   `json:"model_day_stats"`
	ModelHourStats   []store.ModelDayStatsResult   `json:"model_hour_stats"`
	ProjectDayStats  []store.ProjectDayStatsResult `json:"project_day_stats"`
	ProjectHourStats []store.ProjectDayStatsResult `json:"project_hour_stats"`
	Ranges           map[string]dashboardRange     `json:"ranges"`
	LiveStatus       statusView                    `json:"live_status"`
	CurrentSession   *dashboardCurrentSession      `json:"current_session,omitempty"`
	IndexHealth      syncStatusReport              `json:"index_health"`
}

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Return one versioned desktop dashboard snapshot",
	Args:  cobra.NoArgs,
	RunE:  runDashboard,
}

var dashboardSections []string

func init() {
	dashboardCmd.Flags().StringSliceVar(
		&dashboardSections,
		"sections",
		nil,
		"limit the snapshot to these sections: work, stats (default both)",
	)
	rootCmd.AddCommand(dashboardCmd)
}

// dashboardSectionSet selects which halves of the snapshot to compute.
//
// The split exists because the two halves cost three orders of magnitude apart.
// Everything the task list draws — the todos and the now-view built from them —
// is ready in about 3ms, while the statistics are a second of SQL aggregation
// that no task row reads. Shipping them in one envelope meant the desktop app
// could not paint a task until the charts it was not showing had been computed.
// The app asks for `work` to fill the window, then for the full snapshot.
type dashboardSectionSet struct {
	work  bool
	stats bool
}

func parseDashboardSections(names []string) (dashboardSectionSet, error) {
	if len(names) == 0 {
		return dashboardSectionSet{work: true, stats: true}, nil
	}
	var set dashboardSectionSet
	for _, name := range names {
		switch name {
		case "work":
			set.work = true
		case "stats":
			set.stats = true
		default:
			return set, fmt.Errorf("unknown section %q: want work or stats", name)
		}
	}
	return set, nil
}

// dashboardQueryWorkers bounds how many of the dashboard's queries run at once.
//
// The number is deliberately small and deliberately not runtime.NumCPU(). Every
// concurrent query occupies its own SQLite connection, and each connection keeps
// its own page cache, so overlapping readers over the same large tables decode
// the same pages independently. On a 35MB database over 12 cores that costs more
// than the parallelism returns: measured end to end, the whole command runs
// 1.18s serial, 0.60s at three workers, and 0.89s at twelve — with CPU time
// climbing from 0.96s to 1.19s to 2.45s across those three points. Three is
// where wall time bottoms out; past it the duplicated cache work wins.
const dashboardQueryWorkers = 3

// queryGroup runs independent read-only queries against one handle in parallel.
// Every range and every series is a separate scan that shares nothing with its
// neighbours, so running them one after another only reflected the order they
// happened to be written in.
//
// Tasks must stay flat. A bounded pool deadlocks the moment a running task waits
// on a queued one, and the natural shape here is exactly that trap: a range
// wanting to fan out its own five queries. Ranges therefore submit their leaves
// to this same group rather than nesting a group of their own.
type queryGroup struct {
	slots chan struct{}
	wait  sync.WaitGroup
	mu    sync.Mutex
	err   error
}

func newQueryGroup() *queryGroup {
	return &queryGroup{slots: make(chan struct{}, dashboardQueryWorkers)}
}

// Go submits a leaf query. It must not call Go itself.
func (g *queryGroup) Go(fn func() error) {
	g.wait.Add(1)
	go func() {
		defer g.wait.Done()
		g.slots <- struct{}{}
		defer func() { <-g.slots }()
		if err := fn(); err != nil {
			g.mu.Lock()
			if g.err == nil {
				g.err = err
			}
			g.mu.Unlock()
		}
	}()
}

// Wait blocks until every submitted query finishes and reports the first error.
// It always drains the group: a failing query must not leave its siblings
// writing into result slots the caller has already moved on from.
func (g *queryGroup) Wait() error {
	g.wait.Wait()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}

func runDashboard(cmd *cobra.Command, args []string) error {
	sections, err := parseDashboardSections(dashboardSections)
	if err != nil {
		return err
	}
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	// buildStatusView is bound by `ps` over every process and by parsing each
	// agent's live transcript, so it contends with the aggregation below for
	// almost nothing. Started here, its cost disappears into the SQL instead of
	// being added to it. The channel is buffered so the scan still completes and
	// exits when an error below returns before anyone reads the result.
	type liveOutcome struct {
		view statusView
		err  error
	}
	liveDone := make(chan liveOutcome, 1)
	if sections.work {
		go func() {
			view, err := buildStatusView(agent)
			liveDone <- liveOutcome{view: view, err: err}
		}()
	} else {
		liveDone <- liveOutcome{}
	}

	// An empty file rather than nil: a stats-only snapshot still marshals `work`
	// and `todos`, and they should be empty rather than absent.
	todos := &store.TodoFile{}
	if sections.work {
		todos, err = store.LoadTodosReadOnly()
		if err != nil {
			return err
		}
	}
	var current *dashboardCurrentSession
	if sessionID, _ := resolveSessionID(false); sections.work && sessionID != "" {
		context, err := currentSessionBindingContext(sessionID)
		if err != nil {
			return err
		}
		current = &dashboardCurrentSession{
			Bound:     context != nil && context.State == sessionBindingStateBound,
			State:     sessionBindingStateUnbound,
			SessionID: sessionID,
		}
		if context != nil {
			current.State = context.State
			binding := context.Binding
			current.Binding = &binding
			current.Todo = context.Todo
		}
	}

	var envelope dashboardEnvelope
	err = withDB(true, func(db *sql.DB) error {
		now := time.Now().In(config.Loc)
		var (
			dayStats         []store.DayStatsResult
			hourStats        []store.DayStatsResult
			modelDayStats    []store.ModelDayStatsResult
			modelHourStats   []store.ModelDayStatsResult
			projectDayStats  []store.ProjectDayStatsResult
			projectHourStats []store.ProjectDayStatsResult
			indexHealth      syncStatusReport
		)
		// Ranges are assembled after the fan-out rather than during it, so each
		// window's five queries can be separate tasks instead of one serial task.
		parts := make([]dashboardRangeParts, len(config.MetricsRanges))

		group := newQueryGroup()
		// Index health rides with `work`: it is one row, and it is what tells the
		// user their snapshot is stale, which matters most on the first paint.
		if sections.work {
			group.Go(func() (err error) {
				indexHealth, err = syncStatusReportFromDB(db, store.SyncScopeAll)
				return
			})
		}
		if sections.stats {
			group.Go(func() (err error) {
				dayStats, err = dashboardDayStats(db, now, 30, agent, false)
				return
			})
			group.Go(func() (err error) {
				hourStats, err = dashboardDayStats(db, now, 1, agent, true)
				return
			})
			group.Go(func() (err error) {
				modelDayStats, err = dashboardModelStatsByTime(db, now, 30, agent, false)
				return
			})
			group.Go(func() (err error) {
				modelHourStats, err = dashboardModelStatsByTime(db, now, 1, agent, true)
				return
			})
			group.Go(func() (err error) {
				projectDayStats, err = dashboardProjectStatsByTime(db, now, 30, agent, false)
				return
			})
			group.Go(func() (err error) {
				projectHourStats, err = dashboardProjectStatsByTime(db, now, 1, agent, true)
				return
			})
			for index, name := range config.MetricsRanges {
				submitDashboardRange(group, db, now, name, agent, &parts[index])
			}
		}
		if err := group.Wait(); err != nil {
			return err
		}

		// Keyed by name, not by day count: "last 30 days" and "this month" are
		// different windows and only one of them matches a bill. See
		// config.MetricsRange.
		//
		// Left empty rather than filled with zero-valued windows when stats were
		// not asked for: an absent range reads as "not computed", while a range
		// dated 0001-01-01 reads as a window that genuinely had no traffic.
		ranges := map[string]dashboardRange{}
		if sections.stats {
			for index, name := range config.MetricsRanges {
				ranges[string(name)] = parts[index].build()
			}
		}
		envelope = dashboardEnvelope{
			SchemaVersion:    contract.DashboardSchemaVersion,
			GeneratedAt:      now.Format(time.RFC3339),
			Work:             buildNowView(todos, now),
			Todos:            nonNil(todos.Items),
			DayStats:         nonNil(dayStats),
			HourStats:        nonNil(hourStats),
			ModelDayStats:    nonNil(modelDayStats),
			ModelHourStats:   nonNil(modelHourStats),
			ProjectDayStats:  nonNil(projectDayStats),
			ProjectHourStats: nonNil(projectHourStats),
			Ranges:           ranges,
			CurrentSession:   current,
			IndexHealth:      indexHealth,
		}
		return nil
	})
	if err != nil {
		return err
	}
	live := <-liveDone
	if live.err != nil {
		return live.err
	}
	envelope.LiveStatus = live.view
	output.JSON(envelope)
	return nil
}

func dashboardBounds(now time.Time, days int) (int64, int64) {
	return startOfDayWindow(now, days).Unix(), now.Unix()
}

func dashboardDayStats(db *sql.DB, now time.Time, days int, agent string, hourly bool) ([]store.DayStatsResult, error) {
	start, end := dashboardBounds(now, days)
	if hourly {
		return store.GetHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetDayStats(db, start, end, agent, config.Loc)
}

func dashboardModelStatsByTime(db *sql.DB, now time.Time, days int, agent string, hourly bool) ([]store.ModelDayStatsResult, error) {
	start, end := dashboardBounds(now, days)
	if hourly {
		return store.GetModelHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetModelDayStats(db, start, end, agent, config.Loc)
}

func dashboardProjectStatsByTime(
	db *sql.DB,
	now time.Time,
	days int,
	agent string,
	hourly bool,
) ([]store.ProjectDayStatsResult, error) {
	start, end := dashboardBounds(now, days)
	if hourly {
		return store.GetProjectHourStats(db, start, end, agent, config.Loc)
	}
	return store.GetProjectDayStats(db, start, end, agent, config.Loc)
}

// dashboardRangeParts holds one window's five query results while they are still
// in flight. The queries do not depend on each other, so they are submitted
// individually and only assembled into a dashboardRange once the whole group has
// landed — the alternative, one task per window, would make the slowest window
// (last_30_days) the floor for the entire command.
type dashboardRangeParts struct {
	startTime time.Time
	endTime   time.Time
	models    []store.ModelStatsResult
	skills    []store.SkillStatsResult
	projects  []store.StatsResult
	sessions  []store.ListResult
	speed     store.SpeedReport
}

func submitDashboardRange(
	group *queryGroup,
	db *sql.DB,
	now time.Time,
	name config.MetricsRange,
	agent string,
	into *dashboardRangeParts,
) {
	into.startTime, into.endTime = name.Bounds(now)
	start, end := into.startTime.Unix(), into.endTime.Unix()
	group.Go(func() (err error) {
		into.models, err = store.GetModelStats(db, start, end, agent)
		return
	})
	group.Go(func() (err error) {
		into.skills, err = store.GetSkillStats(db, start, end, agent)
		return
	})
	group.Go(func() (err error) {
		into.projects, err = store.GetStats(db, start, end, agent)
		return
	})
	group.Go(func() (err error) {
		into.sessions, err = store.ListSessions(db, start, end, agent, "")
		return
	})
	group.Go(func() (err error) {
		into.speed, err = store.GetSpeedStats(db, start, end, agent)
		return
	})
}

func (p dashboardRangeParts) build() dashboardRange {
	view := make([]dashboardSession, 0, len(p.sessions))
	for _, session := range p.sessions {
		createdAt := session.CreatedAt
		if session.CreatedTS > 0 {
			createdAt = time.Unix(session.CreatedTS, 0).In(config.Loc).Format(time.RFC3339)
		}
		lastAt := ""
		if session.LastTS > 0 {
			lastAt = time.Unix(session.LastTS, 0).In(config.Loc).Format(time.RFC3339)
		}
		view = append(view, dashboardSession{
			ID:        session.FullID,
			ShortID:   session.ShortID,
			Agent:     session.Agent,
			Project:   session.Project,
			CreatedAt: createdAt,
			LastAt:    lastAt,
			QCount:    session.QCount,
			Summary:   session.Summary,
			FirstQ:    truncLine(cleanMsg(session.FirstQ), 200),
		})
	}
	speed := p.speed
	speed.Models = nonNil(speed.Models)
	speed.Turns = nonNil(speed.Turns)
	return dashboardRange{
		// end is exclusive, so the last included day is the one before it.
		StartDate:    p.startTime.Format("2006-01-02"),
		EndDate:      p.endTime.AddDate(0, 0, -1).Format("2006-01-02"),
		ModelStats:   nonNil(p.models),
		Sessions:     nonNil(view),
		SkillStats:   nonNil(p.skills),
		ProjectStats: nonNil(p.projects),
		Speed:        speed,
	}
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
