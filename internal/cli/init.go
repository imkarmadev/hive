package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imkarma/hive/internal/config"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize hive in the current directory",
	Long:  "Creates a .hive/ directory with default config and database.",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	hiveDir := ".hive"
	runsDir := filepath.Join(hiveDir, "runs")

	// Check if already initialized.
	if _, err := os.Stat(hiveDir); err == nil {
		return fmt.Errorf("hive already initialized in this directory (.hive/ exists)")
	}

	// Create directories.
	if err := os.MkdirAll(runsDir, 0755); err != nil {
		return fmt.Errorf("create .hive/runs: %w", err)
	}

	// Write default config.
	cfgPath := filepath.Join(hiveDir, "config.yaml")
	cfg := config.DefaultConfig()
	if err := config.Save(cfgPath, cfg); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	// Create database by opening store (migration runs automatically).
	dbPath := filepath.Join(hiveDir, "hive.db")
	store, err := openStore(dbPath)
	if err != nil {
		return fmt.Errorf("create database: %w", err)
	}
	store.Close()

	fmt.Println("Initialized hive in .hive/")
	fmt.Println("")
	fmt.Println("Next steps:")
	fmt.Println("  1. Edit .hive/config.yaml to add your agents")
	fmt.Println("  2. Run: hive task \"your task description\"")
	fmt.Println("  3. Run: hive board")

	return nil
}
