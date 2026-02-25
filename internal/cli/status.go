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
		fmt.Println("No tasks. Run: hive task create \"description\"")
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

	fmt.Printf("Tasks: %d total\n", len(tasks))
	fmt.Printf("  backlog:     %d\n", counts[store.StatusBacklog])
	fmt.Printf("  in_progress: %d\n", counts[store.StatusInProgress])
	fmt.Printf("  blocked:     %d\n", counts[store.StatusBlocked])
	fmt.Printf("  review:      %d\n", counts[store.StatusReview])
	fmt.Printf("  done:        %d\n", counts[store.StatusDone])
	fmt.Printf("  failed:      %d\n", counts[store.StatusFailed])

	if len(blocked) > 0 {
		fmt.Println("\nBlockers (need your input):")
		for _, t := range blocked {
			fmt.Printf("  #%d: %s\n", t.ID, t.BlockedReason)
		}
	}

	return nil
}
