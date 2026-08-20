package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

type databaseOpener func(bool) (*sql.DB, error)
type syncProvider func(*sql.DB) (int, error)
type queryProvider func(context.Context, *sql.DB, querySpec) (Result, error)

type querySpec struct {
	group     Group
	startTS   int64
	endTS     int64
	agent     string
	sessionID string
	location  *time.Location
	window    Window
}

// Service owns the complete read transaction: input policy, database mode,
// optional source synchronization, query selection, and report aggregation.
// The function fields are infrastructure ports used by focused tests; callers
// use NewService or Default rather than assembling them themselves.
type Service struct {
	now           func() time.Time
	location      *time.Location
	open          databaseOpener
	sync          syncProvider
	query         queryProvider
	subscriptions func() map[string]float64
}

func NewService() Service {
	return Service{
		now:           time.Now,
		location:      config.Loc,
		open:          openDatabase,
		sync:          store.SyncAll,
		query:         queryStore,
		subscriptions: currentSubscriptions,
	}
}

var Default = NewService()

// Query returns exactly one typed projection selected by Input.Group.
func (service Service) Query(
	ctx context.Context,
	call application.Call,
	input Input,
) (Result, error) {
	if ctx == nil {
		return Result{}, invalid("context", nil, "stats context is required")
	}
	if err := call.Validate(); err != nil {
		return Result{}, err
	}
	group, err := parseGroup(input.Group)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(input.Range) != "" && input.Days != 0 {
		return Result{}, invalid("range", input.Range, "stats range and days are mutually exclusive")
	}
	agent, err := normalizeAgent(input.Agent)
	if err != nil {
		return Result{}, err
	}
	location := service.location
	if location == nil {
		location = config.Loc
	}
	now := time.Now
	if service.now != nil {
		now = service.now
	}
	window, err := buildWindow(now().In(location), input.Days, input.Range, location)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, unavailable("query stats", err)
	}
	if service.open == nil {
		return Result{}, unavailable("open stats database", errors.New("database opener is not configured"))
	}
	db, err := service.open(input.Sync)
	if err != nil {
		return Result{}, unavailable("open stats database", err)
	}
	if db == nil {
		return Result{}, unavailable("open stats database", errors.New("database opener returned nil"))
	}
	defer db.Close()

	syncedFiles := 0
	if input.Sync {
		if service.sync == nil {
			return Result{}, unavailable("sync stats sessions", errors.New("sync provider is not configured"))
		}
		syncedFiles, err = service.sync(db)
		if err != nil {
			return Result{}, unavailable("sync stats sessions", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, unavailable("query stats", err)
	}
	if service.query == nil {
		return Result{}, unavailable("query stats", errors.New("query provider is not configured"))
	}
	result, err := service.query(ctx, db, querySpec{
		group:     group,
		startTS:   window.Start.Unix(),
		endTS:     window.End.Unix(),
		agent:     agent,
		sessionID: strings.TrimSpace(input.SessionID),
		location:  location,
		window:    window,
	})
	if err != nil {
		var appErr *application.Error
		if errors.As(err, &appErr) {
			return Result{}, err
		}
		return Result{}, unavailable("query stats", err)
	}
	result.Group = group
	result.Window = window
	result.SyncedFiles = syncedFiles
	if usesSubscriptionComparison(group) {
		result.Subscription = buildSubscriptionComparison(
			result.Totals.CostUSD,
			window.Days,
			service.subscriptionValues(),
		)
	}
	return result, nil
}

func (service Service) subscriptionValues() map[string]float64 {
	if service.subscriptions == nil {
		return nil
	}
	return service.subscriptions()
}

func openDatabase(syncBeforeRead bool) (*sql.DB, error) {
	if syncBeforeRead {
		return store.Open()
	}
	return store.OpenReadOnly()
}

func currentSubscriptions() map[string]float64 {
	return config.Subscriptions
}

func parseGroup(raw string) (Group, error) {
	group := Group(strings.TrimSpace(raw))
	switch group {
	case GroupProject, GroupModel, GroupModelDay, GroupModelHour, GroupSkill,
		GroupSession, GroupSessionUsage, GroupRequest, GroupSpeed, GroupDay,
		GroupHour, GroupWrapped:
		return group, nil
	default:
		return "", invalid(
			"group",
			raw,
			fmt.Sprintf("unknown stats group %q (use model, model-day, model-hour, skill, session, session-usage, request, speed, day, hour, or wrapped)", raw),
		)
	}
}

func normalizeAgent(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	agent := config.NormalizeAgent(raw)
	if agent == "" {
		return "", invalid(
			"agent",
			raw,
			fmt.Sprintf("unknown agent: %s (use claude, codex, pi, copilot, qoder, qodercli, qoderwork, grokbuild, or antigravity)", raw),
		)
	}
	return agent, nil
}

func buildWindow(now time.Time, days int, rawRange string, location *time.Location) (Window, error) {
	if days < 1 {
		days = 1
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location).
		AddDate(0, 0, -(days - 1))
	end := now
	label := "today"
	if days > 1 {
		label = fmt.Sprintf("last %d days", days)
	}
	if strings.TrimSpace(rawRange) != "" {
		namedRange, err := config.ParseMetricsRange(rawRange)
		if err != nil {
			return Window{}, invalid("range", rawRange, err.Error())
		}
		start, end = namedRange.Bounds(now)
		days = namedRange.Days(now)
		label = string(namedRange)
	}
	return Window{Start: start, End: end, Label: label, Days: days}, nil
}

func invalid(field string, value any, message string) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func unavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action+" failed", cause)
	err.Retryable = true
	return err
}

func usesSubscriptionComparison(group Group) bool {
	switch group {
	case GroupProject, GroupModel, GroupSession, GroupDay, GroupHour, GroupWrapped:
		return true
	default:
		return false
	}
}

func buildSubscriptionComparison(totalCost float64, days int, subscriptions map[string]float64) *SubscriptionComparison {
	if totalCost == 0 || days < 1 || len(subscriptions) == 0 {
		return nil
	}
	names := make([]string, 0, len(subscriptions))
	for name := range subscriptions {
		names = append(names, name)
	}
	sort.Strings(names)
	comparison := &SubscriptionComparison{Plans: make([]SubscriptionPlan, 0, len(names))}
	for _, name := range names {
		monthly := subscriptions[name]
		comparison.SubscriptionMonthlyUSD += monthly
		comparison.Plans = append(comparison.Plans, SubscriptionPlan{Name: name, MonthlyUSD: monthly})
	}
	if comparison.SubscriptionMonthlyUSD == 0 {
		return nil
	}
	comparison.APIEquivalentMonthlyUSD = totalCost / float64(days) * 30
	comparison.ValueRatio = comparison.APIEquivalentMonthlyUSD / comparison.SubscriptionMonthlyUSD
	return comparison
}

func queryStore(ctx context.Context, db *sql.DB, spec querySpec) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	switch spec.group {
	case GroupProject:
		rows, err := store.GetStats(db, spec.startTS, spec.endTS, spec.agent)
		return projectResult(rows), err
	case GroupModel:
		rows, err := store.GetModelStats(db, spec.startTS, spec.endTS, spec.agent)
		return modelResult(rows), err
	case GroupModelDay:
		rows, err := store.GetModelDayStats(db, spec.startTS, spec.endTS, spec.agent, spec.location)
		return modelPeriodResult(rows), err
	case GroupModelHour:
		rows, err := store.GetModelHourStats(db, spec.startTS, spec.endTS, spec.agent, spec.location)
		return modelPeriodResult(rows), err
	case GroupSkill:
		rows, err := store.GetSkillStats(db, spec.startTS, spec.endTS, spec.agent)
		return skillResult(rows), err
	case GroupSession:
		rows, err := store.GetSessionStats(db, spec.startTS, spec.endTS, spec.agent)
		return sessionResult(rows), err
	case GroupSessionUsage:
		rows, err := store.GetSessionUsageStats(db, spec.startTS, spec.endTS, spec.agent)
		return sessionUsageResult(rows), err
	case GroupRequest:
		rows, err := store.GetRequestStats(db, spec.startTS, spec.endTS, spec.agent, spec.sessionID)
		return requestResult(rows), err
	case GroupSpeed:
		report, err := store.GetSpeedStats(db, spec.startTS, spec.endTS, spec.agent)
		return speedResult(report), err
	case GroupDay:
		rows, err := store.GetDayStats(db, spec.startTS, spec.endTS, spec.agent, spec.location)
		return periodResult(rows), err
	case GroupHour:
		rows, err := store.GetHourStats(db, spec.startTS, spec.endTS, spec.agent, spec.location)
		return periodResult(rows), err
	case GroupWrapped:
		return queryWrapped(db, spec)
	default:
		return Result{}, invalid("group", spec.group, "unknown stats group")
	}
}

func projectResult(rows []store.StatsResult) Result {
	result := Result{Projects: make([]ProjectRow, 0, len(rows))}
	for _, row := range rows {
		result.Projects = append(result.Projects, ProjectRow{
			Project: row.Project, Agent: row.Agent, Sessions: row.Sessions,
			Queries: row.Queries, ToolCalls: row.ToolCalls, InputTokens: row.InputTokens,
			OutputTokens: row.OutputTokens, CacheReadTokens: row.CacheReadTokens, CostUSD: row.CostUSD,
		})
		result.Totals.Sessions += row.Sessions
		result.Totals.Queries += row.Queries
		result.Totals.ToolCalls += row.ToolCalls
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheReadTokens
		result.Totals.CostUSD += row.CostUSD
	}
	return result
}

func modelResult(rows []store.ModelStatsResult) Result {
	result := Result{Models: make([]ModelRow, 0, len(rows))}
	for _, row := range rows {
		result.Models = append(result.Models, ModelRow{
			Client: row.Client, Model: row.Model, Sessions: row.Sessions,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CostUSD: row.CostUSD,
			CostEstimated: row.CostEstimated, PricingSource: string(row.PricingSource),
		})
		result.Totals.Sessions += row.Sessions
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheReadTokens
		result.Totals.CostUSD += row.CostUSD
		if row.CostEstimated {
			result.Totals.AnyEstimated = true
			result.Totals.EstimatedCostUSD += row.CostUSD
		}
	}
	return result
}

func modelPeriodResult(rows []store.ModelDayStatsResult) Result {
	result := Result{ModelPeriods: make([]ModelPeriodRow, 0, len(rows))}
	for _, row := range rows {
		result.ModelPeriods = append(result.ModelPeriods, ModelPeriodRow{
			Date: row.Date, Client: row.Client, Model: row.Model, Sessions: row.Sessions,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CostUSD: row.CostUSD,
			CostEstimated: row.CostEstimated, PricingSource: string(row.PricingSource),
			MeasuredOutputTokens: row.MeasuredOutputTokens, MeasuredDurationMS: row.MeasuredDurationMS,
		})
		result.Totals.Sessions += row.Sessions
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheReadTokens
		result.Totals.CostUSD += row.CostUSD
		if row.CostEstimated {
			result.Totals.AnyEstimated = true
			result.Totals.EstimatedCostUSD += row.CostUSD
		}
	}
	return result
}

func skillResult(rows []store.SkillStatsResult) Result {
	result := Result{Skills: make([]SkillRow, 0, len(rows))}
	for _, row := range rows {
		result.Skills = append(result.Skills, SkillRow{
			Skill: row.Skill, Calls: row.Calls, Sessions: row.Sessions, Agents: row.Agents,
		})
	}
	return result
}

func sessionResult(rows []store.SessionStatsResult) Result {
	result := Result{Sessions: make([]SessionRow, 0, len(rows))}
	var tokenTotal int64
	for _, row := range rows {
		tokenTotal += row.InputTokens + row.OutputTokens + row.CacheTokens
		result.Totals.Queries += row.Queries
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheTokens
		result.Totals.CostUSD += row.CostUSD
		result.Sessions = append(result.Sessions, SessionRow{
			ShortID: row.ShortID, Project: row.Project, Model: row.Model, Queries: row.Queries,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheTokens: row.CacheTokens, CostUSD: row.CostUSD,
		})
	}
	if tokenTotal > 0 {
		for index := range result.Sessions {
			row := &result.Sessions[index]
			row.Share = float64(row.InputTokens+row.OutputTokens+row.CacheTokens) / float64(tokenTotal)
		}
	}
	return result
}

func sessionUsageResult(rows []store.SessionUsageStatsResult) Result {
	result := Result{SessionUsage: make([]SessionUsageRow, 0, len(rows))}
	for _, row := range rows {
		result.SessionUsage = append(result.SessionUsage, SessionUsageRow{
			SessionID: row.SessionID, ShortID: row.ShortID, Agent: row.Agent,
			Project: row.Project, Model: row.Model, StartedTS: row.StartedTS, LastTS: row.LastTS,
			Requests: row.Requests, InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheCreateTokens: row.CacheCreateTokens, CacheReadTokens: row.CacheReadTokens,
			TotalTokens: row.TotalTokens, CostUSD: row.CostUSD, Share: row.Share,
		})
		result.Totals.Requests += row.Requests
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheCreateTokens + row.CacheReadTokens
		result.Totals.CostUSD += row.CostUSD
	}
	return result
}

func requestResult(rows []store.RequestStatsResult) Result {
	result := Result{Requests: make([]RequestRow, 0, len(rows))}
	for _, row := range rows {
		count := row.RequestCount
		if count < 1 {
			count = 1
		}
		result.Requests = append(result.Requests, RequestRow{
			SessionID: row.SessionID, Agent: row.Agent, Project: row.Project, Model: row.Model,
			TS: row.TS, RequestCount: count, InputTokens: row.InputTokens,
			OutputTokens: row.OutputTokens, CacheTokens: row.CacheTokens, CostUSD: row.CostUSD,
		})
		result.Totals.Requests += count
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheTokens
		result.Totals.CostUSD += row.CostUSD
	}
	return result
}

func speedResult(report store.SpeedReport) Result {
	result := Result{Speed: SpeedReport{Untimed: report.Untimed, OutOfWindow: report.OutOfWindow}}
	if report.Models != nil {
		result.Speed.Models = make([]SpeedModelRow, 0, len(report.Models))
	}
	for _, row := range report.Models {
		result.Speed.Models = append(result.Speed.Models, SpeedModelRow{
			Client: row.Client, Model: row.Model, Requests: row.Requests, Sampled: row.Sampled,
			TokensPerSecondP50: row.TokensPerSecondP50, TokensPerSecondP90: row.TokensPerSecondP90,
			TokensPerSecondWeighted: row.TokensPerSecondWeighted,
			DurationP50Seconds:      row.DurationP50Seconds, DurationP90Seconds: row.DurationP90Seconds,
			OutputTokens: row.OutputTokens, SampledSeconds: row.SampledSeconds,
		})
	}
	if report.Turns != nil {
		result.Speed.Turns = make([]TurnWaitRow, 0, len(report.Turns))
	}
	for _, row := range report.Turns {
		result.Speed.Turns = append(result.Speed.Turns, TurnWaitRow{
			Agent: row.Agent, Turns: row.Turns, WaitP50Seconds: row.WaitP50Seconds,
			WaitP90Seconds: row.WaitP90Seconds, WaitMaxSeconds: row.WaitMaxSeconds,
			RequestsPerTurn: row.RequestsPerTurn,
		})
	}
	return result
}

func periodResult(rows []store.DayStatsResult) Result {
	result := Result{Periods: make([]PeriodRow, 0, len(rows))}
	for _, row := range rows {
		result.Periods = append(result.Periods, PeriodRow{
			Date: row.Date, Sessions: row.Sessions, Queries: row.Queries,
			InputTokens: row.InputTokens, OutputTokens: row.OutputTokens,
			CacheReadTokens: row.CacheReadTokens, CostUSD: row.CostUSD,
		})
		result.Totals.Sessions += row.Sessions
		result.Totals.Queries += row.Queries
		result.Totals.InputTokens += row.InputTokens
		result.Totals.OutputTokens += row.OutputTokens
		result.Totals.CacheTokens += row.CacheReadTokens
		result.Totals.CostUSD += row.CostUSD
		if row.CostUSD > result.Totals.MaxCostUSD {
			result.Totals.MaxCostUSD = row.CostUSD
		}
	}
	return result
}

func queryWrapped(db *sql.DB, spec querySpec) (Result, error) {
	projects, err := store.GetStats(db, spec.startTS, spec.endTS, spec.agent)
	if err != nil {
		return Result{}, err
	}
	models, err := store.GetModelStats(db, spec.startTS, spec.endTS, spec.agent)
	if err != nil {
		return Result{}, err
	}
	periods, err := store.GetDayStats(db, spec.startTS, spec.endTS, spec.agent, spec.location)
	if err != nil {
		return Result{}, err
	}
	result := projectResult(projects)
	if len(projects) == 0 {
		return result, nil
	}
	wrapped := &Wrapped{
		Period: spec.window.Label, Days: spec.window.Days,
		Sessions: result.Totals.Sessions, Queries: result.Totals.Queries,
		ToolCalls: result.Totals.ToolCalls, InputTokens: result.Totals.InputTokens,
		OutputTokens: result.Totals.OutputTokens, CostUSD: result.Totals.CostUSD,
	}
	if len(models) > 0 {
		wrapped.TopModel = models[0].Model
		if models[0].Client != "" {
			wrapped.TopModel += " · " + models[0].Client
		}
	}
	projectCosts := make(map[string]float64, len(projects))
	projectOrder := make([]string, 0, len(projects))
	for _, row := range projects {
		if _, exists := projectCosts[row.Project]; !exists {
			projectOrder = append(projectOrder, row.Project)
		}
		projectCosts[row.Project] += row.CostUSD
	}
	for _, project := range projectOrder {
		if cost := projectCosts[project]; cost > wrapped.TopProjectCost {
			wrapped.TopProject = project
			wrapped.TopProjectCost = cost
		}
	}
	for _, row := range periods {
		if row.Sessions > 0 {
			wrapped.ActiveDays++
		}
		if wrapped.PeakDay == "" || row.CostUSD > wrapped.PeakCost {
			wrapped.PeakDay = row.Date
			wrapped.PeakCost = row.CostUSD
		}
	}
	result.Wrapped = wrapped
	return result, nil
}
