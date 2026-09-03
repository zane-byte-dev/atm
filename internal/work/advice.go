package work

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zane-byte-dev/atm/internal/application"
)

const maxAdviceReviews = 5

// Advice is an observation, never a lifecycle transition. Previous contains
// only the last successful comment observations, persisted by the desktop.
type AdviceInput struct {
	TodoID   string                  `json:"todo_id"`
	Previous []AdviceCommentBaseline `json:"previous,omitempty"`
}

type AdviceCommentBaseline struct {
	URL        string  `json:"url"`
	CheckedAt  string  `json:"checked_at"`
	CommentIDs []int64 `json:"comment_ids"`
}

type AdviceResult struct {
	TodoID    string         `json:"todo_id"`
	CheckedAt string         `json:"checked_at"`
	Summary   string         `json:"summary"`
	Reviews   []AdviceReview `json:"reviews"`
}

type AdviceReview struct {
	URL             string                 `json:"url"`
	Repo            string                 `json:"repo"`
	MRID            int64                  `json:"mr_id"`
	Title           string                 `json:"title"`
	State           string                 `json:"state"`
	StatusLabel     string                 `json:"status_label"`
	Suggestion      string                 `json:"suggestion"`
	Severity        string                 `json:"severity"`
	CommentCount    *int                   `json:"comment_count,omitempty"`
	UnresolvedCount *int                   `json:"unresolved_count,omitempty"`
	NewCommentCount *int                   `json:"new_comment_count,omitempty"`
	Comments        []AdviceComment        `json:"comments"`
	Baseline        *AdviceCommentBaseline `json:"baseline,omitempty"`
	Errors          []string               `json:"errors"`
}

type AdviceComment struct {
	ID        int64  `json:"id"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

type adviceReference struct {
	URL, Repo string
	ID        int64
}

var adviceURLPattern = regexp.MustCompile(`https?://[^\s<>"\x60\[\]（）。，；！？、]+`)
var adviceRepoSegment = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

func adviceReferences(text string) []adviceReference {
	refs := []adviceReference{}
	seen := map[string]bool{}
	for _, raw := range adviceURLPattern.FindAllString(text, -1) {
		u, err := url.Parse(strings.TrimRight(raw, ").,;!:}"))
		if err != nil || !strings.EqualFold(u.Host, "code.alibaba-inc.com") || u.User != nil {
			continue
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 4 || parts[len(parts)-2] != "codereview" {
			continue
		}
		id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		valid := true
		for _, segment := range parts[:len(parts)-2] {
			if !adviceRepoSegment.MatchString(segment) || segment == "." || segment == ".." || strings.HasPrefix(segment, "-") {
				valid = false
			}
		}
		if !valid {
			continue
		}
		repo := strings.Join(parts[:len(parts)-2], "/")
		canonical := fmt.Sprintf("https://code.alibaba-inc.com/%s/codereview/%d", repo, id)
		if !seen[canonical] {
			seen[canonical] = true
			refs = append(refs, adviceReference{URL: canonical, Repo: repo, ID: id})
		}
	}
	return refs
}

func (service Service) Advice(ctx context.Context, call application.Call, input AdviceInput) (AdviceResult, error) {
	_, todo, err := loadTodoForRead(ctx, call, input.TodoID)
	if err != nil {
		return AdviceResult{}, err
	}
	if len(input.Previous) > maxAdviceReviews {
		return AdviceResult{}, readInvalidArgument("too many comment baselines", "previous", len(input.Previous))
	}
	previous := map[string]AdviceCommentBaseline{}
	for _, baseline := range input.Previous {
		if len(baseline.CommentIDs) > 10000 {
			return AdviceResult{}, readInvalidArgument("too many comment IDs", "previous", len(baseline.CommentIDs))
		}
		if checked, err := time.Parse(time.RFC3339, baseline.CheckedAt); err == nil && !checked.After(time.Now()) {
			previous[baseline.URL] = baseline
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result := AdviceResult{TodoID: todo.ID, CheckedAt: time.Now().UTC().Format(time.RFC3339), Reviews: []AdviceReview{}}
	doc := inspectTodoDocument(todo.ID, 0)
	texts := []string{todo.Title, todo.Description, todo.Source}
	for _, link := range todo.Links {
		texts = append(texts, link.URL)
	}
	texts = append(texts, doc.Content)
	refs := adviceReferences(strings.Join(texts, "\n"))
	if len(refs) == 0 {
		result.Summary = "暂未找到可查询的 CR 链接。可在任务描述或动态中补充 Code 评审地址，再获取建议。"
		return result, nil
	}
	result.Summary = "根据 CR 最新状态和评论，建议下一步操作。"
	if len(refs) > maxAdviceReviews {
		result.Summary = fmt.Sprintf("找到 %d 个 CR，本次查询前 %d 个。", len(refs), maxAdviceReviews)
		refs = refs[:maxAdviceReviews]
	}
	run := service.AdviceRunner
	if run == nil {
		run = runAdviceA1
	}
	result.Reviews = make([]AdviceReview, len(refs))
	var group sync.WaitGroup
	for index, ref := range refs {
		group.Go(func() {
			var baseline *AdviceCommentBaseline
			if old, ok := previous[ref.URL]; ok {
				baseline = &old
			}
			result.Reviews[index] = queryAdviceReview(ctx, run, ref, baseline, result.CheckedAt, todo.Status)
		})
	}
	group.Wait()
	return result, nil
}

func adviceText(value string, limit int) string {
	text := []rune(strings.Join(strings.Fields(value), " "))
	if len(text) > limit {
		return string(text[:limit]) + "…"
	}
	return string(text)
}
