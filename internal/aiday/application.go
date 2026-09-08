package aiday

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/executionlock"
	"github.com/zane-byte-dev/atm/internal/store"
)

// ServiceOptions are the infrastructure ports used by the AI Day application
// service. Production uses the local SQLite store; tests can replace individual
// ports without teaching a command handler about persistence.
type ServiceOptions struct {
	OpenRead    func() (*sql.DB, error)
	OpenWrite   func() (*sql.DB, error)
	SyncContext func(context.Context, *sql.DB) (int, error)
	Now         func() time.Time
	Location    func() *time.Location
}

// Service owns AI Day use-case orchestration. The lower-level projection,
// reward, privacy, and persistence functions remain reusable domain operations;
// adapters call this type instead of sequencing those operations themselves.
type Service struct {
	openRead  func() (*sql.DB, error)
	openWrite func() (*sql.DB, error)
	sync      func(context.Context, *sql.DB) (int, error)
	now       func() time.Time
	location  func() *time.Location
}

// Default is the application service used by CLI and Web adapters.
var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.OpenWrite == nil {
		options.OpenWrite = store.Open
	}
	if options.SyncContext == nil {
		options.SyncContext = store.SyncAllContext
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
		sync:      options.SyncContext,
		now:       options.Now,
		location:  options.Location,
	}
}

// OperationMeta reports adapter-neutral work performed while serving a use
// case. It is intentionally not embedded in the stable AI Day JSON contracts.
type OperationMeta struct {
	SyncedFiles int
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

func (service Service) Rebuild(ctx context.Context, input RebuildInput) (RebuildSummary, OperationMeta, error) {
	from, to, err := service.rebuildRange(input.From, input.To)
	if err != nil {
		return RebuildSummary{}, OperationMeta{}, err
	}
	db, meta, lock, err := service.openProjectionDatabase(ctx, input.Sync)
	if err != nil {
		return RebuildSummary{}, meta, err
	}
	defer db.Close()
	defer lock.Close()

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
	db, meta, lock, err := service.openProjectionDatabase(ctx, input.Sync)
	if err != nil {
		return Dashboard{}, meta, err
	}
	defer db.Close()
	defer lock.Close()

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

func (service Service) Sources(ctx context.Context) ([]SourceSetting, error) {
	privacy, err := service.Privacy(ctx)
	if err != nil {
		return nil, err
	}
	return privacy.Sources, nil
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

// The projection lock covers all days in one refresh, including baseline and
// award reads. Sync finishes (and releases its own lock) before this acquisition
// so the two domains never hold each other's locks in opposite order.
func (service Service) openProjectionDatabase(ctx context.Context, syncRequested bool) (*sql.DB, OperationMeta, *executionlock.Lock, error) {
	db, meta, err := service.openDatabaseContext(ctx, false, syncRequested)
	if err != nil {
		return nil, meta, nil, err
	}
	lock, err := executionlock.AcquireDatabase(ctx, db, "aiday")
	if err != nil {
		db.Close()
		return nil, meta, nil, unavailable("lock AI Day projection", err)
	}
	return db, meta, lock, nil
}

func (service Service) openDatabase(readOnly, syncRequested bool) (*sql.DB, OperationMeta, error) {
	return service.openDatabaseContext(context.Background(), readOnly, syncRequested)
}

func (service Service) openDatabaseContext(ctx context.Context, readOnly, syncRequested bool) (*sql.DB, OperationMeta, error) {
	if err := ctx.Err(); err != nil {
		return nil, OperationMeta{}, unavailable("open ATM database", err)
	}
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
		meta.SyncedFiles, err = service.sync(ctx, db)
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
