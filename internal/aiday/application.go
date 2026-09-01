package aiday

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// ServiceOptions are the infrastructure ports used by the AI Day application
// service. Production uses the local SQLite store; tests can replace individual
// ports without teaching a command handler about persistence.
type ServiceOptions struct {
	OpenRead  func() (*sql.DB, error)
	OpenWrite func() (*sql.DB, error)
	Sync      func(*sql.DB) (int, error)
	Now       func() time.Time
	Location  func() *time.Location
}

// Service owns AI Day use-case orchestration. The lower-level projection,
// reward, privacy, and persistence functions remain reusable domain operations;
// adapters call this type instead of sequencing those operations themselves.
type Service struct {
	openRead  func() (*sql.DB, error)
	openWrite func() (*sql.DB, error)
	sync      func(*sql.DB) (int, error)
	now       func() time.Time
	location  func() *time.Location
}

// Default is the application service used by CLI and IPC adapters.
var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.OpenWrite == nil {
		options.OpenWrite = store.Open
	}
	if options.Sync == nil {
		options.Sync = store.SyncAll
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Location == nil {
		options.Location = func() *time.Location { return config.Loc }
	}
	return Service{
		openRead:  options.OpenRead,
		openWrite: options.OpenWrite,
		sync:      options.Sync,
		now:       options.Now,
		location:  options.Location,
	}
}

// OperationMeta reports adapter-neutral work performed while serving a use
// case. It is intentionally not embedded in the stable AI Day JSON contracts.
type OperationMeta struct {
	SyncedFiles int
}

type TodayInput struct {
	Sync bool `json:"sync,omitempty"`
}

type RebuildInput struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Sync bool   `json:"sync,omitempty"`
}

type DashboardInput struct {
	Days int  `json:"days"`
	Sync bool `json:"sync,omitempty"`
}

type DayInput struct {
	Day string `json:"day"`
}

type FeedbackInput struct {
	Day            string   `json:"day"`
	Verdict        string   `json:"verdict,omitempty"`
	CorrectedBadge string   `json:"corrected_badge_id,omitempty"`
	SemanticLabels []string `json:"semantic_labels,omitempty"`
	Clear          bool     `json:"clear,omitempty"`
}

type SourceInput struct {
	Source          string `json:"source"`
	Enabled         bool   `json:"enabled"`
	SemanticEnabled bool   `json:"semantic_enabled"`
}

type SourceDeleteInput struct {
	Source    string `json:"source"`
	Confirmed bool   `json:"confirmed"`
}

type SourceDeleteResult struct {
	Source        string `json:"source"`
	EventsDeleted int64  `json:"events_deleted"`
	Paused        bool   `json:"paused"`
}

type PrivacyPatch struct {
	SemanticEnabled *bool `json:"semantic_enabled,omitempty"`
	RetentionDays   *int  `json:"retention_days,omitempty"`
}

type DeleteInput struct {
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	All       bool   `json:"all,omitempty"`
	Confirmed bool   `json:"confirmed"`
}

func (service Service) Today(ctx context.Context, input TodayInput) (Result, OperationMeta, error) {
	db, meta, err := service.openDatabase(false, input.Sync)
	if err != nil {
		return Result{}, meta, err
	}
	defer db.Close()

	now, loc := service.now(), service.location()
	summary, err := Refresh(ctx, db, now, loc, 30)
	if err != nil {
		return Result{}, meta, unavailable("refresh AI Day", err)
	}
	if len(summary.Days) == 0 {
		return Result{}, meta, application.NewError(application.CodeInternal, "AI Day refresh returned no current day")
	}
	return summary.Days[len(summary.Days)-1], meta, nil
}

func (service Service) Show(ctx context.Context, day string) (Result, error) {
	if _, err := service.parseDay(day); err != nil {
		return Result{}, err
	}
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()

	result, err := Load(ctx, db, day)
	if errors.Is(err, ErrDayNotBuilt) {
		appErr := application.WrapError(application.CodeNotFound, fmt.Sprintf("AI Day %s has not been built", day), err)
		appErr.Details = map[string]any{"day": day}
		return Result{}, appErr
	}
	if err != nil {
		return Result{}, unavailable("load AI Day", err)
	}
	return result, nil
}

func (service Service) Rebuild(ctx context.Context, input RebuildInput) (RebuildSummary, OperationMeta, error) {
	from, to, err := service.rebuildRange(input.From, input.To)
	if err != nil {
		return RebuildSummary{}, OperationMeta{}, err
	}
	db, meta, err := service.openDatabase(false, input.Sync)
	if err != nil {
		return RebuildSummary{}, meta, err
	}
	defer db.Close()

	summary, err := Rebuild(ctx, db, from, to, service.location())
	if err != nil {
		return RebuildSummary{}, meta, unavailable("rebuild AI Day", err)
	}
	return summary, meta, nil
}

func (service Service) Dashboard(ctx context.Context, input DashboardInput) (Dashboard, OperationMeta, error) {
	if input.Days < 1 || input.Days > 3650 {
		return Dashboard{}, OperationMeta{}, invalidArgument("days must be between 1 and 3650", "days", input.Days)
	}
	db, meta, err := service.openDatabase(false, input.Sync)
	if err != nil {
		return Dashboard{}, meta, err
	}
	defer db.Close()

	now, loc := service.now(), service.location()
	summary, err := Refresh(ctx, db, now, loc, 30)
	if err != nil {
		return Dashboard{}, meta, unavailable("refresh AI Day dashboard", err)
	}
	if len(summary.Days) == 0 {
		return Dashboard{}, meta, application.NewError(application.CodeInternal, "AI Day refresh returned no current day")
	}
	atlas, err := LoadAtlas(ctx, db)
	if err != nil {
		return Dashboard{}, meta, unavailable("load AI Day atlas", err)
	}
	today := now.In(loc)
	history, err := LoadHistory(ctx, db, today.AddDate(0, 0, -input.Days+1).Format(time.DateOnly), today.Format(time.DateOnly))
	if err != nil {
		return Dashboard{}, meta, unavailable("load AI Day history", err)
	}
	privacy, err := LoadPrivacy(ctx, db)
	if err != nil {
		return Dashboard{}, meta, unavailable("load AI Day privacy", err)
	}
	return Dashboard{
		SchemaVersion: ContractVersion,
		Today:         summary.Days[len(summary.Days)-1],
		Atlas:         atlas,
		History:       history,
		Privacy:       privacy,
	}, meta, nil
}

func (service Service) History(ctx context.Context, days int) (History, error) {
	if days < 1 || days > 3650 {
		return History{}, invalidArgument("days must be between 1 and 3650", "days", days)
	}
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return History{}, err
	}
	defer db.Close()

	to := service.now().In(service.location())
	from := to.AddDate(0, 0, -days+1)
	history, err := LoadHistory(ctx, db, from.Format(time.DateOnly), to.Format(time.DateOnly))
	if err != nil {
		return History{}, unavailable("load AI Day history", err)
	}
	return history, nil
}

func (service Service) Atlas(ctx context.Context) (Atlas, error) {
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return Atlas{}, err
	}
	defer db.Close()
	atlas, err := LoadAtlas(ctx, db)
	if err != nil {
		return Atlas{}, unavailable("load AI Day atlas", err)
	}
	return atlas, nil
}

func (service Service) Badge(ctx context.Context, id string) (Badge, error) {
	if strings.TrimSpace(id) == "" {
		return Badge{}, invalidArgument("badge ID is required", "id", id)
	}
	if _, ok := definition(id); !ok {
		err := application.NewError(application.CodeNotFound, fmt.Sprintf("unknown AI Day badge %q", id))
		err.Details = map[string]any{"badge_id": id}
		return Badge{}, err
	}
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return Badge{}, err
	}
	defer db.Close()
	badge, err := LoadBadge(ctx, db, id)
	if err != nil {
		return Badge{}, unavailable("load AI Day badge", err)
	}
	return badge, nil
}

func (service Service) Feedback(ctx context.Context, input FeedbackInput) (Result, error) {
	day, err := service.parseDay(input.Day)
	if err != nil {
		return Result{}, err
	}
	if input.Clear {
		if input.Verdict != "" || input.CorrectedBadge != "" || len(input.SemanticLabels) > 0 {
			return Result{}, invalidArgument("clear cannot be combined with feedback values", "clear", true)
		}
	} else if input.Verdict == "" {
		return Result{}, invalidArgument("verdict is required unless clear is set", "verdict", input.Verdict)
	}
	if !input.Clear {
		if input.Verdict != "accurate" && input.Verdict != "inaccurate" && input.Verdict != "corrected" {
			return Result{}, invalidArgument(fmt.Sprintf("invalid verdict %q", input.Verdict), "verdict", input.Verdict)
		}
		if input.CorrectedBadge != "" {
			if _, ok := definition(input.CorrectedBadge); !ok {
				return Result{}, invalidArgument(
					fmt.Sprintf("unknown AI Day badge %q", input.CorrectedBadge),
					"corrected_badge_id", input.CorrectedBadge,
				)
			}
		}
		for _, label := range input.SemanticLabels {
			if !semanticIntentAllowed(label) {
				return Result{}, invalidArgument(
					fmt.Sprintf("unknown semantic label %q", label), "semantic_labels", label,
				)
			}
		}
	}

	db, _, err := service.openDatabase(false, false)
	if err != nil {
		return Result{}, err
	}
	defer db.Close()
	if input.Clear {
		err = ClearFeedback(ctx, db, input.Day)
	} else {
		err = SaveFeedback(ctx, db, Feedback{
			Day:            input.Day,
			Verdict:        input.Verdict,
			CorrectedBadge: input.CorrectedBadge,
			SemanticLabels: input.SemanticLabels,
		})
	}
	if err != nil {
		return Result{}, unavailable("save AI Day feedback", err)
	}
	result, err := RebuildDay(ctx, db, day, service.location())
	if err != nil {
		return Result{}, unavailable("rebuild AI Day after feedback", err)
	}
	return result, nil
}

func (service Service) Sources(ctx context.Context) ([]SourceSetting, error) {
	privacy, err := service.Privacy(ctx)
	if err != nil {
		return nil, err
	}
	return privacy.Sources, nil
}

func (service Service) SetSource(ctx context.Context, input SourceInput) (Privacy, error) {
	if strings.TrimSpace(input.Source) == "" {
		return Privacy{}, invalidArgument("source is required", "source", input.Source)
	}
	db, _, err := service.openDatabase(false, false)
	if err != nil {
		return Privacy{}, err
	}
	defer db.Close()
	if err := SetSource(ctx, db, input.Source, input.Enabled, input.SemanticEnabled); err != nil {
		return Privacy{}, unavailable("save AI Day source preference", err)
	}
	privacy, err := LoadPrivacy(ctx, db)
	if err != nil {
		return Privacy{}, unavailable("load AI Day privacy", err)
	}
	return privacy, nil
}

func (service Service) DeleteSource(ctx context.Context, source string, confirmed bool) (SourceDeleteResult, error) {
	if !confirmed {
		return SourceDeleteResult{}, invalidArgument("source deletion requires confirmation", "confirmed", false)
	}
	if strings.TrimSpace(source) == "" {
		return SourceDeleteResult{}, invalidArgument("source is required", "source", source)
	}
	db, _, err := service.openDatabase(false, false)
	if err != nil {
		return SourceDeleteResult{}, err
	}
	defer db.Close()
	n, err := DeleteSource(ctx, db, source)
	if err != nil {
		return SourceDeleteResult{}, unavailable("delete AI Day source data", err)
	}
	return SourceDeleteResult{Source: source, EventsDeleted: n, Paused: true}, nil
}

func (service Service) Privacy(ctx context.Context) (Privacy, error) {
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return Privacy{}, err
	}
	defer db.Close()
	privacy, err := LoadPrivacy(ctx, db)
	if err != nil {
		return Privacy{}, unavailable("load AI Day privacy", err)
	}
	return privacy, nil
}

func (service Service) SetPrivacy(ctx context.Context, patch PrivacyPatch) (Privacy, error) {
	if patch.SemanticEnabled == nil && patch.RetentionDays == nil {
		return Privacy{}, invalidArgument("set semantic_enabled and/or retention_days", "patch", nil)
	}
	if patch.RetentionDays != nil && (*patch.RetentionDays < 1 || *patch.RetentionDays > 3650) {
		return Privacy{}, invalidArgument(
			"retention days must be between 1 and 3650", "retention_days", *patch.RetentionDays,
		)
	}
	db, _, err := service.openDatabase(false, false)
	if err != nil {
		return Privacy{}, err
	}
	defer db.Close()
	if err := SetPrivacy(ctx, db, patch.SemanticEnabled, patch.RetentionDays); err != nil {
		return Privacy{}, unavailable("save AI Day privacy", err)
	}
	privacy, err := LoadPrivacy(ctx, db)
	if err != nil {
		return Privacy{}, unavailable("load AI Day privacy", err)
	}
	return privacy, nil
}

func (service Service) Export(ctx context.Context) (Export, error) {
	db, _, err := service.openDatabase(true, false)
	if err != nil {
		return Export{}, err
	}
	defer db.Close()
	export, err := ExportAll(ctx, db)
	if err != nil {
		return Export{}, unavailable("export AI Day data", err)
	}
	return export, nil
}

func (service Service) Delete(ctx context.Context, input DeleteInput) (DeleteSummary, error) {
	if !input.Confirmed {
		return DeleteSummary{}, invalidArgument("AI Day deletion requires confirmation", "confirmed", false)
	}
	from, to := input.From, input.To
	if input.All {
		from, to = "0001-01-01", "9999-12-31"
	} else {
		if from == "" {
			return DeleteSummary{}, invalidArgument("from is required unless all is set", "from", from)
		}
		if to == "" {
			to = from
		}
		if _, err := service.parseDay(from); err != nil {
			return DeleteSummary{}, err
		}
		if _, err := service.parseDay(to); err != nil {
			return DeleteSummary{}, err
		}
		if to < from {
			return DeleteSummary{}, invalidArgument("delete end day is before start day", "to", to)
		}
	}
	db, _, err := service.openDatabase(false, false)
	if err != nil {
		return DeleteSummary{}, err
	}
	defer db.Close()
	summary, err := DeleteRange(ctx, db, from, to)
	if err != nil {
		return DeleteSummary{}, unavailable("delete AI Day data", err)
	}
	return summary, nil
}

func (service Service) openDatabase(readOnly, syncRequested bool) (*sql.DB, OperationMeta, error) {
	opener := service.openWrite
	if readOnly && !syncRequested {
		opener = service.openRead
	}
	db, err := opener()
	if err != nil {
		return nil, OperationMeta{}, unavailable("open ATM database", err)
	}
	meta := OperationMeta{}
	if syncRequested {
		meta.SyncedFiles, err = service.sync(db)
		if err != nil {
			db.Close()
			return nil, OperationMeta{}, unavailable("sync AI Day sources", err)
		}
	}
	return db, meta, nil
}

func (service Service) parseDay(value string) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, value, service.location())
	if err != nil {
		return time.Time{}, invalidArgument(fmt.Sprintf("invalid day %q (use YYYY-MM-DD)", value), "day", value)
	}
	return day, nil
}

func (service Service) rebuildRange(fromValue, toValue string) (time.Time, time.Time, error) {
	now, loc := service.now().In(service.location()), service.location()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	var err error
	if fromValue != "" {
		from, err = service.parseDay(fromValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	to := from
	if toValue != "" {
		to, err = service.parseDay(toValue)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
	}
	if to.Before(from) {
		return time.Time{}, time.Time{}, invalidArgument(
			fmt.Sprintf("end day %s is before start day %s", to.Format(time.DateOnly), from.Format(time.DateOnly)),
			"to", toValue,
		)
	}
	return from, to, nil
}

func invalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func unavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action+" failed", cause)
	err.Retryable = true
	return err
}

func semanticIntentAllowed(value string) bool {
	for _, allowed := range SemanticIntents {
		if value == allowed {
			return true
		}
	}
	return false
}
