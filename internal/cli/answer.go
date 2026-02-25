package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var answerCmd = &cobra.Command{
	Use:   "answer [task-id] [answer]",
	Short: "Answer a blocker on a task",
	Long:  "Resolves a blocked task by providing the requested information.",
	Args:  cobra.MinimumNArgs(2),
	RunE:  runAnswer,
}

func runAnswer(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	// Check task exists and is blocked.
	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("task #%d not found", id)
	}
	if task.Status != "blocked" {
		return fmt.Errorf("task #%d is not blocked (status: %s)", id, task.Status)
	}

	answer := strings.Join(args[1:], " ")
	if err := s.UnblockTask(id, answer); err != nil {
		return err
	}

	fmt.Printf("Unblocked task #%d\n", id)
	fmt.Printf("  Question was: %s\n", task.BlockedReason)
	fmt.Printf("  Your answer:  %s\n", answer)
	return nil
}
