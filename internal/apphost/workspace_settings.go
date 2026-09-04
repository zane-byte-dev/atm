package apphost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zane-byte-dev/atm/internal/aiday"
	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/store"
)

// This adapter deliberately does not call aiday.Service.Dashboard: even with
// Sync=false that operation refreshes projections. Browsing uses stored rows.
func (h *Host) callWorkspaceSettings(ctx context.Context, call application.Call, method string, input json.RawMessage) (any, error) {
	switch method {
	case "day.snapshot":
		return invoke(input, func(value AIDayRangeInput) (any, error) { return h.AIDaySnapshot(ctx, call, value) })
	case "day.show":
		return invoke(input, func(value aiday.DayInput) (any, error) { return h.AIDayShow(ctx, call, value) })
	case "day.ledger":
		return invoke(input, func(value AIDayLedgerInput) (any, error) { return h.AIDayLedger(ctx, call, value) })
	case "settings.get":
		return invoke(input, func(struct{}) (any, error) { return h.WorkspaceSettings(ctx, call) })
	case "settings.preferences.save":
		return invoke(input, func(value WorkspacePreferencesInput) (any, error) {
			return h.SaveWorkspacePreferences(ctx, call, value)
		})
	case "settings.business.save":
		return invoke(input, func(value WorkspaceBusinessInput) (any, error) { return h.SaveWorkspaceBusiness(ctx, call, value) })
	case "settings.credential.save":
		return invoke(input, func(value config.CredentialSaveInput) (any, error) {
			return h.saveWorkspaceCredential(ctx, call, &value)
		})
	case "settings.credential.delete":
		return invoke(input, func(struct{}) (any, error) { return h.saveWorkspaceCredential(ctx, call, nil) })
	default:
		return nil, application.NewError(application.CodeNotFound, "unknown API method")
	}
}

type AIDayRangeInput struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

type AIDaySummary struct {
	Day           string `json:"day"`
	State         string `json:"state"`
	Title         string `json:"title"`
	Explanation   string `json:"explanation"`
	BadgeID       string `json:"badge_id"`
	Origin        string `json:"origin"`
	SessionCount  int64  `json:"session_count"`
	TurnCount     int64  `json:"turn_count"`
	ToolCalls     int64  `json:"tool_calls"`
	EventCount    int64  `json:"event_count"`
	WorkTokens    int64  `json:"work_tokens"`
	ActiveSeconds int64  `json:"active_seconds"`
	SourceCount   int64  `json:"source_count"`
	GeneratedAt   int64  `json:"generated_at"`
}

type AIDaySnapshot struct {
	From     string         `json:"from"`
	To       string         `json:"to"`
	Today    string         `json:"today"`
	Timezone string         `json:"timezone"`
	Indexed  bool           `json:"indexed"`
	History  []AIDaySummary `json:"history"`
	Atlas    *aiday.Atlas   `json:"atlas"`
	Privacy  *aiday.Privacy `json:"privacy"`
}

func workspaceLocation() *time.Location {
	if config.Loc != nil {
		return config.Loc
	}
	return time.UTC
}

func workspaceDay(value string) (time.Time, error) {
	day, err := time.ParseInLocation(time.DateOnly, value, workspaceLocation())
	if err != nil || len(value) != 10 {
		return time.Time{}, invalid("day must be a calendar date (YYYY-MM-DD)")
	}
	return day, nil
}

func workspaceDayRange(input AIDayRangeInput) (string, string, error) {
	to := time.Now().In(workspaceLocation())
	var err error
	if input.To != "" {
		to, err = workspaceDay(input.To)
		if err != nil {
			return "", "", err
		}
	}
	from := to.AddDate(0, 0, -29)
	if input.From != "" {
		from, err = workspaceDay(input.From)
		if err != nil {
			return "", "", err
		}
	}
	fromValue, toValue := from.Format(time.DateOnly), to.Format(time.DateOnly)
	if fromValue > toValue || from.AddDate(0, 0, 365).Format(time.DateOnly) < toValue {
		return "", "", invalid("date range must be ordered and contain at most 366 days")
	}
	return fromValue, toValue, nil
}

func workspaceSettingsReadError(message string, cause error) error {
	// Database/credential errors can contain local paths or connector output.
	// Preserve the cause internally, but expose only a fixed diagnostic.
	return application.WrapError(application.CodeUnavailable, message, cause)
}

func workspaceHasTable(ctx context.Context, db *sql.DB, name string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&count)
	return count > 0, err
}

func (h *Host) AIDaySnapshot(ctx context.Context, call application.Call, input AIDayRangeInput) (AIDaySnapshot, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return AIDaySnapshot{}, err
	}
	from, to, err := workspaceDayRange(input)
	if err != nil {
		return AIDaySnapshot{}, err
	}
	result := AIDaySnapshot{From: from, To: to, Today: time.Now().In(workspaceLocation()).Format(time.DateOnly), Timezone: workspaceLocation().String(), History: []AIDaySummary{}}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, workspaceSettingsReadError("AI Day data could not be read", err)
	}
	defer db.Close()
	result.Indexed = true
	exists, err := workspaceHasTable(ctx, db, "ai_day_results")
	if err != nil {
		return result, workspaceSettingsReadError("AI Day data could not be read", err)
	}
	if !exists {
		return result, nil
	}
	// The range query is bounded and avoids loading/recomputing every day's
	// coverage and percentiles just to render the history picker.
	rows, err := db.QueryContext(ctx, `SELECT f.day,r.state,r.title,r.explanation,r.concept_id,r.origin,
		f.session_count,f.turn_count,f.tool_calls,COALESCE(d.event_count,0),
		f.input_tokens+f.output_tokens+f.cache_create_tokens,COALESCE(d.active_seconds,0),f.source_count,r.generated_at
		FROM ai_day_features f JOIN ai_day_results r ON r.day=f.day
		LEFT JOIN ai_day_feature_details d ON d.day=f.day
		WHERE f.day>=? AND f.day<=? ORDER BY f.day DESC LIMIT 366`, from, to)
	if err != nil {
		return result, workspaceSettingsReadError("AI Day history could not be read", err)
	}
	for rows.Next() {
		var item AIDaySummary
		if err := rows.Scan(&item.Day, &item.State, &item.Title, &item.Explanation, &item.BadgeID, &item.Origin, &item.SessionCount, &item.TurnCount, &item.ToolCalls, &item.EventCount, &item.WorkTokens, &item.ActiveSeconds, &item.SourceCount, &item.GeneratedAt); err != nil {
			rows.Close()
			return result, workspaceSettingsReadError("AI Day history could not be read", err)
		}
		result.History = append(result.History, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, workspaceSettingsReadError("AI Day history could not be read", err)
	}
	atlas, err := aiday.LoadAtlas(ctx, db)
	if err != nil {
		return result, workspaceSettingsReadError("AI Day badges could not be read", err)
	}
	privacy, err := aiday.LoadPrivacy(ctx, db)
	if err != nil {
		return result, workspaceSettingsReadError("AI Day source settings could not be read", err)
	}
	result.Atlas, result.Privacy = &atlas, &privacy
	return result, nil
}

func (h *Host) AIDayShow(ctx context.Context, call application.Call, input aiday.DayInput) (aiday.Result, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return aiday.Result{}, err
	}
	if _, err := workspaceDay(input.Day); err != nil {
		return aiday.Result{}, err
	}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return aiday.Result{}, application.NewError(application.CodeNotFound, "this day has no stored AI Day result")
	}
	if err != nil {
		return aiday.Result{}, workspaceSettingsReadError("AI Day data could not be read", err)
	}
	defer db.Close()
	result, err := aiday.Load(ctx, db, input.Day)
	if errors.Is(err, aiday.ErrDayNotBuilt) {
		return result, application.NewError(application.CodeNotFound, "this day has no stored AI Day result")
	}
	if err != nil {
		return result, workspaceSettingsReadError("AI Day result could not be read", err)
	}
	return result, nil
}

type AIDayLedgerInput struct {
	Day    string `json:"day"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// A ledger event has measurements only. In particular it never returns the
// session hash, event ID, semantic labels, raw messages, or source file path.
type AIDayLedgerEvent struct {
	OccurredAt        int64  `json:"occurred_at"`
	Source            string `json:"source"`
	EventType         string `json:"event_type"`
	Quantity          int64  `json:"quantity"`
	Modality          string `json:"modality"`
	ExecutionMode     string `json:"execution_mode"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	CacheCreateTokens int64  `json:"cache_create_tokens"`
	CacheReadTokens   int64  `json:"cache_read_tokens"`
	DurationMS        int64  `json:"duration_ms"`
}

type AIDayLedger struct {
	Day    string             `json:"day"`
	Items  []AIDayLedgerEvent `json:"items"`
	Total  int                `json:"total"`
	Limit  int                `json:"limit"`
	Offset int                `json:"offset"`
}

func (h *Host) AIDayLedger(ctx context.Context, call application.Call, input AIDayLedgerInput) (AIDayLedger, error) {
	h.gate.RLock()
	defer h.gate.RUnlock()
	if err := validate(ctx, call); err != nil {
		return AIDayLedger{}, err
	}
	day, err := workspaceDay(input.Day)
	if err != nil {
		return AIDayLedger{}, err
	}
	if input.Limit < 0 || input.Limit > 100 || input.Offset < 0 || input.Offset > 100000 {
		return AIDayLedger{}, invalid("limit must be 1–100 and offset must be 0–100000")
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	result := AIDayLedger{Day: input.Day, Items: []AIDayLedgerEvent{}, Limit: input.Limit, Offset: input.Offset}
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		return result, workspaceSettingsReadError("AI Day events could not be read", err)
	}
	defer db.Close()
	exists, err := workspaceHasTable(ctx, db, "ai_day_events")
	if err != nil {
		return result, workspaceSettingsReadError("AI Day events could not be read", err)
	}
	if !exists {
		return result, nil
	}
	start, end := day.Unix(), day.AddDate(0, 0, 1).Unix()
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ai_day_events WHERE occurred_at>=? AND occurred_at<?`, start, end).Scan(&result.Total); err != nil {
		return result, workspaceSettingsReadError("AI Day events could not be read", err)
	}
	rows, err := db.QueryContext(ctx, `SELECT occurred_at,source,event_type,quantity,modality,execution_mode,input_tokens,output_tokens,cache_create_tokens,cache_read_tokens,duration_ms
		FROM ai_day_events WHERE occurred_at>=? AND occurred_at<? ORDER BY occurred_at DESC,event_id DESC LIMIT ? OFFSET ?`, start, end, input.Limit, input.Offset)
	if err != nil {
		return result, workspaceSettingsReadError("AI Day events could not be read", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event AIDayLedgerEvent
		if err := rows.Scan(&event.OccurredAt, &event.Source, &event.EventType, &event.Quantity, &event.Modality, &event.ExecutionMode, &event.InputTokens, &event.OutputTokens, &event.CacheCreateTokens, &event.CacheReadTokens, &event.DurationMS); err != nil {
			return result, workspaceSettingsReadError("AI Day events could not be read", err)
		}
		result.Items = append(result.Items, event)
	}
	if err := rows.Err(); err != nil {
		return result, workspaceSettingsReadError("AI Day events could not be read", err)
	}
	return result, nil
}

type WorkspacePreferencesInput struct {
	OwnerName string `json:"owner_name"`
}

type WorkspaceBusinessInput struct {
	Revision string `json:"revision"`
	config.SettingsPatch
}

type WorkspacePreferences struct {
	GrokLiveQuota                  bool   `json:"grok_live_quota"`
	CollectionEnabled              bool   `json:"collection_enabled"`
	CollectionIntervalMinutes      int    `json:"collection_interval_minutes"`
	CollectionLookbackMinutes      int    `json:"collection_lookback_minutes"`
	CollectionMessageRetentionDays int    `json:"collection_message_retention_days"`
	TodoRefineOnAdd                bool   `json:"todo_refine_on_add"`
	TodoRefinePrompt               string `json:"todo_refine_prompt"`
}

type WorkspaceModel struct {
	Name                 string `json:"name"`
	Source               string `json:"source"`
	BaseURL              string `json:"base_url"`
	CredentialConfigured bool   `json:"credential_configured"`
	CredentialStatus     string `json:"credential_status"`
}

type WorkspaceProvider struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
}

type WorkspaceRuntime struct {
	Mode           string `json:"mode"`
	Version        string `json:"version"`
	BackgroundSync bool   `json:"background_sync"`
	Collection     bool   `json:"collection"`
	Models         bool   `json:"models"`
	AgentHooks     bool   `json:"agent_hooks"`
}

type WorkspaceSync struct {
	Status           string  `json:"status"`
	RunStatus        string  `json:"run_status"`
	SchemaVersion    int     `json:"schema_version"`
	IndexedSessions  int     `json:"indexed_sessions"`
	RetainedSessions int     `json:"retained_sessions"`
	LastAttemptAt    *string `json:"last_attempt_at"`
	LastSuccessAt    *string `json:"last_success_at"`
	AgeSeconds       *int64  `json:"age_seconds"`
	LastSyncedFiles  int     `json:"last_synced_files"`
	HasError         bool    `json:"has_error"`
	Indexed          bool    `json:"indexed"`
}

type WorkspaceSettings struct {
	Revision    string               `json:"revision"`
	OwnerName   string               `json:"owner_name"`
	Timezone    string               `json:"timezone"`
	Preferences WorkspacePreferences `json:"preferences"`
	Model       WorkspaceModel       `json:"model"`
	Providers   []WorkspaceProvider  `json:"providers"`
	Runtime     WorkspaceRuntime     `json:"runtime"`
	Sync        WorkspaceSync        `json:"sync"`
}

func (h *Host) WorkspaceSettings(ctx context.Context, call application.Call) (WorkspaceSettings, error) {
	if err := validate(ctx, call); err != nil {
		return WorkspaceSettings{}, err
	}
	// Refresh opportunistically when no operation has pinned the current
	// configuration. While a background job is running, serve the exact cached
	// snapshot it uses instead of making the settings page return a transient
	// 503 for the duration of the job.
	if h.gate.TryLock() {
		defer h.gate.Unlock()
		revision, err := config.Default.ReloadRevision()
		h.restoreDataPaths()
		if err != nil {
			h.configRevisionErr = err
			return WorkspaceSettings{}, workspaceSettingsReadError("settings could not be read", err)
		}
		h.configRevision, h.configRevisionErr = revision, nil
		return h.workspaceSettings(ctx)
	}
	h.gate.RLock()
	defer h.gate.RUnlock()
	return h.workspaceSettings(ctx)
}

func (h *Host) workspaceSettings(ctx context.Context) (WorkspaceSettings, error) {
	if h.configRevisionErr != nil {
		return WorkspaceSettings{}, workspaceSettingsReadError("settings could not be read", h.configRevisionErr)
	}
	if h.configRevision == "" {
		return WorkspaceSettings{}, workspaceSettingsReadError("settings could not be read", errors.New("configuration revision is unavailable"))
	}
	result := WorkspaceSettings{
		Revision:  h.configRevision,
		OwnerName: config.OwnerName, Timezone: workspaceLocation().String(),
		Preferences: WorkspacePreferences{GrokLiveQuota: config.GrokLiveQuota, CollectionEnabled: config.CollectionEnabled, CollectionIntervalMinutes: config.CollectionIntervalMinutes, CollectionLookbackMinutes: config.CollectionLookbackMinutes, CollectionMessageRetentionDays: config.CollectionMessageRetentionDays, TodoRefineOnAdd: config.TodoRefineOnAdd, TodoRefinePrompt: config.TodoRefinePrompt},
		Model:       WorkspaceModel{Name: config.TextModelName, Source: config.TextModelSource, BaseURL: safeModelBaseURL(config.TextModelBaseURL), CredentialStatus: "missing"},
		Providers:   []WorkspaceProvider{},
		Runtime:     h.workspaceRuntime(),
		Sync:        WorkspaceSync{Status: "never", RunStatus: "never"},
	}
	credential, err := config.Default.CredentialStatus()
	if err != nil {
		result.Model.CredentialStatus = "unavailable"
	} else if credential.Configured {
		result.Model.CredentialConfigured = true
		result.Model.CredentialStatus = "configured"
	}
	for name := range config.QuotaProviders {
		result.Providers = append(result.Providers, WorkspaceProvider{Name: name, Kind: "quota", Enabled: true})
	}
	for name := range config.CollectionConnectors {
		result.Providers = append(result.Providers, WorkspaceProvider{Name: name, Kind: "collection", Enabled: config.CollectionEnabled})
	}
	sort.Slice(result.Providers, func(i, j int) bool {
		if result.Providers[i].Kind != result.Providers[j].Kind {
			return result.Providers[i].Kind < result.Providers[j].Kind
		}
		return result.Providers[i].Name < result.Providers[j].Name
	})
	db, err := store.OpenReadOnly()
	if errors.Is(err, store.ErrDatabaseMissing) {
		return result, nil
	}
	if err != nil {
		result.Sync.Status, result.Sync.HasError = "unavailable", true
		return result, nil
	}
	defer db.Close()
	result.Sync.Indexed = true
	health, err := store.ReadSyncHealth(db, store.SyncScopeAll, time.Now(), store.DefaultSyncStaleAfter)
	if err != nil {
		result.Sync.Status, result.Sync.HasError = "unavailable", true
		return result, nil
	}
	result.Sync = WorkspaceSync{Status: health.Status, RunStatus: health.RunStatus, SchemaVersion: health.SchemaVersion, IndexedSessions: health.IndexedSessions, RetainedSessions: health.RetainedSessions, LastAttemptAt: health.LastAttemptAt, LastSuccessAt: health.LastSuccessAt, AgeSeconds: health.AgeSeconds, LastSyncedFiles: health.LastSyncedFiles, HasError: health.LastError != "", Indexed: true}
	return result, ctx.Err()
}

func safeModelBaseURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return value
}

func (h *Host) SaveWorkspaceBusiness(ctx context.Context, call application.Call, input WorkspaceBusinessInput) (WorkspaceSettings, error) {
	if !h.gate.TryLock() {
		return WorkspaceSettings{}, configBusy()
	}
	defer h.gate.Unlock()
	if err := validateWrite(ctx, call); err != nil {
		return WorkspaceSettings{}, err
	}
	if len(input.Revision) != 64 {
		return WorkspaceSettings{}, invalid("settings revision is required")
	}
	if input.OwnerName != nil {
		name := strings.TrimSpace(*input.OwnerName)
		if name == "" || utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
			return WorkspaceSettings{}, invalid("owner_name must contain 1–80 characters without control characters")
		}
		input.OwnerName = &name
	}
	for _, bounded := range []struct {
		value *int
		max   int
	}{{input.CollectionIntervalMinutes, 1440}, {input.CollectionLookbackMinutes, 10080}, {input.CollectionMessageRetentionDays, 3650}} {
		if bounded.value != nil && (*bounded.value < 0 || *bounded.value > bounded.max) {
			return WorkspaceSettings{}, invalid("collection schedule or retention is outside the supported range")
		}
	}
	for _, value := range []*string{input.TextModelBaseURL, input.TextModelName, input.TextModelSource} {
		if value != nil && (len(*value) > 2000 || strings.IndexFunc(*value, unicode.IsControl) >= 0) {
			return WorkspaceSettings{}, invalid("model settings contain an invalid value")
		}
	}
	if input.TextModelBaseURL != nil && safeModelBaseURL(strings.TrimSpace(*input.TextModelBaseURL)) == "" {
		return WorkspaceSettings{}, invalid("model endpoint must be an HTTP(S) URL without embedded credentials, query or fragment")
	}
	defer h.restoreDataPaths()
	if err := config.Default.ApplyRevision(input.Revision, input.SettingsPatch); err != nil {
		return WorkspaceSettings{}, err
	}
	revision, err := config.Default.ReloadRevision()
	h.restoreDataPaths()
	if err != nil {
		h.configRevisionErr = err
		return WorkspaceSettings{}, workspaceSettingsReadError("settings could not be read", err)
	}
	h.configRevision, h.configRevisionErr = revision, nil
	return h.workspaceSettings(ctx)
}

func (h *Host) saveWorkspaceCredential(ctx context.Context, call application.Call, input *config.CredentialSaveInput) (config.CredentialStatus, error) {
	if !h.gate.TryLock() {
		return config.CredentialStatus{}, configBusy()
	}
	defer h.gate.Unlock()
	if err := validateWrite(ctx, call); err != nil {
		return config.CredentialStatus{}, err
	}
	var result config.CredentialStatus
	var err error
	if input == nil {
		result, err = config.Default.DeleteCredential()
	} else {
		result, err = config.Default.SaveCredential(*input)
	}
	if err != nil {
		if errors.Is(err, application.ErrInvalidArgument) {
			return config.CredentialStatus{}, err
		}
		return config.CredentialStatus{}, workspaceSettingsReadError("model credential could not be saved", err)
	}
	return result, nil
}

func (h *Host) SaveWorkspacePreferences(ctx context.Context, call application.Call, input WorkspacePreferencesInput) (WorkspaceSettings, error) {
	if !h.gate.TryLock() {
		return WorkspaceSettings{}, configBusy()
	}
	defer h.gate.Unlock()
	if err := validate(ctx, call); err != nil {
		return WorkspaceSettings{}, err
	}
	if call.Actor.Kind != application.ActorHuman {
		return WorkspaceSettings{}, application.NewError(application.CodeForbidden, "only the owner can change personal preferences")
	}
	name := strings.TrimSpace(input.OwnerName)
	if name == "" || utf8.RuneCountInString(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return WorkspaceSettings{}, invalid("owner_name must contain 1–80 characters without control characters")
	}
	// Service.Set uses the existing advisory config lock and atomic rename. It
	// patches only this field and never opens/migrates the application database.
	// Set reloads config, including data_dir; the resident host remains pinned
	// to its explicit startup paths even when the saved file points elsewhere.
	defer h.restoreDataPaths()
	if _, err := config.Default.Set("owner_name", name); err != nil {
		return WorkspaceSettings{}, workspaceSettingsReadError("personal preference could not be saved", err)
	}
	revision, err := config.Default.ReloadRevision()
	h.restoreDataPaths()
	if err != nil {
		h.configRevisionErr = err
		return WorkspaceSettings{}, workspaceSettingsReadError("settings could not be read", err)
	}
	h.configRevision, h.configRevisionErr = revision, nil
	return h.workspaceSettings(ctx)
}
