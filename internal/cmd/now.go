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

var nowLaneFlag string

type nowSummary struct {
	Open        int `json:"open"`
	InProgress  int `json:"in_progress"`
	Waiting     int `json:"waiting"`
	Review      int `json:"review"`
	Blocked     int `json:"blocked"`
	Due         int `json:"due"`
	Maintenance int `json:"maintenance"`
}

type nowView struct {
	GeneratedAt string       `json:"generated_at"`
	Open        []store.Todo `json:"open"`
	Working     []store.Todo `json:"working"`
	Waiting     []store.Todo `json:"waiting"`
	Review      []store.Todo `json:"review"`
	Blocked     []store.Todo `json:"blocked"`
	Due         []store.Todo `json:"due"`
	Summary     nowSummary   `json:"summary"`
}

func init() {
	nowCmd.Flags().StringVar(&nowLaneFlag, "lane", "", "show one work lane (for example: work, personal)")
	rootCmd.AddCommand(nowCmd)
}

var nowCmd = &cobra.Command{
	Use:   "now",
	Short: "Show current work by lifecycle status",
	RunE:  runNow,
}

func runNow(cmd *cobra.Command, args []string) error {
	lane := ""
	if nowLaneFlag != "" {
		var err error
		lane, err = normalizeLane(nowLaneFlag)
		if err != nil {
			return err
		}
	}

	tf, err := store.LoadTodosReadOnly()
	if err != nil {
		return err
	}
	view := buildNowView(tf, lane, time.Now().In(config.Loc))

	if jsonOutput {
		output.JSON(view)
		return nil
	}
	printNow(view, lane)
	return nil
}

func buildNowView(tf *store.TodoFile, lane string, now time.Time) nowView {
	today := now.Format("2006-01-02")
	view := nowView{
		GeneratedAt: now.Format(time.RFC3339),
		Open:        []store.Todo{},
		Working:     []store.Todo{},
		Waiting:     []store.Todo{},
		Review:      []store.Todo{},
		Blocked:     []store.Todo{},
		Due:         []store.Todo{},
	}

	for _, t := range tf.Items {
		if !store.TodoIsActive(t) || (lane != "" && t.Lane != lane) {
			continue
		}
		switch t.Status {
		case store.TodoStatusInProgress:
			view.Working = append(view.Working, t)
		case store.TodoStatusReview:
			view.Review = append(view.Review, t)
		case store.TodoStatusBlocked:
			view.Blocked = append(view.Blocked, t)
		case store.TodoStatusWaiting:
			if t.ReviewAt != "" && t.ReviewAt <= today {
				view.Due = append(view.Due, t)
			} else {
				view.Waiting = append(view.Waiting, t)
			}
		default:
			view.Open = append(view.Open, t)
		}
	}

	for _, items := range [][]store.Todo{view.Working, view.Review, view.Blocked, view.Due, view.Waiting, view.Open} {
		sortTodosForWork(items)
	}
	maintenance := 0
	for _, t := range tf.Items {
		if store.TodoIsActive(t) && (lane == "" || t.Lane == lane) && store.TodoHasTag(t, store.TodoTagMaintenance) {
			maintenance++
		}
	}
	view.Summary = nowSummary{
		Open:        len(view.Open),
		InProgress:  len(view.Working),
		Waiting:     len(view.Waiting),
		Review:      len(view.Review),
		Blocked:     len(view.Blocked),
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

func printNow(view nowView, lane string) {
	title := "ATM Now"
	if lane != "" {
		title += " · " + lane
	}
	fmt.Printf("%s  (%s)\n", title, view.GeneratedAt)
	fmt.Println(strings.Repeat("=", 64))

	fmt.Printf("\nWorking (%d)\n", len(view.Working))
	printNowTodos("working", view.Working)
	if len(view.Working) == 0 {
		fmt.Println("  none")
	}

	needsAction := len(view.Review) + len(view.Blocked) + len(view.Due)
	fmt.Printf("\nNeeds action (%d)\n", needsAction)
	printNowTodos("review", view.Review)
	printNowTodos("blocked", view.Blocked)
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
