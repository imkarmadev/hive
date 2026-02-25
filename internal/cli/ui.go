package cli

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/tui"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open interactive TUI dashboard",
	Long:  "Opens a lazygit-style interactive kanban board where you can manage tasks, answer blockers, and monitor agent work.",
	RunE:  runUI,
}

func init() {
	rootCmd.AddCommand(uiCmd)
}

func runUI(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}

	model := tui.New(s)
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	// Close store after TUI exits.
	_ = finalModel
	s.Close()

	return nil
}
