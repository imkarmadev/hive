package cli

import (
	"fmt"
	"strings"

	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

// ANSI color codes.
const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorBgRed   = "\033[41m"
	colorBgGreen = "\033[42m"
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
		fmt.Printf("%sBoard is empty.%s Create an epic: %shive epic create \"description\"%s\n",
			colorDim, colorReset, colorCyan, colorReset)
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

	type col struct {
		status store.TaskStatus
		label  string
		color  string
	}
	order := []col{
		{store.StatusBacklog, "BACKLOG", colorWhite},
		{store.StatusInProgress, "IN PROGRESS", colorBlue},
		{store.StatusBlocked, "BLOCKED", colorRed},
		{store.StatusReview, "REVIEW", colorMagenta},
		{store.StatusDone, "DONE", colorGreen},
	}

	// Print header.
	colWidth := 24
	headerLine := ""
	sepLine := ""
	for _, c := range order {
		count := len(columns[c.status])
		header := fmt.Sprintf(" %s%s%s (%d)", c.color+colorBold, c.label, colorReset, count)
		// padRight needs visible length, not byte length (ANSI codes add bytes).
		visibleLen := len(fmt.Sprintf(" %s (%d)", c.label, count))
		padding := colWidth - visibleLen
		if padding < 0 {
			padding = 0
		}
		headerLine += header + strings.Repeat(" ", padding)
		sepLine += strings.Repeat("─", colWidth)
	}
	fmt.Println(headerLine)
	fmt.Println(colorDim + sepLine + colorReset)

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
				priColor := priorityColor(t.Priority)
				prefix := ""
				if t.Kind == store.KindEpic {
					prefix = "E"
				}
				idStr := fmt.Sprintf("%s#%d", prefix, t.ID)
				titleStr := truncate(t.Title, colWidth-len(idStr)-3)
				card := fmt.Sprintf(" %s%s%s %s", priColor, idStr, colorReset, titleStr)
				visibleLen := len(fmt.Sprintf(" %s %s", idStr, titleStr))
				padding := colWidth - visibleLen
				if padding < 0 {
					padding = 0
				}
				line += card + strings.Repeat(" ", padding)
			} else {
				line += strings.Repeat(" ", colWidth)
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
				visibleDetail := ""
				if t.AssignedAgent != "" {
					detail = fmt.Sprintf("    %s[%s]%s", colorCyan, t.AssignedAgent, colorReset)
					visibleDetail = fmt.Sprintf("    [%s]", t.AssignedAgent)
				}
				if t.Status == store.StatusBlocked && t.BlockedReason != "" {
					reason := truncate(t.BlockedReason, colWidth-7)
					detail = fmt.Sprintf("    %s⚠ %s%s", colorRed, reason, colorReset)
					visibleDetail = fmt.Sprintf("    ⚠ %s", reason) // ⚠ is multi-byte but 1 col wide... approximately
				}
				padding := colWidth - len(visibleDetail)
				if padding < 0 {
					padding = 0
				}
				detailLine += detail + strings.Repeat(" ", padding)
			} else {
				detailLine += strings.Repeat(" ", colWidth)
			}
		}
		fmt.Println(detailLine)
		fmt.Println() // spacing between cards
	}

	// Show blocked tasks summary.
	blocked := columns[store.StatusBlocked]
	if len(blocked) > 0 {
		fmt.Printf("%s%s⚠  Blockers (need your input)%s\n", colorBold, colorRed, colorReset)
		for _, t := range blocked {
			fmt.Printf("  %s#%d%s: %s\n", colorYellow, t.ID, colorReset, t.BlockedReason)
			fmt.Printf("       → %shive answer %d \"your answer\"%s\n", colorCyan, t.ID, colorReset)
		}
		fmt.Println()
	}

	// Show failed tasks.
	failed := columns[store.StatusFailed]
	if len(failed) > 0 {
		fmt.Printf("%s%s✗  Failed tasks%s\n", colorBold, colorRed, colorReset)
		for _, t := range failed {
			agent := ""
			if t.AssignedAgent != "" {
				agent = fmt.Sprintf(" [%s]", t.AssignedAgent)
			}
			fmt.Printf("  %s#%d%s: %s%s\n", colorYellow, t.ID, colorReset, t.Title, agent)
		}
		fmt.Println()
	}

	// Summary line.
	total := len(tasks)
	doneCount := len(columns[store.StatusDone])
	inProgress := len(columns[store.StatusInProgress])
	blockedCount := len(blocked)

	fmt.Printf("%s%d tasks%s", colorBold, total, colorReset)
	if doneCount > 0 {
		fmt.Printf("  %s✓ %d done%s", colorGreen, doneCount, colorReset)
	}
	if inProgress > 0 {
		fmt.Printf("  %s● %d in progress%s", colorBlue, inProgress, colorReset)
	}
	if blockedCount > 0 {
		fmt.Printf("  %s⚠ %d blocked%s", colorRed, blockedCount, colorReset)
	}
	fmt.Println()

	return nil
}

func priorityColor(priority string) string {
	switch priority {
	case "high":
		return colorRed + colorBold
	case "medium":
		return colorYellow
	case "low":
		return colorDim
	default:
		return ""
	}
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
