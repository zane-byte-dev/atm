package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/zane-byte-dev/atm/internal/config"
	"github.com/zane-byte-dev/atm/internal/output"
	"github.com/zane-byte-dev/atm/internal/store"
)

type nowSummary struct {
	Open        int `json:"open"`
	InProgress  int `json:"in_progress"`
	Waiting     int `json:"waiting"`
	Review      int `json:"review"`
	Due         int `json:"due"`
	Maintenance int `json:"maintenance"`
}

type nowView struct {
	GeneratedAt string       `json:"generated_at"`
	Open        []store.Todo `json:"open"`
	Working     []store.Todo `json:"working"`
	Waiting     []store.Todo `json:"waiting"`
	Review      []store.Todo `json:"review"`
	Due         []store.Todo `json:"due"`
	Summary     nowSummary   `json:"summary"`
}

func init() {
	rootCmd.AddCommand(nowCmd)
}

var nowCmd = &cobra.Command{
	Use:   "now",
	Short: "Show current work by lifecycle status",
	Args:  cobra.NoArgs,
	RunE:  runNow,
}

func runNow(cmd *cobra.Command, args []string) error {
	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	view := buildNowView(tf, time.Now().In(config.Loc))

	if jsonOutput {
		output.JSON(view)
		return nil
	}
	printNow(view)
	return nil
}

func buildNowView(tf *store.TodoFile, now time.Time) nowView {
	today := now.Format("2006-01-02")
	view := nowView{
		GeneratedAt: now.Format(time.RFC3339),
		Open:        []store.Todo{},
		Working:     []store.Todo{},
		Waiting:     []store.Todo{},
		Review:      []store.Todo{},
		Due:         []store.Todo{},
	}

	for _, t := range tf.Items {
		if !store.TodoIsActive(t) {
			continue
		}
		switch t.Status {
		case store.TodoStatusInProgress:
			view.Working = append(view.Working, t)
			if t.ReviewAt != "" && t.ReviewAt <= today {
				view.Due = append(view.Due, t)
			} else if strings.TrimSpace(t.WakeCondition) != "" || t.ReviewAt != "" {
				// Waiting is an attention projection of in-progress work, not a
				// separate lifecycle state.
				view.Waiting = append(view.Waiting, t)
			}
		case store.TodoStatusReview:
			view.Review = append(view.Review, t)
		default:
			view.Open = append(view.Open, t)
		}
	}

	for _, items := range [][]store.Todo{view.Working, view.Review, view.Due, view.Waiting, view.Open} {
		sortTodosForWork(items)
	}
	maintenance := 0
	for _, t := range tf.Items {
		if store.TodoIsActive(t) && store.TodoHasTag(t, store.TodoTagMaintenance) {
			maintenance++
		}
	}
	view.Summary = nowSummary{
		Open:        len(view.Open),
		InProgress:  len(view.Working),
		Waiting:     len(view.Waiting),
		Review:      len(view.Review),
		Due:         len(view.Due),
		Maintenance: maintenance,
	}
	return view
}

func sortTodosForWork(items []store.Todo) {
	rank := map[string]int{"P0": 0, "P1": 1, "P2": 2}
	sort.SliceStable(items, func(i, j int) bool {
		left, ok := rank[items[i].Priority]
		if !ok {
			left = 99
		}
		right, ok := rank[items[j].Priority]
		if !ok {
			right = 99
		}
		if left != right {
			return left < right
		}
		if items[i].Created != items[j].Created {
			return items[i].Created < items[j].Created
		}
		return items[i].ID < items[j].ID
	})
}

func printNow(view nowView) {
	fmt.Printf("ATM Now  (%s)\n", view.GeneratedAt)
	fmt.Println(strings.Repeat("=", 64))

	fmt.Printf("\nWorking (%d)\n", len(view.Working))
	printNowTodos("working", view.Working)
	if len(view.Working) == 0 {
		fmt.Println("  none")
	}

	needsAction := len(view.Review) + len(view.Due)
	fmt.Printf("\nNeeds action (%d)\n", needsAction)
	printNowTodos("review", view.Review)
	printNowTodos("due", view.Due)
	if needsAction == 0 {
		fmt.Println("  none")
	}

	fmt.Printf("\nWaiting (%d)\n", len(view.Waiting))
	printNowTodos("waiting", view.Waiting)
	if len(view.Waiting) == 0 {
		fmt.Println("  none")
	}

	fmt.Println("\nOther work")
	fmt.Printf("  open=%d  maintenance=%d\n", len(view.Open), view.Summary.Maintenance)
}

func printNowTodos(label string, items []store.Todo) {
	for _, t := range items {
		extra := ""
		if label == "due" && t.ReviewAt != "" {
			extra = " review=" + t.ReviewAt
		}
		if label == "waiting" {
			switch {
			case t.WakeCondition != "":
				extra = " wake=" + truncLine(t.WakeCondition, 100)
			case t.ReviewAt != "":
				extra = " review=" + t.ReviewAt
			}
		}
		fmt.Printf("  %-8s %-5s %-4s %s%s\n", label, t.ID, t.Priority, t.Title, extra)
	}
}
