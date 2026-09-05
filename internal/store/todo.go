package store

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zane-byte-dev/atm/internal/config"
)

type TodoLink struct {
	URL      string `json:"url" yaml:"url"`
	Kind     string `json:"kind,omitempty" yaml:"kind,omitempty"`
	Title    string `json:"title,omitempty" yaml:"title,omitempty"`
	Relation string `json:"relation,omitempty" yaml:"relation,omitempty"`
}

// TodoImage is one locally managed image attachment. StoredName is the private
// filename under ~/.atm/todos/assets/<todo-id>; Path is derived at read time so
// moving ATM_HOME does not bake an obsolete absolute path into SQLite.
type TodoImage struct {
	Name       string `json:"name" yaml:"name"`
	Path       string `json:"path" yaml:"path"`
	MediaType  string `json:"media_type" yaml:"media_type"`
	SizeBytes  int64  `json:"size_bytes" yaml:"size_bytes"`
	StoredName string `json:"-" yaml:"-"`
}

type Todo struct {
	// ID is the complete identifier, of the form "t104". Todos have no short/long
	// distinction, so there is no short_id here — unlike a session, whose id is a
	// UUID and whose short_id is the prefix humans type. Scripts that read
	// short_id off a todo get nothing back.
	ID               string      `json:"id"`
	Title            string      `json:"title"`
	Description      string      `json:"description,omitempty"`
	Priority         string      `json:"priority"`
	Status           string      `json:"status"`
	Project          string      `json:"project,omitempty"`
	Tags             []string    `json:"tags,omitempty"`
	WakeCondition    string      `json:"wake_condition,omitempty"`
	ReviewAt         string      `json:"review_at,omitempty"`
	MaintenanceLimit int         `json:"maintenance_limit,omitempty"`
	DependsOn        []string    `json:"depends_on,omitempty"`
	Links            []TodoLink  `json:"links,omitempty"`
	Images           []TodoImage `json:"images,omitempty"`
	Created          string      `json:"created"`
	Source           string      `json:"source,omitempty"`
	// Creator is who filed the todo: "me", "collect", or an agent name. See
	// todo_creator.go for the vocabulary and why it is separate from Source.
	// Empty on every todo that predates the field.
	Creator      string  `json:"creator,omitempty"`
	Closed       *string `json:"closed"`
	ClosedReason *string `json:"closed_reason"`
	OnDone       string  `json:"on_done,omitempty"`
	StartTS      *int64  `json:"start_ts,omitempty"`
	DoneTS       *int64  `json:"done_ts,omitempty"`
}

// TodoFile is an in-memory snapshot of the live todos. baseline records the rows
// as they were read so writeTodos can persist only what changed; a file built by
// hand (rather than loaded) has no baseline and writes as all-inserts.
type TodoFile struct {
	Items    []Todo `json:"items"`
	baseline map[string]todoRow
	// archived maps the ID of every todo that has left the working set to its
	// closing status. They are not in Items, but they still exist: their IDs
	// must not be reused, progress notes may still refer to them, and a
	// dependency on an archived-and-done todo is satisfied, not broken.
	archived map[string]string
}

// ArchivedTodo is a todo that has left the working set. Only the scalar columns
// are read back — the archive views list and search, they do not act.
type ArchivedTodo struct {
	Todo
	ArchivedAt int64 `json:"archived_at"`
}

const (
	TodoStatusOpen       = "open"
	TodoStatusInProgress = "in_progress"
	TodoStatusReview     = "review"
	TodoStatusDone       = "done"

	TodoTagMaintenance = "maintenance"
)

func TodoIsActive(t Todo) bool {
	return t.Status != TodoStatusDone
}

func TodoHasTag(t Todo, tag string) bool {
	for _, value := range t.Tags {
		if value == tag {
			return true
		}
	}
	return false
}

func AddTodoTag(t *Todo, tag string) {
	tag = strings.TrimSpace(tag)
	if tag == "" || TodoHasTag(*t, tag) {
		return
	}
	t.Tags = append(t.Tags, tag)
	sort.Strings(t.Tags)
}

// LoadTodosReadOnly reads the live todos without creating or migrating the
// database. Writes go through UpdateWorkState, which loads its own snapshot
// under the write lock.
func LoadTodosReadOnly() (*TodoFile, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return loadTodos(db)
}

// LoadArchivedTodos reads the todos that have left the working set, most
// recently archived first.
func LoadArchivedTodos() ([]ArchivedTodo, error) {
	db, err := OpenReadOnly()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT id,title,description,priority,status,project,created,source,creator,
		closed,closed_reason,archived_at FROM todos
		WHERE archived_at IS NOT NULL
		-- Newest first. IDs sort numerically, so t9 does not outrank t10.
		ORDER BY archived_at DESC, closed DESC, CAST(SUBSTR(id,2) AS INTEGER) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	archived := []ArchivedTodo{}
	for rows.Next() {
		var todo ArchivedTodo
		if err := rows.Scan(&todo.ID, &todo.Title, &todo.Description, &todo.Priority, &todo.Status,
			&todo.Project, &todo.Created, &todo.Source, &todo.Creator, &todo.Closed,
			&todo.ClosedReason, &todo.ArchivedAt); err != nil {
			return nil, err
		}
		archived = append(archived, todo)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	images, err := loadGroupedTodoImages(db)
	if err != nil {
		return nil, err
	}
	links, err := loadGroupedTodoLinks(db)
	if err != nil {
		return nil, err
	}
	for index := range archived {
		archived[index].Images = images[archived[index].ID]
		archived[index].Links = links[archived[index].ID]
	}
	return archived, nil
}

// NextTodoID returns an unused ID. Archived todos are counted even though they
// are not in Items: reusing their ID would attach an old document, old progress
// notes, and old references to a brand new task.
func NextTodoID(tf *TodoFile) string {
	highest := 0
	consider := func(id string) {
		if !strings.HasPrefix(id, "t") {
			return
		}
		if n, err := strconv.Atoi(id[1:]); err == nil && n > highest {
			highest = n
		}
	}
	for _, todo := range tf.Items {
		consider(todo.ID)
	}
	for id := range tf.archived {
		consider(id)
	}
	return fmt.Sprintf("t%d", highest+1)
}

// ArchivedStatus reports the closing status of an archived todo. The second
// result distinguishes "archived" from "never existed".
func ArchivedStatus(tf *TodoFile, id string) (string, bool) {
	status, archived := tf.archived[NormalizeTodoID(id)]
	return status, archived
}

// TodoNotFoundError explains why an ID resolves to nothing in the working set.
// An archived todo still exists, so saying so — and how to get it back — beats
// claiming it was never there.
func TodoNotFoundError(tf *TodoFile, id string) error {
	if status, archived := ArchivedStatus(tf, id); archived {
		// The suggested command names the canonical id, not whatever spelling was
		// typed: it is meant to be pasted, and `atm todo restore #275` would have
		// worked but reads like a typo.
		canonical := NormalizeTodoID(id)
		return fmt.Errorf("todo %s is archived (%s): run `atm todo restore %s` to work on it again, "+
			"or `atm todo list --status archived` to read it", canonical, status, canonical)
	}
	// Echoes what was typed rather than the normalised form: the reader is looking
	// for their own input, and "todo not found: t7" after typing "007" reads as a
	// different bug.
	return fmt.Errorf("todo not found: %s", id)
}

// todoIDPattern matches the ways a todo gets referred to in practice. IDs
// themselves are always `t<digits>` (see NextTodoID), but people and agents
// write `#t65` when quoting a chat, `65` when reading it off a list, and `T65`
// when the shell capitalised it. All three used to be "todo not found".
var todoIDPattern = regexp.MustCompile(`^#?[tT]?([0-9]+)$`)

// LooksLikeTodoID reports whether a string is a todo reference in any of its
// written forms, without needing the todo to exist. Callers use it to tell an id
// apart from prose when a positional argument could be either.
func LooksLikeTodoID(s string) bool {
	return todoIDPattern.MatchString(strings.TrimSpace(s))
}

// NormalizeTodoID turns the ways a todo is written into the one way it is
// stored. An input that is not a todo reference at all comes back unchanged, so
// callers still get "todo not found: <what they typed>" rather than a confusing
// rewrite of it.
func NormalizeTodoID(id string) string {
	trimmed := strings.TrimSpace(id)
	match := todoIDPattern.FindStringSubmatch(trimmed)
	if match == nil {
		return trimmed
	}
	// Leading zeros would make "t007" and "t7" different keys.
	digits := strings.TrimLeft(match[1], "0")
	if digits == "" {
		return trimmed
	}
	return "t" + digits
}

// FindTodo resolves an id in any of its written forms. Every lookup in the
// codebase funnels through here — including work.Transaction.Todo — so this is
// the one place the normalisation has to happen.
func FindTodo(tf *TodoFile, id string) *Todo {
	wanted := NormalizeTodoID(id)
	for i := range tf.Items {
		if tf.Items[i].ID == wanted {
			return &tf.Items[i]
		}
	}
	return nil
}

func Today() string {
	return time.Now().In(config.Loc).Format("2006-01-02")
}

func TodoDocDir() string {
	return filepath.Join(config.AtmDir, "todos")
}

func TodoDocPath(id string) string {
	return filepath.Join(TodoDocDir(), id+".md")
}

func TodoDocExists(id string) bool {
	_, err := os.Stat(TodoDocPath(id))
	return err == nil
}

func ReadTodoDoc(id string) (string, error) {
	data, err := os.ReadFile(TodoDocPath(id))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func InitTodoDoc(t *Todo) (string, error) {
	path := TodoDocPath(t.ID)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("doc already exists: %s", path)
	}
	if err := os.MkdirAll(TodoDocDir(), 0755); err != nil {
		return "", err
	}

	desc := "待补充"
	if t.Description != "" {
		desc = t.Description
	}
	project := ""
	if t.Project != "" {
		project = t.Project
	}

	content := fmt.Sprintf(`# %s

- **ID**: %s
- **状态**: %s
- **优先级**: %s
- **标签**: %s
- **项目**: %s
- **创建**: %s
- **创建者**: %s

## 需求

%s

%s

## 分析

待补充

## 进展

## 备注
`, t.Title, t.ID, todoStatusDisplay(t.Status), t.Priority, strings.Join(t.Tags, ", "), project, t.Created,
		TodoCreatorDocLabel(t.Creator), todoDocRequirementNotice, desc)

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return path, err
	}
	return path, SyncTodoDocMetadata(t)
}

// EnsureTodoDoc creates the markdown task card when missing, otherwise only
// refreshes generated metadata. GUI-created todos and bare `todo add` used to
// leave the file out; agent handoff always starts with `todo doc`, so a missing
// card reads like a failed bind even though the binding row itself is fine.
func EnsureTodoDoc(t *Todo) (string, error) {
	if t == nil {
		return "", fmt.Errorf("todo is nil")
	}
	if TodoDocExists(t.ID) {
		if err := SyncTodoDocMetadata(t); err != nil {
			return TodoDocPath(t.ID), err
		}
		return TodoDocPath(t.ID), nil
	}
	return InitTodoDoc(t)
}

// Generated section names are shared with ValidateTodoLogMessage so a manual
// append cannot report success and then disappear on the next projection sync.
const todoDocGeneratedSection = "需求"
const todoDocPlanGeneratedSection = "执行计划"

// todoDocRequirementNotice labels the 需求 section as generated. The database is
// the single source of truth for Description, and every metadata sync overwrites
// that section from it, so an unmarked section would silently eat hand edits.
const todoDocRequirementNotice = "<!-- 由 atm 从 todo description 生成,手工编辑会在下次同步时被覆盖;请用 `atm todo edit <id> --desc` 修改 -->"

// SyncTodoDocMetadata mirrors the structured todo into an existing markdown task
// card. Analysis, progress, and notes remain user-maintained; the title, the
// metadata header, and the 需求 section are generated from the database row.
func SyncTodoDocMetadata(t *Todo) error {
	path := TodoDocPath(t.ID)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	content := setTodoDocTitle(string(data), t.Title)
	content = setTodoDocField(content, "ID", t.ID, "")
	content = setTodoDocField(content, "状态", todoStatusDisplay(t.Status), "ID")
	content = setTodoDocField(content, "优先级", t.Priority, "状态")
	content = setTodoDocField(content, "注意力", "", "优先级")
	tags := strings.Join(t.Tags, ", ")
	content = setTodoDocField(content, "标签", tags, "优先级")
	// 领域 (the work/personal lane) was retired in schema v35. Clearing it drops
	// the line from documents written before then, the same way 注意力 above is
	// cleared rather than left to linger.
	content = setTodoDocField(content, "领域", "", "")
	projectAfter := "标签"
	if tags == "" {
		projectAfter = "优先级"
	}
	content = setTodoDocField(content, "项目", t.Project, projectAfter)
	content = setTodoDocField(content, "创建", t.Created, "项目")
	content = setTodoDocField(content, "创建者", TodoCreatorDocLabel(t.Creator), "创建")

	closed := ""
	if t.Closed != nil {
		closed = *t.Closed
	}
	content = setTodoDocField(content, "完结日期", closed, "状态")

	reason := ""
	if t.ClosedReason != nil {
		reason = *t.ClosedReason
	}
	content = setTodoDocField(content, "完结原因", reason, "完结日期")

	requirement := strings.TrimSpace(t.Description)
	if requirement == "" {
		requirement = "待补充"
	}
	content = setTodoDocSection(content, todoDocGeneratedSection, todoDocRequirementNotice+"\n\n"+requirement)

	return os.WriteFile(path, []byte(content), 0644)
}

func setTodoDocTitle(content, title string) string {
	titleLine := "# " + strings.TrimSpace(title)
	if newline := strings.IndexByte(content, '\n'); newline >= 0 {
		if strings.HasPrefix(content[:newline], "# ") {
			return titleLine + content[newline:]
		}
	} else if strings.HasPrefix(content, "# ") {
		return titleLine
	}
	return titleLine + "\n\n" + strings.TrimLeft(content, "\n")
}

// todoDocSections is the card's own skeleton, in order. Section boundaries are
// resolved against this set rather than against any "## " line, because 需求
// holds the todo description verbatim: a description detailed enough to carry its
// own headings would otherwise appear to end the section at its first one. That
// cost twice over — todoDocSection read back only the generated notice and
// reported the card as drifting from the database, and the writer below replaced
// only the text above that heading, leaving the rest of the old description in
// place and adding another copy of the new one on every metadata sync.
var todoDocSections = []string{"需求", "分析", "执行计划", "进展", "备注"}

func isTodoDocSection(name string) bool {
	for _, section := range todoDocSections {
		if name == section {
			return true
		}
	}
	return false
}

// todoDocSectionBody locates one card section's body: the offset just past its
// heading line, and the offset where the next card section starts (end of content
// if it is the last). Fenced code blocks are skipped, so a "## " inside ``` is
// content — a requirement quoting markdown is still one requirement.
func todoDocSectionBody(content, section string) (bodyStart, bodyEnd int, found bool) {
	offset := 0
	inFence := false
	for _, line := range strings.SplitAfter(content, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "```"):
			inFence = !inFence
		case !inFence && strings.HasPrefix(line, "## "):
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "## "))
			if found {
				if isTodoDocSection(name) {
					return bodyStart, offset, true
				}
			} else if name == section {
				found = true
				bodyStart = offset + len(line)
			}
		}
		offset += len(line)
	}
	if found {
		return bodyStart, len(content), true
	}
	return 0, 0, false
}

func setTodoDocSection(content, section, value string) string {
	value = strings.TrimSpace(value)
	bodyStart, bodyEnd, found := todoDocSectionBody(content, section)
	if !found {
		// Insert before the next known card section so adding a generated section
		// does not move 进展 or 备注 away from their stable positions.
		sectionIndex := -1
		for index, candidate := range todoDocSections {
			if candidate == section {
				sectionIndex = index
				break
			}
		}
		if sectionIndex >= 0 {
			for _, candidate := range todoDocSections[sectionIndex+1:] {
				marker := "\n## " + candidate + "\n"
				if offset := strings.Index(content, marker); offset >= 0 {
					head := strings.TrimRight(content[:offset], "\n")
					tail := strings.TrimLeft(content[offset:], "\n")
					return head + "\n\n## " + section + "\n\n" + value + "\n\n" + tail
				}
			}
		}
		return strings.TrimRight(content, "\n") + "\n\n## " + section + "\n\n" + value + "\n"
	}
	head := strings.TrimRight(content[:bodyStart], "\n")
	tail := content[bodyEnd:]
	if strings.TrimSpace(tail) == "" {
		return head + "\n\n" + value + "\n"
	}
	return head + "\n\n" + value + "\n\n" + tail
}

// TodoPlanDocumentItem is the presentation-neutral subset needed to mirror a
// Work plan into the generated section of a Todo card.
type TodoPlanDocumentItem struct {
	Step   string
	Status string
}

// SyncTodoDocPlan replaces the generated execution-plan section while leaving
// requirements, analysis, progress and notes intact. The database plan stream
// remains authoritative; this Markdown is a repairable reader projection.
func SyncTodoDocPlan(todoID, explanation string, items []TodoPlanDocumentItem) error {
	path := TodoDocPath(todoID)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const notice = "<!-- 由 atm 从最新 plan revision 生成,手工编辑会在下次 plan set/doc 同步时被覆盖 -->"
	parts := []string{notice}
	if explanation = strings.TrimSpace(explanation); explanation != "" {
		quoted := strings.ReplaceAll(explanation, "\n", "\n> ")
		parts = append(parts, "> "+quoted)
	}
	if len(items) == 0 {
		parts = append(parts, "（空计划）")
	} else {
		lines := make([]string, 0, len(items))
		for _, item := range items {
			marker := " "
			switch item.Status {
			case "completed":
				marker = "x"
			case "in_progress":
				marker = ">"
			}
			step := strings.ReplaceAll(strings.TrimSpace(item.Step), "\n", "\n  ")
			lines = append(lines, fmt.Sprintf("- [%s] %s", marker, step))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	content := setTodoDocSection(string(data), todoDocPlanGeneratedSection, strings.Join(parts, "\n\n"))
	return os.WriteFile(path, []byte(content), 0644)
}

// todoStatusDisplay writes the Status line on a Todo's markdown card. The four
// labels are the same words the UI shows, so a card and the UI never describe
// one Todo differently.
func todoStatusDisplay(status string) string {
	switch status {
	case "open":
		return "open（待办）"
	case "in_progress":
		return "in_progress（进行中）"
	case "review":
		return "review（待验收）"
	case "done":
		return "done（已完成）"
	default:
		return status
	}
}

func setTodoDocField(content, label, value, afterLabel string) string {
	headerEnd := strings.Index(content, "\n## ")
	if headerEnd < 0 {
		headerEnd = len(content)
	}
	header, body := content[:headerEnd], content[headerEnd:]
	lines := strings.Split(header, "\n")
	prefix := "- **" + label + "**:"
	afterPrefix := "- **" + afterLabel + "**:"
	value = strings.Join(strings.Fields(value), " ")

	for i, line := range lines {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		if value == "" {
			lines = append(lines[:i], lines[i+1:]...)
		} else {
			lines[i] = prefix + " " + value
		}
		return strings.Join(lines, "\n") + body
	}

	if value == "" {
		return content
	}

	insertAt := len(lines)
	for i, line := range lines {
		if strings.HasPrefix(line, afterPrefix) {
			insertAt = i + 1
			break
		}
	}
	line := prefix + " " + value
	lines = append(lines, "")
	copy(lines[insertAt+1:], lines[insertAt:])
	lines[insertAt] = line
	return strings.Join(lines, "\n") + body
}

func AppendTodoLog(t *Todo, msg, section string) (string, error) {
	if section == "" {
		section = "进展"
	}
	if err := ValidateTodoLogMessage(msg, section); err != nil {
		return "", err
	}
	path := TodoDocPath(t.ID)

	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err := InitTodoDoc(t); err != nil {
			return "", err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	ts := time.Now().In(config.Loc).Format("2006-01-02 15:04")
	messageLines := strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n")
	entry := fmt.Sprintf("- [%s] %s\n", ts, messageLines[0])
	for _, line := range messageLines[1:] {
		entry += "  " + line + "\n"
	}

	content := string(data)
	marker := "## " + section
	idx := strings.Index(content, marker)
	if idx < 0 {
		content = strings.TrimRight(content, "\n") + "\n\n" + marker + "\n\n" + entry
	} else {
		afterMarker := idx + len(marker)
		rest := content[afterMarker:]
		nlIdx := strings.Index(rest, "\n")
		if nlIdx < 0 {
			content = content + "\n\n" + entry
		} else {
			insertAt := afterMarker + nlIdx + 1
			nextSection := strings.Index(content[insertAt:], "\n## ")
			if nextSection < 0 {
				content = strings.TrimRight(content, "\n") + "\n" + entry
			} else {
				insertPos := insertAt + nextSection
				before := strings.TrimRight(content[:insertPos], "\n")
				content = before + "\n" + entry + "\n" + content[insertPos:]
			}
		}
	}

	return entry, os.WriteFile(path, []byte(content), 0644)
}
