package aiday

import "strings"

type Classification struct {
	Labels     []string
	Confidence float64
}

// Classify is a small local rules engine. Only the returned labels are stored.
func Classify(content string) Classification {
	text := strings.ToLower(strings.TrimSpace(content))
	type rule struct {
		label string
		terms []string
	}
	rules := []rule{
		{"correction", []string{"不对", "错了", "应该是", "改成", "纠正", "not right", "incorrect", "should be"}},
		{"retry", []string{"再试", "重试", "重新来", "再来一次", "try again", "retry"}},
		{"refinement", []string{"优化", "细化", "完善", "更具体", "调整一下", "refine", "improve", "polish"}},
		{"question", []string{"?", "？", "为什么", "怎么", "如何", "什么", "是否", "why", "how", "what", "can you"}},
		{"directive", []string{"请", "帮我", "实现", "创建", "生成", "修改", "删除", "运行", "please", "build", "create", "implement", "update"}},
		{"acceptance", []string{"可以", "不错", "通过", "就这样", "没问题", "很好", "looks good", "approved", "ship it"}},
		{"brainstorm", []string{"想法", "方案", "头脑风暴", "有哪些可能", "创意", "brainstorm", "ideas", "alternatives"}},
		{"explanation", []string{"解释", "说明", "原理", "分析一下", "讲讲", "explain", "describe", "walk me through"}},
	}
	labels := make([]string, 0, 2)
	for _, candidate := range rules {
		for _, term := range candidate.terms {
			if strings.Contains(text, term) {
				labels = append(labels, candidate.label)
				break
			}
		}
	}
	if len(labels) == 0 && text != "" {
		labels = append(labels, "directive")
	}
	confidence := 0.0
	if len(labels) > 0 {
		confidence = 0.72
	}
	if len(labels) > 1 {
		confidence = 0.82
	}
	return Classification{Labels: labels, Confidence: confidence}
}
