package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"
)

func Audit(dataDir string, options AuditOptions) (*AuditReport, error) {
	if options.StaleDays <= 0 {
		options.StaleDays = 180
	}
	if options.Now.IsZero() {
		options.Now = time.Now().UTC()
	}
	documents, err := Discover(dataDir)
	if err != nil {
		return nil, err
	}
	report := &AuditReport{
		GeneratedAt: options.Now,
		StaleDays:   options.StaleDays,
		Documents:   len(documents),
		Issues:      []AuditIssue{},
		Counts:      make(map[string]int),
	}
	titles := make(map[string][]Document)
	contents := make(map[string][]Document)
	for _, document := range documents {
		if document.Metadata.Status == "archived" {
			continue
		}
		report.Active++
		titles[auditNormalize(document.Metadata.Title)] = append(titles[auditNormalize(document.Metadata.Title)], document)
		body := auditNormalize(document.Content)
		if body != "" {
			contents[body] = append(contents[body], document)
		}
		if !document.Metadata.UpdatedAt.IsZero() && document.Metadata.UpdatedAt.Before(options.Now.AddDate(0, 0, -options.StaleDays)) {
			report.Issues = append(report.Issues, auditDocumentIssue("stale", "warning", document, fmt.Sprintf("未更新超过 %d 天", options.StaleDays), "复核内容；仍有效则更新，失效则归档"))
		}
		if source := document.Metadata.Source; source != nil && source.Type == "file" {
			path, pathErr := filePathFromURI(source.URI)
			if pathErr != nil {
				report.Issues = append(report.Issues, auditDocumentIssue("source_invalid", "error", document, pathErr.Error(), "修复来源 URI 或重新导入"))
				continue
			}
			data, readErr := os.ReadFile(path)
			if os.IsNotExist(readErr) {
				report.Issues = append(report.Issues, auditDocumentIssue("source_missing", "error", document, "源 Markdown 已不存在", "恢复源文件或将条目迁移为 ATM 原生知识"))
				continue
			}
			if readErr != nil {
				return nil, readErr
			}
			hash := sha256.Sum256(data)
			current := "sha256:" + hex.EncodeToString(hash[:])
			if source.Hash != "" && source.Hash != current {
				report.Issues = append(report.Issues, auditDocumentIssue("source_drift", "warning", document, "源 Markdown 已变更但尚未重新导入", "重新执行 knowledge import 同步内容"))
			}
		}
	}
	appendDuplicateIssues := func(code string, groups map[string][]Document, detail string) {
		for _, group := range groups {
			if len(group) < 2 {
				continue
			}
			ids := make([]string, 0, len(group))
			for _, document := range group {
				ids = append(ids, document.Metadata.ID)
			}
			sort.Strings(ids)
			report.Issues = append(report.Issues, AuditIssue{Code: code, Severity: "warning", DocumentIDs: ids, Title: group[0].Metadata.Title, Detail: detail, SuggestedAction: "比较内容并保留权威版本，其余归档"})
		}
	}
	appendDuplicateIssues("duplicate_title", titles, "存在规范化后相同的标题")
	appendDuplicateIssues("duplicate_content", contents, "存在规范化后完全相同的内容")
	qualities, qualityErr := KnowledgeQualities(dataDir)
	if qualityErr == nil {
		byID := make(map[string]Document, len(documents))
		for _, document := range documents {
			byID[document.Metadata.ID] = document
		}
		for _, quality := range qualities {
			evidence := quality.Adopted + quality.Corrected + quality.Rejected
			if evidence >= 3 && quality.Score < 0.35 {
				document := byID[quality.DocumentID]
				report.Issues = append(report.Issues, auditDocumentIssue("low_quality", "warning", document, fmt.Sprintf("质量分 %.2f，已有 %d 次结果反馈", quality.Score, evidence), "复核并纠正内容；无法修复时归档"))
			}
		}
	}
	sort.SliceStable(report.Issues, func(i, j int) bool {
		if report.Issues[i].Severity != report.Issues[j].Severity {
			return report.Issues[i].Severity < report.Issues[j].Severity
		}
		if report.Issues[i].Code != report.Issues[j].Code {
			return report.Issues[i].Code < report.Issues[j].Code
		}
		return strings.Join(report.Issues[i].DocumentIDs, ",") < strings.Join(report.Issues[j].DocumentIDs, ",")
	})
	for _, issue := range report.Issues {
		report.Counts[issue.Code]++
	}
	return report, nil
}

func auditDocumentIssue(code, severity string, document Document, detail, action string) AuditIssue {
	return AuditIssue{Code: code, Severity: severity, DocumentIDs: []string{document.Metadata.ID}, Collection: document.Collection, Title: document.Metadata.Title, Detail: detail, SuggestedAction: action}
}

func auditNormalize(value string) string {
	var result strings.Builder
	for _, item := range strings.ToLower(value) {
		if unicode.IsLetter(item) || unicode.IsNumber(item) {
			result.WriteRune(item)
		}
	}
	return result.String()
}
