package cmd

import (
	"database/sql"
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

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard(cmd *cobra.Command, args []string) error {
	agent, err := resolveAgent()
	if err != nil {
		return err
	}
	todos, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	live, err := buildStatusView(agent)
	if err != nil {
		return err
	}
	var current *dashboardCurrentSession
	if sessionID, _ := resolveSessionID(false); sessionID != "" {
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
		dayStats, err := dashboardDayStats(db, now, 30, agent, false)
		if err != nil {
			return err
		}
		hourStats, err := dashboardDayStats(db, now, 1, agent, true)
		if err != nil {
			return err
		}
		modelDayStats, err := dashboardModelStatsByTime(db, now, 30, agent, false)
		if err != nil {
			return err
		}
		modelHourStats, err := dashboardModelStatsByTime(db, now, 1, agent, true)
		if err != nil {
			return err
		}
		projectDayStats, err := dashboardProjectStatsByTime(db, now, 30, agent, false)
		if err != nil {
			return err
		}
		projectHourStats, err := dashboardProjectStatsByTime(db, now, 1, agent, true)
		if err != nil {
			return err
		}
		// Keyed by name, not by day count: "last 30 days" and "this month" are
		// different windows and only one of them matches a bill. See
		// config.MetricsRange.
		ranges := map[string]dashboardRange{}
		for _, name := range config.MetricsRanges {
			value, err := dashboardRangeFor(db, now, name, agent)
			if err != nil {
				return err
			}
			ranges[string(name)] = value
		}
		indexHealth, err := syncStatusReportFromDB(db, store.SyncScopeAll)
		if err != nil {
			return err
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
			LiveStatus:       live,
			CurrentSession:   current,
			IndexHealth:      indexHealth,
		}
		return nil
	})
	if err != nil {
		return err
	}
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

func dashboardRangeFor(db *sql.DB, now time.Time, name config.MetricsRange, agent string) (dashboardRange, error) {
	startTime, endTime := name.Bounds(now)
	start, end := startTime.Unix(), endTime.Unix()
	models, err := store.GetModelStats(db, start, end, agent)
	if err != nil {
		return dashboardRange{}, err
	}
	skills, err := store.GetSkillStats(db, start, end, agent)
	if err != nil {
		return dashboardRange{}, err
	}
	projects, err := store.GetStats(db, start, end, agent)
	if err != nil {
		return dashboardRange{}, err
	}
	sessions, err := store.ListSessions(db, start, end, agent, "")
	if err != nil {
		return dashboardRange{}, err
	}
	speed, err := store.GetSpeedStats(db, start, end, agent)
	if err != nil {
		return dashboardRange{}, err
	}
	view := make([]dashboardSession, 0, len(sessions))
	for _, session := range sessions {
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
	speed.Models = nonNil(speed.Models)
	speed.Turns = nonNil(speed.Turns)
	return dashboardRange{
		// end is exclusive, so the last included day is the one before it.
		StartDate:    startTime.Format("2006-01-02"),
		EndDate:      endTime.AddDate(0, 0, -1).Format("2006-01-02"),
		ModelStats:   nonNil(models),
		Sessions:     nonNil(view),
		SkillStats:   nonNil(skills),
		ProjectStats: nonNil(projects),
		Speed:        speed,
	}, nil
}

func nonNil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}
