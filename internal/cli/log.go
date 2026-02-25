package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var logCmd = &cobra.Command{
	Use:   "log [task-id]",
	Short: "Show event log for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runLog,
}

func runLog(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}

	events, err := s.GetEvents(id)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		fmt.Printf("No events for task #%d\n", id)
		return nil
	}

	fmt.Printf("Events for task #%d:\n\n", id)
	for _, e := range events {
		agent := ""
		if e.Agent != "" {
			agent = fmt.Sprintf("[%s] ", e.Agent)
		}
		fmt.Printf("  %s  %s%-14s %s\n", e.Timestamp.Format("2006-01-02 15:04:05"), agent, e.Type, e.Content)
	}
	return nil
}
