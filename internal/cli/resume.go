package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var resumeCmd = &cobra.Command{
	Use:   "resume [run-id]",
	Short: "Resume an interrupted pipeline run",
	Long: `Resumes a pipeline that was interrupted by a crash, Ctrl+C, or system restart.

Without arguments, lists all interrupted runs so you can pick one.
With a run ID, resumes that specific pipeline.

Resuming will:
  1. Reset any tasks stuck in in_progress or review back to backlog
  2. Mark the interrupted pipeline run as ended
  3. Re-run 'hive auto' on the same epic with the same settings (--skip-plan)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runResume,
}

func init() {
	rootCmd.AddCommand(resumeCmd)
}

func runResume(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	// If no argument, list interrupted runs.
	if len(args) == 0 {
		return listInterruptedRuns(s)
	}

	// Resume a specific run.
	runID, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid run ID: %s", args[0])
	}

	return resumeRun(s, runID, cmd)
}

func listInterruptedRuns(s *store.Store) error {
	runs, err := s.ListInterruptedRuns()
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		fmt.Printf("  %s✓ No interrupted pipelines found.%s\n", colorGreen, colorReset)
		return nil
	}

	fmt.Printf("%s╔══════════════════════════════════════╗%s\n", colorBold, colorReset)
	fmt.Printf("%s║  Interrupted pipeline runs           ║%s\n", colorBold, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════╝%s\n\n", colorBold, colorReset)

	for _, run := range runs {
		epic, err := s.GetTask(run.EpicID)
		epicTitle := fmt.Sprintf("(epic #%d)", run.EpicID)
		if err == nil {
			epicTitle = epic.Title
		}

		age := time.Since(run.StartedAt).Truncate(time.Second)

		fmt.Printf("  %sRun #%d%s  %sE#%d%s %s\n",
			colorYellow, run.ID, colorReset,
			colorCyan, run.EpicID, colorReset,
			epicTitle)
		fmt.Printf("    Started:  %s (%s ago)\n", run.StartedAt.Local().Format("2006-01-02 15:04:05"), age)
		fmt.Printf("    Settings: max-loops=%d parallel=%d\n", run.MaxLoops, run.Parallel)

		// Show task status summary for this epic.
		tasks, _ := s.ListTasksByEpic(run.EpicID)
		if len(tasks) > 0 {
			counts := map[store.TaskStatus]int{}
			for _, t := range tasks {
				counts[t.Status]++
			}

			var parts []string
			if n := counts[store.StatusDone]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d done%s", colorGreen, n, colorReset))
			}
			if n := counts[store.StatusInProgress]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d stuck in_progress%s", colorRed, n, colorReset))
			}
			if n := counts[store.StatusReview]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d stuck in review%s", colorRed, n, colorReset))
			}
			if n := counts[store.StatusBlocked]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d blocked%s", colorYellow, n, colorReset))
			}
			if n := counts[store.StatusBacklog]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d backlog", n))
			}
			if n := counts[store.StatusFailed]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s%d failed%s", colorRed, n, colorReset))
			}

			fmt.Printf("    Tasks:    %s\n", strings.Join(parts, ", "))
		}
		fmt.Println()
	}

	fmt.Printf("  Resume with: %shive resume <run-id>%s\n", colorCyan, colorReset)
	return nil
}

func resumeRun(s *store.Store, runID int64, cmd *cobra.Command) error {
	// Find the run by scanning all interrupted runs.
	runs, err := s.ListInterruptedRuns()
	if err != nil {
		return err
	}

	var target *store.PipelineRun
	for i := range runs {
		if runs[i].ID == runID {
			target = &runs[i]
			break
		}
	}

	if target == nil {
		return fmt.Errorf("run #%d not found or not in 'running' state (already completed?)", runID)
	}

	epic, err := s.GetTask(target.EpicID)
	if err != nil {
		return fmt.Errorf("epic #%d not found: %w", target.EpicID, err)
	}

	fmt.Printf("%s╔══════════════════════════════════════╗%s\n", colorBold, colorReset)
	fmt.Printf("%s║  hive resume — crash recovery        ║%s\n", colorBold, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════╝%s\n\n", colorBold, colorReset)

	fmt.Printf("  Run:      %s#%d%s\n", colorYellow, target.ID, colorReset)
	fmt.Printf("  Epic:     %sE#%d%s %s\n", colorCyan, epic.ID, colorReset, epic.Title)
	fmt.Printf("  Started:  %s\n", target.StartedAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Println()

	// Step 1: Reset stale tasks.
	resetCount, err := s.ResetStaleTasks(epic.ID)
	if err != nil {
		return fmt.Errorf("reset stale tasks: %w", err)
	}
	if resetCount > 0 {
		fmt.Printf("  %s↺ Reset %d stale task(s) back to backlog%s\n", colorYellow, resetCount, colorReset)
	} else {
		fmt.Printf("  %s✓ No stale tasks to reset%s\n", colorGreen, colorReset)
	}

	// Step 2: Mark old run as interrupted.
	if err := s.EndPipelineRun(target.ID, "interrupted"); err != nil {
		return fmt.Errorf("end old run: %w", err)
	}
	fmt.Printf("  %s✓ Marked run #%d as interrupted%s\n\n", colorDim, target.ID, colorReset)

	// Step 3: Re-run auto with same settings.
	fmt.Printf("  Resuming with: max-loops=%d parallel=%d --skip-plan\n\n", target.MaxLoops, target.Parallel)

	// Set the global flags used by runAuto, then call it.
	autoMaxLoops = target.MaxLoops
	autoParallel = target.Parallel
	autoSkipPlan = true

	return runAuto(cmd, []string{strconv.FormatInt(epic.ID, 10)})
}
