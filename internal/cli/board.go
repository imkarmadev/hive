package cli

import (
	"fmt"
	"strings"

	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board",
	Short: "Show the kanban board",
	RunE:  runBoard,
}

func runBoard(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	tasks, err := s.ListTasks("")
	if err != nil {
		return err
	}

	if len(tasks) == 0 {
		fmt.Println("Board is empty. Create a task: hive task create \"description\"")
		return nil
	}

	// Group tasks by status.
	columns := map[store.TaskStatus][]store.Task{
		store.StatusBacklog:    {},
		store.StatusInProgress: {},
		store.StatusBlocked:    {},
		store.StatusReview:     {},
		store.StatusDone:       {},
		store.StatusFailed:     {},
	}

	for _, t := range tasks {
		columns[t.Status] = append(columns[t.Status], t)
	}

	// Render columns.
	type col struct {
		status store.TaskStatus
		label  string
	}
	order := []col{
		{store.StatusBacklog, "BACKLOG"},
		{store.StatusInProgress, "IN PROGRESS"},
		{store.StatusBlocked, "BLOCKED"},
		{store.StatusReview, "REVIEW"},
		{store.StatusDone, "DONE"},
	}

	// Print header.
	colWidth := 22
	headerLine := ""
	sepLine := ""
	for _, c := range order {
		count := len(columns[c.status])
		header := fmt.Sprintf(" %s (%d)", c.label, count)
		headerLine += padRight(header, colWidth)
		sepLine += strings.Repeat("-", colWidth)
	}
	fmt.Println(headerLine)
	fmt.Println(sepLine)

	// Find max rows.
	maxRows := 0
	for _, c := range order {
		if len(columns[c.status]) > maxRows {
			maxRows = len(columns[c.status])
		}
	}

	// Print rows.
	for i := 0; i < maxRows; i++ {
		// Task title line.
		line := ""
		for _, c := range order {
			tasks := columns[c.status]
			if i < len(tasks) {
				t := tasks[i]
				card := fmt.Sprintf(" #%d %s", t.ID, truncate(t.Title, colWidth-6))
				line += padRight(card, colWidth)
			} else {
				line += padRight("", colWidth)
			}
		}
		fmt.Println(line)

		// Agent/details line.
		detailLine := ""
		for _, c := range order {
			tasks := columns[c.status]
			if i < len(tasks) {
				t := tasks[i]
				detail := ""
				if t.AssignedAgent != "" {
					detail = fmt.Sprintf("    [%s]", t.AssignedAgent)
				}
				if t.Status == store.StatusBlocked && t.BlockedReason != "" {
					detail = fmt.Sprintf("    ? %s", truncate(t.BlockedReason, colWidth-6))
				}
				detailLine += padRight(detail, colWidth)
			} else {
				detailLine += padRight("", colWidth)
			}
		}
		fmt.Println(detailLine)
		fmt.Println() // spacing between cards
	}

	// Show blocked tasks summary if any.
	blocked := columns[store.StatusBlocked]
	if len(blocked) > 0 {
		fmt.Println("--- Blockers (need your input) ---")
		for _, t := range blocked {
			fmt.Printf("  #%d: %s\n", t.ID, t.BlockedReason)
			fmt.Printf("       -> hive answer %d \"your answer\"\n", t.ID)
		}
	}

	return nil
}

func padRight(s string, width int) string {
	if len(s) >= width {
		return s[:width]
	}
	return s + strings.Repeat(" ", width-len(s))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
