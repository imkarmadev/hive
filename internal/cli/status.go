package cli

import (
	"fmt"

	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Quick status overview",
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
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
		fmt.Printf("No tasks. Run: %shive task create \"description\"%s\n", colorCyan, colorReset)
		return nil
	}

	counts := map[store.TaskStatus]int{}
	var blocked []store.Task

	for _, t := range tasks {
		counts[t.Status]++
		if t.Status == store.StatusBlocked {
			blocked = append(blocked, t)
		}
	}

	fmt.Printf("%sTasks: %d total%s\n", colorBold, len(tasks), colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "backlog:", colorWhite, counts[store.StatusBacklog], colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "in_progress:", colorBlue, counts[store.StatusInProgress], colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "blocked:", colorRed, counts[store.StatusBlocked], colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "review:", colorMagenta, counts[store.StatusReview], colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "done:", colorGreen, counts[store.StatusDone], colorReset)
	fmt.Printf("  %-14s %s%d%s\n", "failed:", colorRed, counts[store.StatusFailed], colorReset)

	if len(blocked) > 0 {
		fmt.Printf("\n%s⚠  Blockers (need your input):%s\n", colorRed+colorBold, colorReset)
		for _, t := range blocked {
			fmt.Printf("  %s#%d%s: %s\n", colorYellow, t.ID, colorReset, t.BlockedReason)
		}
	}

	return nil
}
