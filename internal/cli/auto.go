package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/imkarma/hive/internal/agent"
	"github.com/imkarma/hive/internal/config"
	agentctx "github.com/imkarma/hive/internal/context"
	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
	"github.com/imkarma/hive/internal/worker"
	"github.com/spf13/cobra"
)

var autoCmd = &cobra.Command{
	Use:   "auto [epic-or-task-id]",
	Short: "Run full autonomous pipeline on an epic or task",
	Long: `Runs the complete pipeline automatically:

  1. PM agent breaks epic into tasks (plan)
  2. Auto-assigns agents by role from config
  3. For each task: code → review → fix loop
  4. Reports final summary

All work happens on the epic's git safety branch.
When done, review with 'hive epic diff' and accept/reject.

Stops on blockers — answer them with 'hive answer' and re-run.`,
	Args: cobra.ExactArgs(1),
	RunE: runAuto,
}

var (
	autoMaxLoops int
	autoSkipPlan bool
	autoParallel int
)

func init() {
	autoCmd.Flags().IntVar(&autoMaxLoops, "max-loops", 3, "Maximum fix-review iterations per task")
	autoCmd.Flags().BoolVar(&autoSkipPlan, "skip-plan", false, "Skip planning, run directly on existing tasks")
	autoCmd.Flags().IntVar(&autoParallel, "parallel", 1, "Number of tasks to run in parallel (uses git worktrees)")
	rootCmd.AddCommand(autoCmd)
}

func runAuto(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	cfg, err := config.Load(hivePath("config.yaml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}
	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("task #%d not found", id)
	}

	// Check for interrupted pipeline runs on this epic.
	if task.Kind == store.KindEpic {
		active, _ := s.GetActivePipelineRun(task.ID)
		if active != nil {
			fmt.Printf("  %s⚠ WARNING: Epic #%d has an interrupted pipeline (run #%d, started %s)%s\n",
				colorYellow, task.ID, active.ID,
				active.StartedAt.Local().Format("2006-01-02 15:04"),
				colorReset)
			fmt.Printf("  Some tasks may be stuck in in_progress/review.\n")
			fmt.Printf("  → Use %shive resume %d%s to cleanly recover, or continue anyway.\n\n",
				colorCyan, active.ID, colorReset)
		}
	}

	workDir, _ := os.Getwd()

	// If this is an epic, ensure we're on its safety branch.
	if task.Kind == store.KindEpic {
		safety := git.New(workDir)
		if safety.IsGitRepo() {
			if task.GitBranch == "" {
				// Create safety branch.
				branch := git.BranchName(task.ID)
				if !safety.HasUncommittedChanges() {
					if err := safety.CreateBranch(branch); err == nil {
						s.SetGitBranch(task.ID, branch)
						task.GitBranch = branch
					}
				}
			}
			if task.GitBranch != "" {
				current, _ := safety.CurrentBranch()
				if current != task.GitBranch {
					if err := safety.CreateBranch(task.GitBranch); err != nil {
						return fmt.Errorf("switch to safety branch %s: %w", task.GitBranch, err)
					}
				}
			}
		}
	}

	// Resolve agents by role.
	pmName, pmCfg := findAgentByRole(cfg, "pm")
	coderName, coderCfg := findAgentByRole(cfg, "coder")
	reviewerName, reviewerCfg := findAgentByRole(cfg, "reviewer")

	label := "Task"
	if task.Kind == store.KindEpic {
		label = "Epic"
	}

	fmt.Printf("%s╔══════════════════════════════════════╗%s\n", colorBold, colorReset)
	fmt.Printf("%s║  hive auto — full pipeline           ║%s\n", colorBold, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════╝%s\n\n", colorBold, colorReset)

	fmt.Printf("  %s:     %s#%d%s %s\n", label, colorYellow, task.ID, colorReset, task.Title)
	if task.GitBranch != "" {
		fmt.Printf("  Branch:   %s%s%s\n", colorCyan, task.GitBranch, colorReset)
	}
	if pmName != "" {
		fmt.Printf("  PM:       %s%s%s\n", colorCyan, pmName, colorReset)
	}
	if coderName != "" {
		fmt.Printf("  Coder:    %s%s%s\n", colorCyan, coderName, colorReset)
	}
	if reviewerName != "" {
		fmt.Printf("  Reviewer: %s%s%s\n", colorCyan, reviewerName, colorReset)
	}
	fmt.Printf("  Max fix loops: %d\n", autoMaxLoops)
	if autoParallel > 1 {
		fmt.Printf("  Parallel:  %s%d workers%s\n", colorCyan, autoParallel, colorReset)
	}
	fmt.Println()

	// Record pipeline run for crash recovery.
	var pipelineRunID int64
	if task.Kind == store.KindEpic {
		pipelineRunID, _ = s.StartPipelineRun(task.ID, autoMaxLoops, autoParallel)
		if pipelineRunID > 0 {
			// Ensure we mark the run as ended when we exit (crash safety).
			defer func() {
				// If we haven't ended it yet (panic or early return), mark interrupted.
				if run, _ := s.GetActivePipelineRun(task.ID); run != nil && run.ID == pipelineRunID {
					s.EndPipelineRun(pipelineRunID, "interrupted")
				}
			}()
		}
	}

	// ══════════════════════════════════════
	// STEP 1: Plan
	// ══════════════════════════════════════
	var subtasks []store.Task

	if !autoSkipPlan {
		printPhase("1", "PLAN", "Breaking task into subtasks")

		if pmName == "" {
			fmt.Printf("  %s⚠ No PM agent configured, skipping plan.%s\n", colorYellow, colorReset)
			fmt.Printf("  Will run coder directly on the main task.\n\n")
			subtasks = []store.Task{*task}
		} else {
			planned, err := autoPlan(s, cfg, task, pmName, pmCfg, workDir)
			if err != nil {
				return fmt.Errorf("plan failed: %w", err)
			}
			if planned == nil {
				// PM blocked — stop and ask user.
				return nil
			}
			subtasks = planned
		}
	} else {
		printPhase("1", "PLAN", "Skipped (--skip-plan)")
		// Gather existing subtasks.
		subtasks, _ = s.ListTasksByEpic(task.ID)
		if len(subtasks) == 0 {
			// Fallback: check old-style parent_id children.
			allTasks, _ := s.ListTasks("")
			for _, t := range allTasks {
				if t.ParentID != nil && *t.ParentID == task.ID {
					subtasks = append(subtasks, t)
				}
			}
		}
		if len(subtasks) == 0 {
			subtasks = []store.Task{*task}
		}
		fmt.Printf("  Found %d tasks\n\n", len(subtasks))
	}

	// ══════════════════════════════════════
	// STEP 2: Auto-assign
	// ══════════════════════════════════════
	printPhase("2", "ASSIGN", "Assigning agents to subtasks")

	for i := range subtasks {
		t := &subtasks[i]
		if t.AssignedAgent != "" {
			fmt.Printf("  #%d already assigned to %s%s%s\n", t.ID, colorCyan, t.AssignedAgent, colorReset)
			continue
		}
		if coderName != "" {
			s.AssignTask(t.ID, coderName, "coder")
			t.AssignedAgent = coderName
			t.Role = "coder"
			fmt.Printf("  #%d → %s%s%s (coder)\n", t.ID, colorCyan, coderName, colorReset)
		} else {
			fmt.Printf("  %s⚠ #%d has no agent and no coder configured%s\n", colorYellow, t.ID, colorReset)
		}
	}
	fmt.Println()

	// ══════════════════════════════════════
	// STEP 3: Code + Review loop per task
	// ══════════════════════════════════════
	completed := 0
	failed := 0
	blocked := 0

	if autoParallel > 1 && len(subtasks) > 1 {
		// Parallel execution using worker pool.
		printPhase("3", "WORK", fmt.Sprintf("Running %d tasks (%d parallel)", len(subtasks), autoParallel))

		pool := worker.NewPool(worker.PoolConfig{
			Store:      s,
			Config:     cfg,
			WorkDir:    workDir,
			EpicBranch: task.GitBranch,
			MaxWorkers: autoParallel,
			MaxLoops:   autoMaxLoops,
			CoderName:  coderName,
			CoderCfg:   coderCfg,
			ReviewName: reviewerName,
			ReviewCfg:  reviewerCfg,
		})

		results := pool.Run(subtasks)

		for _, r := range results {
			statusIcon := "✗"
			statusColor := colorRed
			switch r.Status {
			case "done":
				statusIcon = "✓"
				statusColor = colorGreen
				completed++
			case "blocked":
				statusIcon = "⚠"
				statusColor = colorYellow
				blocked++
			default:
				failed++
			}

			fmt.Printf("  %s%s%s %s#%d%s %s",
				statusColor, statusIcon, colorReset,
				colorYellow, r.TaskID, colorReset,
				r.Title)
			if r.Duration > 0 {
				fmt.Printf(" %s(%.1fs)%s", colorDim, r.Duration.Seconds(), colorReset)
			}
			fmt.Println()

			// Show log entries indented.
			for _, line := range r.Log {
				fmt.Printf("    %s%s%s\n", colorDim, line, colorReset)
			}
		}
		fmt.Println()
	} else {
		// Sequential execution (original behavior).
		for i, subtask := range subtasks {
			printPhase("3", fmt.Sprintf("WORK %d/%d", i+1, len(subtasks)),
				fmt.Sprintf("#%d: %s", subtask.ID, subtask.Title))

			if subtask.Status == store.StatusDone {
				fmt.Printf("  %s✓ Already done%s\n\n", colorGreen, colorReset)
				completed++
				continue
			}

			if subtask.Status == store.StatusBlocked {
				fmt.Printf("  %s⚠ Blocked: %s%s\n", colorRed, subtask.BlockedReason, colorReset)
				fmt.Printf("  → %shive answer %d \"...\" && hive auto %d --skip-plan%s\n\n",
					colorCyan, subtask.ID, task.ID, colorReset)
				blocked++
				continue
			}

			if subtask.AssignedAgent == "" {
				fmt.Printf("  %s⚠ No agent assigned, skipping%s\n\n", colorYellow, colorReset)
				continue
			}

			// Run fix loop for this subtask.
			result := autoFixLoop(s, cfg, &subtask, coderName, coderCfg, reviewerName, reviewerCfg, workDir, autoMaxLoops)

			switch result {
			case "done":
				completed++
			case "blocked":
				blocked++
			default:
				failed++
			}
		}
	}

	// ══════════════════════════════════════
	// SUMMARY
	// ══════════════════════════════════════
	fmt.Printf("%s╔══════════════════════════════════════╗%s\n", colorBold, colorReset)
	fmt.Printf("%s║  Pipeline complete                   ║%s\n", colorBold, colorReset)
	fmt.Printf("%s╚══════════════════════════════════════╝%s\n\n", colorBold, colorReset)

	fmt.Printf("  Total subtasks: %d\n", len(subtasks))
	if completed > 0 {
		fmt.Printf("  %s✓ Completed: %d%s\n", colorGreen, completed, colorReset)
	}
	if blocked > 0 {
		fmt.Printf("  %s⚠ Blocked:   %d%s (answer blockers, then re-run with --skip-plan)\n", colorYellow, blocked, colorReset)
	}
	if failed > 0 {
		fmt.Printf("  %s✗ Failed:    %d%s\n", colorRed, failed, colorReset)
	}

	// End pipeline run tracking.
	if pipelineRunID > 0 {
		endStatus := "completed"
		if failed > 0 {
			endStatus = "failed"
		} else if blocked > 0 {
			endStatus = "blocked"
		}
		s.EndPipelineRun(pipelineRunID, endStatus)
	}

	if completed == len(subtasks) {
		if task.Kind == store.KindEpic {
			// Don't mark epic as done yet — user needs to accept/reject.
			s.UpdateTaskStatus(task.ID, store.StatusReview)
			fmt.Printf("\n  %sAll tasks complete!%s\n", colorGreen+colorBold, colorReset)

			// Commit all work on the safety branch.
			if task.GitBranch != "" {
				safety := git.New(workDir)
				committed, err := safety.CommitAll(fmt.Sprintf("hive: completed epic #%d — %s", task.ID, task.Title))
				if err != nil {
					fmt.Printf("  %s⚠  Could not commit: %v%s\n", colorYellow, err, colorReset)
				} else if committed {
					fmt.Printf("  Committed changes on %s%s%s\n", colorCyan, task.GitBranch, colorReset)
				}

				baseBranch, _ := safety.BaseBranch()
				stat, _ := safety.DiffStat(baseBranch, task.GitBranch)
				if stat != "" {
					fmt.Printf("\n  %sChanges:%s\n", colorBold, colorReset)
					for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
						fmt.Printf("    %s\n", line)
					}
				}
			}

			fmt.Printf("\n  Review and accept: %shive epic accept %d%s\n", colorCyan, task.ID, colorReset)
			fmt.Printf("  Or reject:         %shive epic reject %d%s\n", colorCyan, task.ID, colorReset)
			fmt.Printf("  View full diff:    %shive epic diff %d%s\n", colorCyan, task.ID, colorReset)
		} else {
			s.UpdateTaskStatus(task.ID, store.StatusDone)
			fmt.Printf("\n  %s✓ Task #%d fully completed!%s\n", colorGreen+colorBold, task.ID, colorReset)
		}
	}

	return nil
}

// autoPlan runs the PM agent and creates subtasks.
func autoPlan(s *store.Store, cfg *config.Config, task *store.Task, pmName string, pmCfg config.Agent, workDir string) ([]store.Task, error) {
	ctxBuilder := agentctx.New(s)
	prompt, err := ctxBuilder.BuildPrompt(task, "pm")
	if err != nil {
		return nil, err
	}

	runner, err := agent.NewRunner(pmName, pmCfg)
	if err != nil {
		return nil, err
	}

	fmt.Printf("  Running %s%s%s...\n", colorCyan, pmName, colorReset)

	resp, err := runner.Run(context.Background(), agent.Request{
		TaskID:     task.ID,
		Prompt:     prompt,
		WorkDir:    workDir,
		TimeoutSec: pmCfg.DefaultTimeout(),
	})
	if err != nil {
		return nil, err
	}

	// Save artifact.
	artifactPath := hivePath("runs", fmt.Sprintf("task-%d-auto-plan.md", task.ID))
	os.MkdirAll(hivePath("runs"), 0755)
	os.WriteFile(artifactPath, []byte(resp.Output), 0644)
	s.AddArtifact(task.ID, "plan", artifactPath)

	// Check for blocker.
	if b := agent.ParseBlocked(resp.Output); b != "" {
		s.BlockTask(task.ID, b)
		fmt.Printf("  %s⚠ PM needs your input:%s %s\n", colorRed+colorBold, colorReset, b)
		fmt.Printf("  → %shive answer %d \"...\" && hive auto %d%s\n", colorCyan, task.ID, task.ID, colorReset)
		return nil, nil
	}

	// Parse subtasks.
	parsed := agent.ParseSubtasks(resp.Output)

	if len(parsed) == 0 {
		fmt.Printf("  PM didn't return structured subtasks. Running coder on main task.\n\n")
		return []store.Task{*task}, nil
	}

	var subtasks []store.Task
	for _, sub := range parsed {
		parentID := task.ID
		created, err := s.CreateTask(sub.Title, sub.Description, sub.Priority, &parentID)
		if err != nil {
			continue
		}
		subtasks = append(subtasks, *created)
		priColor := priorityColor(sub.Priority)
		fmt.Printf("  %s#%d%s %s%s%s [%s]\n", colorYellow, created.ID, colorReset, priColor, sub.Title, colorReset, sub.Priority)
	}

	fmt.Printf("  Created %d subtasks\n\n", len(subtasks))

	s.AddEvent(task.ID, pmName, "planned", fmt.Sprintf("Auto-created %d subtasks", len(subtasks)))
	return subtasks, nil
}

// autoFixLoop runs code → review → fix for a single task. Returns "done", "blocked", or "failed".
func autoFixLoop(
	s *store.Store, cfg *config.Config,
	task *store.Task,
	coderName string, coderCfg config.Agent,
	reviewerName string, reviewerCfg config.Agent,
	workDir string,
	maxLoops int,
) string {
	ctxBuilder := agentctx.New(s)

	// If no reviewer, just run coder and done.
	if reviewerName == "" {
		result := runCoderOnce(s, ctxBuilder, task, coderName, coderCfg, workDir, 0)
		if result == "blocked" {
			return "blocked"
		}
		if result == "failed" {
			return "failed"
		}
		s.UpdateTaskStatus(task.ID, store.StatusDone)
		fmt.Printf("  %s✓ Done%s (no reviewer configured)\n\n", colorGreen, colorReset)
		return "done"
	}

	coderRunner, err := agent.NewRunner(coderName, coderCfg)
	if err != nil {
		fmt.Printf("  %s✗ Failed to create coder: %v%s\n\n", colorRed, err, colorReset)
		return "failed"
	}

	reviewerRunner, err := agent.NewRunner(reviewerName, reviewerCfg)
	if err != nil {
		fmt.Printf("  %s✗ Failed to create reviewer: %v%s\n\n", colorRed, err, colorReset)
		return "failed"
	}

	for iteration := 1; iteration <= maxLoops; iteration++ {
		// Re-fetch task for latest context.
		task, _ = s.GetTask(task.ID)

		// === CODER ===
		s.UpdateTaskStatus(task.ID, store.StatusInProgress)
		fmt.Printf("  [%d/%d] %s%s%s coding... ", iteration, maxLoops, colorBlue, coderName, colorReset)

		coderPrompt, _ := ctxBuilder.BuildPrompt(task, "coder")
		coderResp, err := coderRunner.Run(context.Background(), agent.Request{
			TaskID: task.ID, Prompt: coderPrompt, WorkDir: workDir, TimeoutSec: coderCfg.DefaultTimeout(),
		})
		if err != nil {
			s.UpdateTaskStatus(task.ID, store.StatusFailed)
			fmt.Printf("%s✗ error%s\n\n", colorRed, colorReset)
			return "failed"
		}

		// Save artifact.
		artifactPath := hivePath("runs", fmt.Sprintf("task-%d-auto-code-iter%d.md", task.ID, iteration))
		os.MkdirAll(hivePath("runs"), 0755)
		os.WriteFile(artifactPath, []byte(coderResp.Output), 0644)
		s.AddArtifact(task.ID, "code", artifactPath)

		preview := coderResp.Output
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		s.AddEvent(task.ID, coderName, "agent_output", preview)

		fmt.Printf("%.1fs ", coderResp.Duration)

		// Check blocked.
		if b := agent.ParseBlocked(coderResp.Output); b != "" {
			s.BlockTask(task.ID, b)
			fmt.Printf("%s⚠ BLOCKED%s\n", colorYellow, colorReset)
			fmt.Printf("    %s\n", b)
			fmt.Printf("    → %shive answer %d \"...\"%s\n\n", colorCyan, task.ID, colorReset)
			return "blocked"
		}

		if coderResp.ExitCode != 0 {
			s.UpdateTaskStatus(task.ID, store.StatusFailed)
			fmt.Printf("%s✗ exit %d%s\n\n", colorRed, coderResp.ExitCode, colorReset)
			return "failed"
		}

		// === REVIEWER ===
		s.UpdateTaskStatus(task.ID, store.StatusReview)
		fmt.Printf("→ %s%s%s reviewing... ", colorMagenta, reviewerName, colorReset)

		reviewPrompt, _ := ctxBuilder.BuildReviewPrompt(task)
		reviewResp, err := reviewerRunner.Run(context.Background(), agent.Request{
			TaskID: task.ID, Prompt: reviewPrompt, WorkDir: workDir, TimeoutSec: reviewerCfg.DefaultTimeout(),
		})
		if err != nil {
			fmt.Printf("%s✗ error%s\n\n", colorRed, colorReset)
			continue
		}

		// Save artifact.
		reviewPath := hivePath("runs", fmt.Sprintf("task-%d-auto-review-iter%d.md", task.ID, iteration))
		os.WriteFile(reviewPath, []byte(reviewResp.Output), 0644)
		s.AddArtifact(task.ID, "review", reviewPath)

		review := agent.ParseReview(reviewResp.Output)

		switch review.Verdict {
		case "APPROVE":
			s.AddReview(task.ID, reviewerName, "approve", reviewResp.Output)
			s.UpdateTaskStatus(task.ID, store.StatusDone)
			fmt.Printf("%s✓ APPROVED%s (%.1fs)\n", colorGreen+colorBold, colorReset, reviewResp.Duration)
			if len(review.Comments) > 0 {
				for _, c := range review.Comments {
					fmt.Printf("    %s•%s %s\n", colorDim, colorReset, c)
				}
			}

			// Commit the approved work on the safety branch.
			safety := git.New(workDir)
			if safety.IsGitRepo() {
				msg := fmt.Sprintf("hive: task #%d — %s", task.ID, task.Title)
				committed, err := safety.CommitAll(msg)
				if err != nil {
					fmt.Printf("    %s⚠ commit: %v%s\n", colorYellow, err, colorReset)
				} else if committed {
					fmt.Printf("    %scommitted%s\n", colorDim, colorReset)
				}
			}

			fmt.Println()
			return "done"

		case "REJECT":
			s.AddReview(task.ID, reviewerName, "reject", reviewResp.Output)
			s.UpdateTaskStatus(task.ID, store.StatusBacklog)
			fmt.Printf("%s✗ REJECTED%s (%.1fs)\n", colorRed, colorReset, reviewResp.Duration)
			for _, c := range review.Comments {
				fmt.Printf("    %s•%s %s\n", colorRed, colorReset, c)
			}
			// Add review comments to event history for coder's next iteration.
			var comments strings.Builder
			for _, c := range review.Comments {
				comments.WriteString("- " + c + "\n")
			}
			s.AddEvent(task.ID, reviewerName, "reviewed",
				fmt.Sprintf("REJECTED (iter %d):\n%s", iteration, comments.String()))

		default:
			fmt.Printf("%s? no verdict%s (%.1fs)\n", colorYellow, colorReset, reviewResp.Duration)
			s.AddEvent(task.ID, reviewerName, "reviewed", "No clear verdict")
		}
	}

	// Max iterations reached.
	s.UpdateTaskStatus(task.ID, store.StatusFailed)
	fmt.Printf("  %s✗ Max iterations reached%s\n\n", colorRed, colorReset)
	return "failed"
}

// runCoderOnce runs coder agent once without review.
func runCoderOnce(s *store.Store, ctxBuilder *agentctx.Builder, task *store.Task, coderName string, coderCfg config.Agent, workDir string, iteration int) string {
	runner, err := agent.NewRunner(coderName, coderCfg)
	if err != nil {
		fmt.Printf("  %s✗ Failed: %v%s\n\n", colorRed, err, colorReset)
		return "failed"
	}

	s.UpdateTaskStatus(task.ID, store.StatusInProgress)
	fmt.Printf("  %s%s%s coding... ", colorBlue, coderName, colorReset)

	prompt, _ := ctxBuilder.BuildPrompt(task, "coder")
	resp, err := runner.Run(context.Background(), agent.Request{
		TaskID: task.ID, Prompt: prompt, WorkDir: workDir, TimeoutSec: coderCfg.DefaultTimeout(),
	})
	if err != nil {
		s.UpdateTaskStatus(task.ID, store.StatusFailed)
		fmt.Printf("%s✗ error%s\n", colorRed, colorReset)
		return "failed"
	}

	preview := resp.Output
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	s.AddEvent(task.ID, coderName, "agent_output", preview)

	if b := agent.ParseBlocked(resp.Output); b != "" {
		s.BlockTask(task.ID, b)
		fmt.Printf("%s⚠ BLOCKED: %s%s\n", colorYellow, b, colorReset)
		return "blocked"
	}

	if resp.ExitCode != 0 {
		s.UpdateTaskStatus(task.ID, store.StatusFailed)
		fmt.Printf("%s✗ exit %d%s\n", colorRed, resp.ExitCode, colorReset)
		return "failed"
	}

	fmt.Printf("%.1fs %s✓%s\n", resp.Duration, colorGreen, colorReset)
	return "done"
}

// findAgentByRole returns the first agent with the given role.
func findAgentByRole(cfg *config.Config, role string) (string, config.Agent) {
	for name, a := range cfg.Agents {
		if a.Role == role {
			return name, a
		}
	}
	return "", config.Agent{}
}

func printPhase(num, label, desc string) {
	fmt.Printf("%s═══ %s: %s%s — %s\n\n", colorBold, num, label, colorReset, desc)
}
