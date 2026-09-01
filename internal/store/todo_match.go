package store

import (
	"sort"
	"strings"
	"unicode"

	"github.com/zane-byte-dev/atm/internal/config"
)

type TodoMatch struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Project string `json:"project,omitempty"`
	Status  string `json:"status"`
	Score   int    `json:"score"`
	// QueryScore is the part of Score the query itself earned, before the bonuses
	// for being in this project, in progress, or high priority. Score alone cannot
	// answer "is this actually related?": being in the current project is worth
	// +100, which outranks any amount of query relevance, so every active todo in
	// the repo outscores the floor no matter what was searched for.
	QueryScore int    `json:"query_score"`
	Reason     string `json:"reason,omitempty"`
}

// TodoDedupMinQueryScore is the relevance floor for deciding whether an existing
// todo already covers a goal.
//
// scoreTodoQuery pays 10 per matched token and 30 for containing the whole query,
// and tokens go down to two-character CJK bigrams, which match across unrelated
// text easily. Measured against the live todo set, real hits and noise separate
// cleanly with room to spare: searching a todo's own title scored it 140 while
// unrelated todos picked up 10-20 from incidental bigrams ("不能", "新建"), and a
// paraphrased search scored its target 60 against 10 elsewhere. 30 sits in that
// gap and is also the exact value of a whole-query substring match.
//
// A duplicate worded so differently that it scores below this is missed, and the
// caller creates a second todo. That is the safer direction to fail in — and
// --min-query-score lowers the floor when a caller would rather sift.
const TodoDedupMinQueryScore = 30

// TodoMatchOptions configures a search. The zero value reproduces the ranking
// used for session-start injection, which is deliberately permissive: missing a
// candidate at startup costs more than offering one too many.
type TodoMatchOptions struct {
	Project string
	Query   string
	Limit   int
	// MinQueryScore drops candidates whose query relevance falls below it. Set it
	// when the caller needs "no match" to be a possible answer — deciding whether
	// to create a todo, rather than picking one to bind.
	MinQueryScore int
	// AllProjects scores todos from every project instead of narrowing to Project
	// once that project has any active todo. A duplicate filed under another
	// project is still a duplicate; a candidate to bind at startup usually is not.
	AllProjects bool
}

func MatchTodos(tf *TodoFile, project, query string, limit int) []TodoMatch {
	return MatchTodosWithOptions(tf, TodoMatchOptions{Project: project, Query: query, Limit: limit})
}

func MatchTodosWithOptions(tf *TodoFile, opts TodoMatchOptions) []TodoMatch {
	limit := opts.Limit
	if limit < 1 {
		limit = 3
	}
	project := config.CanonicalProject(strings.TrimSpace(opts.Project))
	query := opts.Query
	queryTokens := todoMatchTokens(query)
	hasProjectMatch := false
	for _, todo := range tf.Items {
		if TodoIsActive(todo) && project != "" && config.ProjectMatches(todo.Project, project) {
			hasProjectMatch = true
			break
		}
	}

	results := make([]TodoMatch, 0)
	for _, todo := range tf.Items {
		if !TodoIsActive(todo) {
			continue
		}
		projectMatch := project != "" && config.ProjectMatches(todo.Project, project)
		if hasProjectMatch && !projectMatch && !opts.AllProjects {
			continue
		}
		queryScore, matchedTokens := scoreTodoQuery(todo, query, queryTokens)
		if !projectMatch && queryScore == 0 {
			continue
		}
		if queryScore < opts.MinQueryScore {
			continue
		}

		score := queryScore
		var reasons []string
		if projectMatch {
			score += 100
			reasons = append(reasons, "project")
		}
		if matchedTokens > 0 {
			reasons = append(reasons, "query")
		}
		switch todo.Status {
		case TodoStatusInProgress:
			score += 20
		case TodoStatusReview:
			score += 8
		}
		switch todo.Priority {
		case "P0":
			score += 12
		case "P1":
			score += 8
		case "P2":
			score += 4
		case "P3":
			score++
		}
		results = append(results, TodoMatch{
			ID: todo.ID, Title: todo.Title, Project: todo.Project, Status: todo.Status,
			Score: score, QueryScore: queryScore, Reason: strings.Join(reasons, "+"),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].ID < results[j].ID
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results
}

func scoreTodoQuery(todo Todo, query string, tokens []string) (int, int) {
	if strings.TrimSpace(query) == "" {
		return 0, 0
	}
	corpus := strings.ToLower(strings.Join([]string{todo.ID, todo.Title, todo.Description, todo.Project}, " "))
	score := 0
	matched := 0
	normalizedQuery := strings.ToLower(strings.TrimSpace(query))
	if len([]rune(normalizedQuery)) >= 3 && strings.Contains(corpus, normalizedQuery) {
		score += 30
	}
	for _, token := range tokens {
		if strings.Contains(corpus, token) {
			score += 10
			matched++
		}
	}
	return score, matched
}

func todoMatchTokens(value string) []string {
	runes := []rune(strings.ToLower(value))
	seen := map[string]bool{}
	var result []string
	add := func(token string) {
		if len([]rune(token)) < 2 || seen[token] {
			return
		}
		seen[token] = true
		result = append(result, token)
	}
	for index := 0; index < len(runes); {
		if unicode.Is(unicode.Han, runes[index]) {
			start := index
			for index < len(runes) && unicode.Is(unicode.Han, runes[index]) {
				index++
			}
			segment := runes[start:index]
			if len(segment) <= 4 {
				add(string(segment))
			}
			for i := 0; i+1 < len(segment); i++ {
				add(string(segment[i : i+2]))
			}
			continue
		}
		if unicode.IsLetter(runes[index]) || unicode.IsNumber(runes[index]) {
			start := index
			for index < len(runes) && (unicode.IsLetter(runes[index]) || unicode.IsNumber(runes[index])) && !unicode.Is(unicode.Han, runes[index]) {
				index++
			}
			add(string(runes[start:index]))
			continue
		}
		index++
	}
	return result
}
