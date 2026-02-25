package cli

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/tui"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open interactive TUI dashboard",
	Long:  "Opens an interactive dashboard showing epic cards with pipeline progress, blocker resolution, and accept/reject workflows.",
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

	workDir, _ := os.Getwd()
	model := tui.New(s, workDir)
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
