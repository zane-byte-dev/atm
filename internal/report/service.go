package report

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/parser"
	"github.com/zane-byte-dev/atm/internal/store"
)

// agentOrder is the order sections appear in. Fixed rather than alphabetical or
// query-dependent so a daily report read side by side across days keeps the same
// shape.
var agentOrder = []string{
	"pi", "codex", "claude", "copilot", "qoder", "qodercli", "qoderwork",
	"grokbuild", "antigravity",
}

// ServiceOptions are the report's clock and persistence ports.
type ServiceOptions struct {
	Now       func() time.Time
	OpenRead  func() (*sql.DB, error)
	OpenWrite func() (*sql.DB, error)
	SyncAll   func(*sql.DB) (int, error)
	Query     func(*sql.DB, int64, int64, string) ([]store.ReportResult, error)
}

// Service owns the day's read: resolving the date, choosing which sessions are
// worth listing, and cleaning transcript text down to what a person actually
// wrote. Adapters format the result.
type Service struct {
	now       func() time.Time
	openRead  func() (*sql.DB, error)
	openWrite func() (*sql.DB, error)
	syncAll   func(*sql.DB) (int, error)
	query     func(*sql.DB, int64, int64, string) ([]store.ReportResult, error)
}

var Default = NewService(ServiceOptions{})

func NewService(options ServiceOptions) Service {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.OpenRead == nil {
		options.OpenRead = store.OpenReadOnly
	}
	if options.OpenWrite == nil {
		options.OpenWrite = store.Open
	}
	if options.SyncAll == nil {
		options.SyncAll = store.SyncAll
	}
	if options.Query == nil {
		options.Query = store.GetReport
	}
	return Service{
		now: options.Now, openRead: options.OpenRead, openWrite: options.OpenWrite,
		syncAll: options.SyncAll, query: options.Query,
	}
}

// SyncInput asks the service to refresh the index before reading it. It is
// separate from Input because it is a caller's operational choice, not part of
// what the report is about.
type SyncInput struct {
	SyncBeforeRead bool
}

// Daily builds one day's report.
func (service Service) Daily(
	ctx context.Context,
	call application.Call,
	input Input,
	sync SyncInput,
) (Report, error) {
	if ctx == nil {
		return Report{}, invalid("context", nil, "report context is required")
	}
	if err := call.Validate(); err != nil {
		return Report{}, err
	}
	agent, err := normalizeAgent(input.Agent)
	if err != nil {
		return Report{}, err
	}
	start, end, err := service.dateRange(input.Date)
	if err != nil {
		return Report{}, err
	}
	if err := ctx.Err(); err != nil {
		return Report{}, unavailable("read daily report", err)
	}

	db, err := service.open(sync.SyncBeforeRead)
	if err != nil {
		return Report{}, err
	}
	defer db.Close()
	if sync.SyncBeforeRead {
		if _, err := service.syncAll(db); err != nil {
			return Report{}, unavailable("sync before reading the daily report", err)
		}
	}
	rows, err := service.query(db, start.Unix(), end.Unix(), agent)
	if err != nil {
		return Report{}, unavailable("read daily report", err)
	}
	return Report{
		Date:   start.Format("2006-01-02"),
		Agents: sections(rows, input.Verbose),
	}, nil
}

// dateRange resolves the requested day. "today" and "yesterday" are relative to
// the service clock rather than to config's, so a test can pin the day.
func (service Service) dateRange(raw string) (time.Time, time.Time, error) {
	if raw == "" {
		raw = service.now().In(config.Loc).Format("2006-01-02")
	}
	start, end, err := config.DateRange(raw)
	if err != nil {
		return time.Time{}, time.Time{}, invalid("date", raw,
			fmt.Sprintf("invalid date: %s (use today, yesterday, or YYYY-MM-DD)", raw))
	}
	return start, end, nil
}

func (service Service) open(syncBeforeRead bool) (*sql.DB, error) {
	open := service.openRead
	if syncBeforeRead {
		open = service.openWrite
	}
	db, err := open()
	if err != nil {
		return nil, unavailable("open session index", err)
	}
	if db == nil {
		return nil, unavailable("open session index", errors.New("database opener returned nil"))
	}
	return db, nil
}

// sections groups the day's sessions by agent, dropping the ones with nothing
// readable in them.
func sections(rows []store.ReportResult, verbose bool) []AgentSection {
	grouped := map[string][]store.ReportResult{}
	for _, row := range rows {
		grouped[row.Agent] = append(grouped[row.Agent], row)
	}
	var out []AgentSection
	for _, agent := range agentOrder {
		sessions := make([]Session, 0, len(grouped[agent]))
		for _, row := range grouped[agent] {
			session, ok := session(row, verbose)
			if !ok {
				continue
			}
			sessions = append(sessions, session)
		}
		if len(sessions) == 0 {
			continue
		}
		out = append(out, AgentSection{
			Agent: agent, DisplayName: store.AgentDisplayName(agent), Sessions: sessions,
		})
	}
	return out
}

// session converts one row and reports whether it is worth listing.
//
// A session with no readable prompt and no tool call left nothing behind but a
// row in the index; listing it would pad the report with lines that say nothing.
// A summary rescues such a session only in verbose mode, where the report is a
// transcript rather than a digest.
func session(row store.ReportResult, verbose bool) (Session, bool) {
	prompts := readablePrompts(row.Inputs)
	summary := visible(row.Summary)
	if summary == "" && len(prompts) > 0 {
		summary = prompts[0]
	}
	if len(prompts) == 0 && (len(row.Tools) == 0 || (!verbose && summary == "")) {
		return Session{}, false
	}
	converted := Session{
		Project: row.Project, ShortID: row.ShortID, Summary: summary,
		Prompts: prompts, Tools: row.Tools,
	}
	for _, count := range row.Tools {
		converted.ToolCalls += count
	}
	if verbose {
		converted.Exchanges = exchanges(row.Inputs, row.Outputs)
	}
	return converted, true
}

// readablePrompts is what a person actually typed. A transcript input can be
// entirely harness scaffolding — a tool result, a system reminder — and those
// carry no prompt at all once the visible text is extracted.
func readablePrompts(inputs []string) []string {
	var prompts []string
	for _, input := range inputs {
		if text := visible(input); text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts
}

// exchanges pairs each readable prompt with the reply at the same position. The
// pairing is positional because that is the only relationship the index records;
// a prompt with no reply at its index is reported alone rather than dropped.
func exchanges(inputs, outputs []string) []Exchange {
	var out []Exchange
	for index, input := range inputs {
		question := visible(input)
		if question == "" {
			continue
		}
		exchange := Exchange{Question: question}
		if index < len(outputs) {
			exchange.Answer = visible(outputs[index])
		}
		out = append(out, exchange)
	}
	return out
}

func visible(text string) string { return parser.VisibleUserText(text) }

func normalizeAgent(raw string) (string, error) {
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
