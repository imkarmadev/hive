package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "hive",
	Short: "Kanban for AI agents",
	Long:  "hive — a CLI tool that gives developers a kanban board for AI agents.\nYou are the PM. Agents are your workers.",
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(taskCmd)
	rootCmd.AddCommand(boardCmd)
	rootCmd.AddCommand(answerCmd)
	rootCmd.AddCommand(logCmd)
	rootCmd.AddCommand(statusCmd)
}
