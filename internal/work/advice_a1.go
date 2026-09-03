package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AdviceRunner is the outbound port. The service constructs a fixed set of
// read-only a1 commands; task text is never executed or passed to a shell.
type AdviceRunner func(context.Context, []string) ([]byte, error)

type adviceMR struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

type adviceMRStatus struct {
	MRID         int64 `json:"mrId"`
	ReadyToMerge *bool `json:"readyToMerge"`
	Conflicted   bool  `json:"conflicted"`
}

type adviceMRComment struct {
	ID     int64  `json:"id"`
	Note   string `json:"note"`
	Author struct {
		Name string `json:"name"`
	} `json:"author"`
	Closed       *int   `json:"closed"`
	CreatedAt    string `json:"createdAt"`
	ParentNoteID int64  `json:"parentNoteId"`
	Path         string `json:"path"`
	IsDraft      bool   `json:"isDraft"`
	IsAISummary  bool   `json:"isAiSummary"`
}

func queryAdviceReview(ctx context.Context, run AdviceRunner, ref adviceReference, previous *AdviceCommentBaseline, checkedAt, todoStatus string) AdviceReview {
	result := AdviceReview{
		URL: ref.URL, Repo: ref.Repo, MRID: ref.ID, Title: fmt.Sprintf("CR %d", ref.ID),
		State: "unknown", StatusLabel: "状态未知", Severity: "warning",
		Suggestion: "暂时无法确认评审状态，请打开 CR 核对或重试。",
		Comments:   []AdviceComment{}, Errors: []string{}, Baseline: previous,
	}
	id := strconv.FormatInt(ref.ID, 10)
	commands := [][]string{
		{"repo", "mr", "view", id},
		{"repo", "mr", "status", id},
		{"repo", "mr", "comment", "list", "--mr", id, "--sort", "desc"},
	}
	var data [3][]byte
	var errs [3]error
	var group sync.WaitGroup
	for index, args := range commands {
		group.Go(func() {
			args = append(args, "--repo", ref.Repo, "--format", "json", "--no-update-check")
			data[index], errs[index] = run(ctx, args)
		})
	}
	group.Wait()

	var view struct {
		MR *adviceMR `json:"mergeRequest"`
	}
	if errs[0] == nil {
		errs[0] = json.Unmarshal(data[0], &view)
		if errs[0] == nil && (view.MR == nil || view.MR.ID != ref.ID || view.MR.State == "") {
			errs[0] = errors.New("评审详情返回不完整")
		}
	}
	var status adviceMRStatus
	if errs[1] == nil {
		errs[1] = json.Unmarshal(data[1], &status)
		if errs[1] == nil && (status.MRID != ref.ID || status.ReadyToMerge == nil) {
			errs[1] = errors.New("合并检查返回不完整")
		}
	}
	if errs[0] == nil {
		result.Title = adviceText(view.MR.Title, 160)
		switch strings.ToLower(view.MR.State) {
		case "merged":
			result.State, result.StatusLabel, result.Severity = "merged", "已合并", "success"
			result.Suggestion = "CR 已合并；若这条任务仅用于评审，核对后可完成任务。"
			if todoStatus == "done" {
				result.Suggestion = "CR 已合并，任务也已完成。"
			}
		case "closed":
			result.State, result.StatusLabel, result.Severity = "closed", "已关闭", "info"
			result.Suggestion = "CR 已关闭；确认是否撤销或已被其他 CR 替代，再决定如何处理任务。"
		case "opened", "open", "reopened":
			result.State, result.StatusLabel, result.Severity = "reviewing", "评审中", "info"
			result.Suggestion = "继续查看评审意见，等待评审与合并检查完成。"
			if errs[1] != nil {
				result.Suggestion = "CR 仍在评审中，但合并检查未查成功；请重试或打开 CR 核对。"
			} else if status.Conflicted {
				result.State, result.StatusLabel, result.Severity = "conflicted", "有合并冲突", "warning"
				result.Suggestion = "先解决合并冲突，再重新检查评审状态。"
			} else if *status.ReadyToMerge {
				result.State, result.StatusLabel, result.Severity = "approved", "已通过，可合并", "success"
				result.Suggestion = "平台合并检查已通过；核对最终改动后，可前往 CR 合并。"
			} else {
				result.StatusLabel = "检查未通过"
				result.Suggestion = "仍有合并检查未通过；打开 CR 查看评审、测试或评论阻塞项。"
			}
		}
	}
	var comments []adviceMRComment
	if errs[2] == nil {
		errs[2] = json.Unmarshal(data[2], &comments)
		if errs[2] == nil && (comments == nil || len(comments) > 10000) {
			errs[2] = errors.New("评论列表返回不完整或超出查询上限")
		}
		for _, comment := range comments {
			if comment.ID <= 0 {
				errs[2] = errors.New("评论缺少有效 ID")
				break
			}
		}
	}
	if errs[2] == nil {
		applyAdviceComments(&result, comments, previous, checkedAt)
	}
	for index, label := range []string{"评审状态", "合并检查", "评论"} {
		if errs[index] != nil {
			result.Errors = append(result.Errors, label+"查询失败："+adviceText(errs[index].Error(), 180))
		}
	}
	return result
}

func applyAdviceComments(result *AdviceReview, comments []adviceMRComment, previous *AdviceCommentBaseline, checkedAt string) {
	slices.SortFunc(comments, func(a, b adviceMRComment) int {
		at, _ := time.Parse(time.RFC3339, a.CreatedAt)
		bt, _ := time.Parse(time.RFC3339, b.CreatedAt)
		if compared := bt.Compare(at); compared != 0 {
			return compared
		}
		if a.ID > b.ID {
			return -1
		}
		if a.ID < b.ID {
			return 1
		}
		return 0
	})
	seen := map[int64]bool{}
	if previous != nil {
		for _, id := range previous.CommentIDs {
			seen[id] = true
		}
	}
	current := map[int64]bool{}
	baseline := AdviceCommentBaseline{URL: result.URL, CheckedAt: checkedAt, CommentIDs: []int64{}}
	count, unresolved, newCount := 0, 0, 0
	for _, comment := range comments {
		if comment.IsDraft || comment.IsAISummary || current[comment.ID] {
			continue
		}
		current[comment.ID] = true
		count++
		baseline.CommentIDs = append(baseline.CommentIDs, comment.ID)
		// Replies and global comments have closed=0 too, but are not unresolved
		// review threads. This matches a1's --unresolved semantics.
		if comment.ParentNoteID == 0 && comment.Path != "" && comment.Closed != nil && *comment.Closed == 0 {
			unresolved++
		}
		if previous != nil && !seen[comment.ID] {
			newCount++
		}
		if len(result.Comments) < 3 {
			result.Comments = append(result.Comments, AdviceComment{
				ID: comment.ID, Author: comment.Author.Name, Text: adviceText(comment.Note, 240), CreatedAt: comment.CreatedAt,
			})
		}
	}
	result.CommentCount, result.UnresolvedCount, result.Baseline = &count, &unresolved, &baseline
	if previous != nil {
		result.NewCommentCount = &newCount
	}
	if newCount > 0 {
		result.Suggestion = fmt.Sprintf("有 %d 条新评论，建议先查看并确认是否需要回复。", newCount) + result.Suggestion
		result.Severity = "warning"
	} else if unresolved > 0 && result.State != "merged" && result.State != "closed" {
		result.Suggestion = fmt.Sprintf("有 %d 条未解决的行内评论，建议先核对处理情况。", unresolved) + result.Suggestion
		result.Severity = "warning"
	}
}

func runAdviceA1(ctx context.Context, args []string) ([]byte, error) {
	bin, err := exec.LookPath("a1")
	if err != nil {
		home, _ := os.UserHomeDir()
		for _, candidate := range []string{filepath.Join(home, ".local", "bin", "a1"), "/opt/homebrew/bin/a1", "/usr/local/bin/a1"} {
			if resolved, lookupErr := exec.LookPath(candidate); lookupErr == nil {
				bin, err = resolved, nil
				break
			}
		}
	}
	if err != nil {
		return nil, errors.New("未找到 a1 CLI，请安装后重试")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, bin, args...)
	command.WaitDelay = time.Second
	stdout, stderr := &adviceBuffer{limit: 4 << 20}, &adviceBuffer{limit: 4096}
	command.Stdout, command.Stderr = stdout, stderr
	err = command.Run()
	if ctx.Err() != nil {
		return nil, errors.New("查询超时或已取消，请检查网络后重试")
	}
	if err != nil {
		message := strings.ToLower(stderr.String())
		if strings.Contains(message, "auth") || strings.Contains(message, "login") || strings.Contains(message, "401") || strings.Contains(message, "登录") {
			return nil, errors.New("a1 登录已失效，请运行 a1 auth login --buc 后重试")
		}
		return nil, errors.New("a1 查询失败，请检查网络、登录状态和仓库访问权限")
	}
	if stdout.exceeded {
		return nil, errors.New("a1 返回内容过大，请打开 CR 查看")
	}
	return stdout.Bytes(), nil
}

type adviceBuffer struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *adviceBuffer) Write(data []byte) (int, error) {
	size := len(data)
	remaining := buffer.limit - buffer.Len()
	if len(data) > remaining {
		data = data[:remaining]
		buffer.exceeded = true
	}
	_, _ = buffer.Buffer.Write(data)
	return size, nil
}
