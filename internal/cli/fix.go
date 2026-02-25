package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/imkarma/hive/internal/agent"
	"github.com/imkarma/hive/internal/config"
	agentctx "github.com/imkarma/hive/internal/context"
	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var fixCmd = &cobra.Command{
	Use:   "fix [task-id]",
	Short: "Run code → review → fix loop on a task",
	Long: `Runs an automated loop:
  1. Coder agent works on the task
  2. Reviewer agent reviews the changes
  3. If rejected → coder fixes → reviewer reviews again
  4. Loop until approved or max iterations reached

The coder receives review comments as context for each fix iteration.`,
	Args: cobra.ExactArgs(1),
	RunE: runFix,
}

var (
	fixMaxLoops    int
	fixCoderAgent  string
	fixReviewAgent string
)

func init() {
	fixCmd.Flags().IntVar(&fixMaxLoops, "max-loops", 3, "Maximum fix-review iterations")
	fixCmd.Flags().StringVar(&fixCoderAgent, "coder", "", "Override coder agent name")
	fixCmd.Flags().StringVar(&fixReviewAgent, "reviewer", "", "Override reviewer agent name")
	rootCmd.AddCommand(fixCmd)
}

func runFix(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	cfg, err := config.Load(hivePath("config.yaml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Get the task.
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid task ID: %s", args[0])
	}
	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("task #%d not found", id)
	}

	// Find coder agent.
	coderName := fixCoderAgent
	if coderName == "" {
		if task.AssignedAgent != "" {
			coderName = task.AssignedAgent
		} else {
			coders := cfg.AgentsByRole("coder")
			for name := range coders {
				coderName = name
				break
			}
		}
	}
	if coderName == "" {
		return fmt.Errorf("no coder agent found. Assign one or use --coder flag")
	}

	// Find reviewer agent.
	reviewerName := fixReviewAgent
	if reviewerName == "" {
		reviewers := cfg.AgentsByRole("reviewer")
		for name := range reviewers {
			reviewerName = name
			break
		}
	}
	if reviewerName == "" {
		return fmt.Errorf("no reviewer agent configured. Add an agent with role: reviewer")
	}

	coderCfg := cfg.Agents[coderName]
	reviewerCfg := cfg.Agents[reviewerName]

	// Force auto_accept for CLI agents to prevent interactive prompts.
	forceAutoAccept(&coderCfg)
	forceAutoAccept(&reviewerCfg)

	coderRunner, err := agent.NewRunner(coderName, coderCfg)
	if err != nil {
		return fmt.Errorf("create coder runner: %w", err)
	}
	reviewerRunner, err := agent.NewRunner(reviewerName, reviewerCfg)
	if err != nil {
		return fmt.Errorf("create reviewer runner: %w", err)
	}

	workDir, _ := os.Getwd()
	ctxBuilder := agentctx.New(s)

	fmt.Printf("%s═══ Fix Loop: Task #%d ═══%s\n", colorBold, task.ID, colorReset)
	fmt.Printf("  Task:     %s\n", task.Title)
	fmt.Printf("  Coder:    %s%s%s\n", colorCyan, coderName, colorReset)
	fmt.Printf("  Reviewer: %s%s%s\n", colorCyan, reviewerName, colorReset)
	fmt.Printf("  Max loops: %d\n\n", fixMaxLoops)

	for iteration := 1; iteration <= fixMaxLoops; iteration++ {
		fmt.Printf("%s── Iteration %d/%d ──%s\n\n", colorBold, iteration, fixMaxLoops, colorReset)

		// Re-fetch task to get latest events/history.
		task, _ = s.GetTask(id)

		// === STEP 1: Coder ===
		fmt.Printf("%s[coder]%s %s working...\n", colorBlue, colorReset, coderName)
		s.UpdateTaskStatus(task.ID, store.StatusInProgress)

		coderPrompt, err := ctxBuilder.BuildPrompt(task, "coder")
		if err != nil {
			return fmt.Errorf("build coder prompt: %w", err)
		}

		coderResp, err := coderRunner.Run(context.Background(), agent.Request{
			TaskID:     task.ID,
			Prompt:     coderPrompt,
			WorkDir:    workDir,
			TimeoutSec: coderCfg.DefaultTimeout(),
		})
		if err != nil {
			s.UpdateTaskStatus(task.ID, store.StatusFailed)
			return fmt.Errorf("coder failed: %w", err)
		}

		// Save coder output.
		coderArtifact := hivePath("runs", fmt.Sprintf("task-%d-code-iter%d.md", task.ID, iteration))
		os.MkdirAll(hivePath("runs"), 0755)
		os.WriteFile(coderArtifact, []byte(coderResp.Output), 0644)
		s.AddArtifact(task.ID, "code", coderArtifact)

		outputPreview := coderResp.Output
		if len(outputPreview) > 200 {
			outputPreview = outputPreview[:200] + "..."
		}
		s.AddEvent(task.ID, coderName, "agent_output", outputPreview)

		fmt.Printf("  Done (%.1fs)\n", coderResp.Duration)

		// Check for blocker from coder.
		if blocked := agent.ParseBlocked(coderResp.Output); blocked != "" {
			s.BlockTask(task.ID, blocked)
			fmt.Printf("\n%s⚠  Coder needs your input:%s %s\n", colorRed+colorBold, colorReset, blocked)
			fmt.Printf("   → %shive answer %d \"your answer\"%s\n", colorCyan, task.ID, colorReset)
			fmt.Printf("   Then re-run: %shive fix %d%s\n", colorCyan, task.ID, colorReset)
			return nil
		}

		if coderResp.ExitCode != 0 {
			s.UpdateTaskStatus(task.ID, store.StatusFailed)
			fmt.Printf("\n%s✗ Coder failed (exit code %d)%s\n", colorRed, coderResp.ExitCode, colorReset)
			return nil
		}

		// === STEP 2: Reviewer ===
		fmt.Printf("%s[reviewer]%s %s reviewing...\n", colorMagenta, colorReset, reviewerName)
		s.UpdateTaskStatus(task.ID, store.StatusReview)

		reviewPrompt, err := ctxBuilder.BuildReviewPrompt(task)
		if err != nil {
			return fmt.Errorf("build review prompt: %w", err)
		}

		reviewResp, err := reviewerRunner.Run(context.Background(), agent.Request{
			TaskID:     task.ID,
			Prompt:     reviewPrompt,
			WorkDir:    workDir,
			TimeoutSec: reviewerCfg.DefaultTimeout(),
		})
		if err != nil {
			return fmt.Errorf("reviewer failed: %w", err)
		}

		// Save review output.
		reviewArtifact := hivePath("runs", fmt.Sprintf("task-%d-review-iter%d.md", task.ID, iteration))
		os.WriteFile(reviewArtifact, []byte(reviewResp.Output), 0644)
		s.AddArtifact(task.ID, "review", reviewArtifact)

		review := agent.ParseReview(reviewResp.Output)

		switch review.Verdict {
		case "APPROVE":
			s.AddReview(task.ID, reviewerName, "approve", reviewResp.Output)
			s.UpdateTaskStatus(task.ID, store.StatusDone)

			fmt.Printf("  %s✓ APPROVED%s (%.1fs)\n", colorGreen+colorBold, colorReset, reviewResp.Duration)
			if len(review.Comments) > 0 {
				for _, c := range review.Comments {
					fmt.Printf("    %s•%s %s\n", colorGreen, colorReset, c)
				}
			}

			// Commit approved work.
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

			fmt.Printf("\n%s═══ Task #%d completed in %d iteration(s) ═══%s\n", colorGreen+colorBold, task.ID, iteration, colorReset)
			return nil

		case "REJECT":
			s.AddReview(task.ID, reviewerName, "reject", reviewResp.Output)
			s.UpdateTaskStatus(task.ID, store.StatusBacklog)

			fmt.Printf("  %s✗ REJECTED%s (%.1fs)\n", colorRed+colorBold, colorReset, reviewResp.Duration)
			if len(review.Comments) > 0 {
				for _, c := range review.Comments {
					fmt.Printf("    %s•%s %s\n", colorRed, colorReset, c)
				}
			}

			if iteration < fixMaxLoops {
				// Add review comments as event so coder sees them next iteration.
				comments := ""
				for _, c := range review.Comments {
					comments += "- " + c + "\n"
				}
				s.AddEvent(task.ID, reviewerName, "reviewed",
					fmt.Sprintf("REJECTED (iteration %d). Issues:\n%s", iteration, comments))
				fmt.Printf("\n  Retrying... (iteration %d/%d)\n\n", iteration+1, fixMaxLoops)
			}

		default:
			s.AddEvent(task.ID, reviewerName, "reviewed", "No clear verdict")
			fmt.Printf("  %s? No clear verdict%s (%.1fs)\n", colorYellow, colorReset, reviewResp.Duration)
			fmt.Println("  Raw output:", reviewResp.Output)

			if iteration < fixMaxLoops {
				fmt.Printf("\n  Retrying... (iteration %d/%d)\n\n", iteration+1, fixMaxLoops)
			}
		}
	}

	// Max loops reached without approval.
	s.UpdateTaskStatus(task.ID, store.StatusFailed)
	fmt.Printf("\n%s═══ Max iterations reached (%d). Task #%d needs manual attention. ═══%s\n",
		colorRed+colorBold, fixMaxLoops, task.ID, colorReset)
	fmt.Printf("Check artifacts: %shive log %d%s\n", colorCyan, task.ID, colorReset)

	return nil
}
