// Package session owns the application use cases for browsing indexed agent
// conversations. It deliberately has no dependency on Cobra or command output;
// adapters supply typed intent and render the returned DTOs.
package session

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

type ServiceOptions struct {
	Now      func() time.Time
	Location *time.Location
}

type Service struct {
	now      func() time.Time
	location *time.Location
}

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Location == nil {
		options.Location = config.Loc
	}
	return Service{now: options.Now, location: options.Location}
}

// ReadMeta reports an explicit pre-read synchronization without coupling the
// service to a progress renderer.
type ReadMeta struct {
	SyncedFiles int `json:"synced_files,omitempty"`
}

func (service Service) openRead(ctx context.Context, syncBeforeRead bool) (*sql.DB, ReadMeta, error) {
	if err := contextError(ctx); err != nil {
		return nil, ReadMeta{}, err
	}
	var (
		db  *sql.DB
		err error
	)
	if syncBeforeRead {
		db, err = store.Open()
	} else {
		db, err = store.OpenReadOnly()
	}
	if err != nil {
		if errors.Is(err, store.ErrDatabaseMissing) {
			return nil, ReadMeta{}, unavailable(store.ErrDatabaseMissing.Error(), err)
		}
		return nil, ReadMeta{}, unavailable("session database is unavailable", err)
	}
	meta := ReadMeta{}
	if syncBeforeRead {
		meta.SyncedFiles, err = store.SyncAll(db)
		if err != nil {
			db.Close()
			return nil, ReadMeta{}, unavailable("session synchronization failed", err)
		}
	}
	if err := contextError(ctx); err != nil {
		db.Close()
		return nil, ReadMeta{}, err
	}
	return db, meta, nil
}

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

func invalidArgument(message, field string, value any) *application.Error {
	err := application.NewError(application.CodeInvalidArgument, message)
	err.Details = map[string]any{"field": field, "value": value}
	return err
}

func sessionNotFound(id string, cause error) *application.Error {
	err := application.WrapError(application.CodeNotFound, "session not found: "+id, cause)
	err.Details = map[string]any{"session_id": id}
	return err
}

func unavailable(action string, cause error) *application.Error {
	err := application.WrapError(application.CodeUnavailable, action, cause)
	err.Retryable = true
	return err
}

func (service Service) parseSince(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.In(service.location), nil
	}
	if parsed, err := time.ParseInLocation("2006-01-02", value, service.location); err == nil {
		return parsed, nil
	}
	return time.Time{}, invalidArgument(
		fmt.Sprintf("invalid since %q: use RFC3339 or YYYY-MM-DD", value), "since", value,
	)
}

func (service Service) startOfDayWindow(now time.Time, days int) time.Time {
	if days < 1 {
		days = 1
	}
	now = now.In(service.location)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, service.location).
		AddDate(0, 0, -(days - 1))
}

func page[T any](values []T, offset, limit int) ([]T, error) {
	if offset < 0 {
		return nil, invalidArgument("offset must not be negative", "offset", offset)
	}
	if limit < 0 {
		return nil, invalidArgument("limit must not be negative", "limit", limit)
	}
	if offset >= len(values) {
		return []T{}, nil
	}
	end := len(values)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return values[offset:end], nil
}
