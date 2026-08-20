package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

const (
	ApprovalPending  = "pending"
	ApprovalApproved = "approved"
	ApprovalRunning  = "running"
	ApprovalDone     = "done"
	ApprovalDenied   = "denied"
	ApprovalExpired  = "expired"
)

// ApprovalTextLimit caps the stored message preview. The approval card and the
// notification banner each render a few lines; anything beyond this is never
// read, and the body is somebody's message rather than ATM's own data.
const ApprovalTextLimit = 400

// ErrApprovalPending reports that an identical command already has a pending
// request, so the caller should attach to it rather than raise a second one.
var ErrApprovalPending = errors.New("an identical request is already pending")

// ErrApprovalRunClaimLost means another owner changed an approved row before
// this caller could claim execution. It is the one expected CAS miss; database
// and driver failures remain distinguishable infrastructure errors.
var ErrApprovalRunClaimLost = errors.New("approval execution claim was lost")

// Approval is one attempt by an agent to run a command that reaches somebody
// else, and the decision made about it.
//
// The lifecycle is pending → approved → running → done, with denied and expired
// as alternative terminals. 'running' is terminal for automation too: a gate
// that dies between running and done leaves no evidence of whether the command
// took effect, and nothing may retry such a row.
type Approval struct {
	ID string `json:"id"`
	// DedupKey identifies the command; ID identifies the request. Keeping them
	// separate is what lets a denial and a later approval of the same command
	// both exist as records.
	DedupKey string   `json:"dedup_key"`
	Tool     string   `json:"tool"`
	RuleID   string   `json:"rule_id,omitempty"`
	RealBin  string   `json:"real_bin"`
	Argv     []string `json:"argv"`
	CWD      string   `json:"cwd,omitempty"`
	EnvAgent string   `json:"env_agent,omitempty"`
	Label    string   `json:"label,omitempty"`

	PreviewTarget string `json:"preview_target,omitempty"`
	PreviewTitle  string `json:"preview_title,omitempty"`
	PreviewBody   string `json:"preview_body,omitempty"`

	Status     string `json:"status"`
	StdinPiped bool   `json:"stdin_piped,omitempty"`
	// GatePID is diagnostic only and must never decide anything: the request
	// outlives the process by long enough for the operating system to reuse the
	// number. GateDeadline is the ownership boundary instead.
	GatePID      int   `json:"gate_pid,omitempty"`
	GateDeadline int64 `json:"gate_deadline,omitempty"`
	AttachCount  int   `json:"attach_count"`
	RequestedAt  int64 `json:"requested_at"`
	ExpiresAt    int64 `json:"expires_at"`

	DecidedAt *int64 `json:"decided_at,omitempty"`
	DecidedBy string `json:"decided_by,omitempty"`
	Reason    string `json:"reason,omitempty"`
	RanBy     string `json:"ran_by,omitempty"`
	ExitCode  *int   `json:"exit_code,omitempty"`
	Output    string `json:"output,omitempty"`

	// Effective is derived on read, never stored. Read commands open the database
	// query_only, so a pending row past its expiry cannot be rewritten at the
	// moment it is listed — it is reported as expired and settled for real by the
	// next decision that touches it.
	Effective string `json:"effective_status,omitempty"`
}

const approvalColumns = `id,dedup_key,tool,rule_id,real_bin,argv,cwd,env_agent,label,` +
	`preview_target,preview_title,preview_body,status,stdin_piped,gate_pid,gate_deadline,` +
	`attach_count,requested_at,expires_at,decided_at,decided_by,reason,ran_by,exit_code,output`

// EffectiveStatus reports what a reader should believe right now. A pending row
// past its expiry is already dead even though no writer has said so yet.
func (a Approval) EffectiveStatus(now int64) string {
	if a.Status == ApprovalPending && a.ExpiresAt > 0 && a.ExpiresAt < now {
		return ApprovalExpired
	}
	return a.Status
}

// GateOwnsExecution reports whether the waiting gate process still holds the
// right to run the command. Deliberately a clock comparison and not a liveness
// check: a gate killed by its agent's timeout stops owning execution at the
// deadline it promised, whether or not its PID has been recycled by then.
func (a Approval) GateOwnsExecution(now int64) bool {
	return a.GateDeadline > 0 && now < a.GateDeadline
}

// ApprovalDedupKey identifies a command: same tool, same arguments, same working
// directory. It is what makes an agent's retry attach to the pending request
// instead of raising a second one.
func ApprovalDedupKey(tool string, argv []string, cwd string) string {
	parts := make([]string, 0, len(argv)+2)
	parts = append(parts, tool, cwd)
	parts = append(parts, argv...)
	hash := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(hash[:8])
}

// ApprovalID identifies a request. The gate's pid is folded in so that a command
// denied and legitimately re-requested within the same second still gets its own
// row rather than colliding with the record of the denial.
func ApprovalID(now int64, dedupKey string, pid int) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%d", now, dedupKey, pid)))
	return "ap_" + hex.EncodeToString(hash[:5])
}

// TruncateApprovalText caps stored and transmitted message text at one limit, so
// the database, the socket envelope and the banner never disagree about how much
// of a message the user was shown before deciding.
func TruncateApprovalText(value string) string {
	runes := []rune(value)
	if len(runes) <= ApprovalTextLimit {
		return value
	}
	return string(runes[:ApprovalTextLimit]) + "…"
}

// CreateApproval inserts a pending request. The partial unique index on
// dedup_key WHERE status='pending' is the claim: a retrying agent cannot create
// a second row, and gets ErrApprovalPending so it attaches to the first.
func CreateApproval(db *sql.DB, a Approval) (Approval, error) {
	a.Tool = strings.TrimSpace(a.Tool)
	a.RealBin = strings.TrimSpace(a.RealBin)
	if a.Tool == "" {
		return Approval{}, fmt.Errorf("approval tool is required")
	}
	if a.RealBin == "" {
		return Approval{}, fmt.Errorf("approval real binary is required")
	}
	if len(a.Argv) == 0 {
		return Approval{}, fmt.Errorf("approval argv is required")
	}
	if a.RequestedAt == 0 {
		a.RequestedAt = time.Now().In(config.Loc).Unix()
	}
	if a.ExpiresAt <= a.RequestedAt {
		return Approval{}, fmt.Errorf("approval must expire after it is requested")
	}
	if a.DedupKey == "" {
		a.DedupKey = ApprovalDedupKey(a.Tool, a.Argv, a.CWD)
	}
	if a.ID == "" {
		a.ID = ApprovalID(a.RequestedAt, a.DedupKey, a.GatePID)
	}
	a.Status = ApprovalPending
	if a.AttachCount <= 0 {
		a.AttachCount = 1
	}
	a.PreviewTarget = TruncateApprovalText(a.PreviewTarget)
	a.PreviewTitle = TruncateApprovalText(a.PreviewTitle)
	a.PreviewBody = TruncateApprovalText(a.PreviewBody)

	argv, err := json.Marshal(a.Argv)
	if err != nil {
		return Approval{}, err
	}
	_, err = db.Exec(`INSERT INTO approvals
		(id,dedup_key,tool,rule_id,real_bin,argv,cwd,env_agent,label,
		 preview_target,preview_title,preview_body,status,stdin_piped,gate_pid,gate_deadline,
		 attach_count,requested_at,expires_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.DedupKey, a.Tool, a.RuleID, a.RealBin, string(argv), a.CWD, a.EnvAgent, a.Label,
		a.PreviewTarget, a.PreviewTitle, a.PreviewBody, a.Status, boolInt(a.StdinPiped),
		a.GatePID, a.GateDeadline, a.AttachCount, a.RequestedAt, a.ExpiresAt)
	if err != nil {
		// The driver names the column, not the index, so both forms are matched:
		// which one appears depends on how the constraint was violated.
		if strings.Contains(err.Error(), "idx_approvals_pending_dedup") ||
			strings.Contains(err.Error(), "approvals.dedup_key") {
			return Approval{}, ErrApprovalPending
		}
		return Approval{}, err
	}
	a.Effective = a.Status
	return a, nil
}

func GetApproval(db *sql.DB, id string, now int64) (*Approval, error) {
	var approval Approval
	err := scanApproval(db.QueryRow(`SELECT `+approvalColumns+` FROM approvals WHERE id=?`, id), &approval)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	approval.Effective = approval.EffectiveStatus(now)
	return &approval, nil
}

// PendingApprovalByDedup finds the live request an identical command should
// attach to. A pending row past its expiry is deliberately not returned: the
// retrying agent gets a fresh request rather than inheriting a dead one.
func PendingApprovalByDedup(db *sql.DB, dedupKey string, now int64) (*Approval, error) {
	var approval Approval
	err := scanApproval(db.QueryRow(`SELECT `+approvalColumns+` FROM approvals
		WHERE dedup_key=? AND status=? AND expires_at>=?`, dedupKey, ApprovalPending, now), &approval)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	approval.Effective = approval.EffectiveStatus(now)
	return &approval, nil
}

// RecentDeniedApproval finds a denial still inside its cooldown, so a retrying
// agent fails fast on a decision the user already made instead of raising the
// same banner again. The cooldown expires so that a command denied once is not
// denied forever.
func RecentDeniedApproval(db *sql.DB, dedupKey string, since int64) (*Approval, error) {
	var approval Approval
	err := scanApproval(db.QueryRow(`SELECT `+approvalColumns+` FROM approvals
		WHERE dedup_key=? AND status=? AND COALESCE(decided_at,0)>=?
		ORDER BY COALESCE(decided_at,0) DESC LIMIT 1`, dedupKey, ApprovalDenied, since), &approval)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	approval.Effective = approval.Status
	return &approval, nil
}

// ListApprovals filters on effective status, computed in SQL so that an expired
// pending row does not show up under --status pending on a read-only connection.
func ListApprovals(db *sql.DB, statuses []string, now int64, limit int) ([]Approval, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT ` + approvalColumns + ` FROM approvals`
	args := []any{}
	if len(statuses) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
		query += ` WHERE (CASE WHEN status='` + ApprovalPending + `' AND expires_at<?
			THEN '` + ApprovalExpired + `' ELSE status END) IN (` + placeholders + `)`
		args = append(args, now)
		for _, status := range statuses {
			args = append(args, status)
		}
	}
	query += ` ORDER BY requested_at DESC,id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	approvals := []Approval{}
	for rows.Next() {
		var approval Approval
		if err := scanApproval(rows, &approval); err != nil {
			return nil, err
		}
		approval.Effective = approval.EffectiveStatus(now)
		approvals = append(approvals, approval)
	}
	return approvals, rows.Err()
}

// AttachApproval records that another invocation of the same command is now
// waiting on this request, and hands it the ownership window. A rising
// attach_count means the wait budget is too short for how this agent behaves.
func AttachApproval(db *sql.DB, id string, gatePID int, gateDeadline int64) error {
	result, err := db.Exec(`UPDATE approvals
		SET attach_count=attach_count+1,gate_pid=?,gate_deadline=?
		WHERE id=? AND status=?`, gatePID, gateDeadline, id, ApprovalPending)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("approval %s is no longer pending", id)
	}
	return nil
}

// ReleaseApprovalGate hands ownership back when a gate stops waiting cleanly, so
// a later approval executes immediately instead of waiting out a deadline the
// gate is no longer honouring. Not conditional on rows affected: releasing a
// request that has meanwhile been decided is the expected race, not an error.
func ReleaseApprovalGate(db *sql.DB, id string) error {
	_, err := db.Exec(`UPDATE approvals SET gate_pid=0,gate_deadline=0
		WHERE id=? AND status=?`, id, ApprovalPending)
	return err
}

// ApproveApproval flips pending → approved. An expired request is refused and
// settled as expired in the same transaction, so no later caller can find it
// still pending and act on a decision the user never had the chance to make.
func ApproveApproval(db *sql.DB, id string, now int64, by, reason string) error {
	return decideApproval(db, id, ApprovalApproved, now, by, reason)
}

// DenyApproval records a refusal. Unlike approval it is accepted past the expiry
// too: the user saying no is worth recording whenever they say it, and the
// denial is what makes a retrying agent fail fast.
func DenyApproval(db *sql.DB, id string, now int64, by, reason string) error {
	return decideApproval(db, id, ApprovalDenied, now, by, reason)
}

func decideApproval(db *sql.DB, id, decision string, now int64, by, reason string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var status string
	var expiresAt int64
	err = tx.QueryRow(`SELECT status,expires_at FROM approvals WHERE id=?`, id).Scan(&status, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("approval not found: %s", id)
	}
	if err != nil {
		return err
	}
	if status != ApprovalPending {
		return fmt.Errorf("approval %s is already %s", id, status)
	}
	if decision == ApprovalApproved && expiresAt > 0 && expiresAt < now {
		if _, err := tx.Exec(`UPDATE approvals SET status=? WHERE id=? AND status=?`,
			ApprovalExpired, id, ApprovalPending); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return fmt.Errorf("approval %s expired at %s", id,
			time.Unix(expiresAt, 0).In(config.Loc).Format("15:04"))
	}
	result, err := tx.Exec(`UPDATE approvals SET status=?,decided_at=?,decided_by=?,reason=?
		WHERE id=? AND status=?`, decision, now, by, reason, id, ApprovalPending)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("approval %s is no longer pending", id)
	}
	return tx.Commit()
}

// ClaimApprovalRun is the hard claim on execution: approved → running, and only
// one caller can win. This is what stops a waiting gate and the app from both
// running one approved command and sending the same message twice.
func ClaimApprovalRun(db *sql.DB, id, ranBy string, pid int) error {
	result, err := db.Exec(`UPDATE approvals SET status=?,ran_by=?,gate_pid=?
		WHERE id=? AND status=?`, ApprovalRunning, ranBy, pid, id, ApprovalApproved)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: %s is not awaiting execution", ErrApprovalRunClaimLost, id)
	}
	return nil
}

// FinishApproval records the outcome, and is only ever called by whoever won the
// claim. There is no counterpart that returns a running row to approved: if this
// never runs, whether the command took effect is genuinely unknown.
func FinishApproval(db *sql.DB, id string, exitCode int, output string) error {
	result, err := db.Exec(`UPDATE approvals SET status=?,exit_code=?,output=?
		WHERE id=? AND status=?`, ApprovalDone, exitCode, TruncateApprovalText(output),
		id, ApprovalRunning)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("approval %s is not running", id)
	}
	return nil
}

type approvalScanner interface {
	Scan(dest ...any) error
}

func scanApproval(scanner approvalScanner, approval *Approval) error {
	var argv string
	var stdinPiped int
	err := scanner.Scan(&approval.ID, &approval.DedupKey, &approval.Tool, &approval.RuleID,
		&approval.RealBin, &argv, &approval.CWD, &approval.EnvAgent, &approval.Label,
		&approval.PreviewTarget, &approval.PreviewTitle, &approval.PreviewBody,
		&approval.Status, &stdinPiped, &approval.GatePID, &approval.GateDeadline,
		&approval.AttachCount, &approval.RequestedAt, &approval.ExpiresAt,
		&approval.DecidedAt, &approval.DecidedBy, &approval.Reason, &approval.RanBy,
		&approval.ExitCode, &approval.Output)
	if err != nil {
		return err
	}
	approval.StdinPiped = stdinPiped != 0
	return json.Unmarshal([]byte(argv), &approval.Argv)
}
