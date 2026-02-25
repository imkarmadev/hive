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
	"github.com/spf13/cobra"
)

var answerCmd = &cobra.Command{
	Use:   "answer [task-id] [answer]",
	Short: "Answer a blocker and auto-continue the pipeline",
	Long: `Resolves a blocked task by providing the requested information,
then automatically continues the pipeline for that task:

  1. Unblocks the task with your answer
  2. Runs architect (if it was the architect who blocked)
  3. If architect is happy → coder → reviewer loop
  4. Commits approved work on the epic's safety branch

Use "skip" as the answer to cancel the task instead:
  hive answer 5 skip`,
	Args: cobra.MinimumNArgs(2),
	RunE: runAnswer,
}

var answerMaxLoops int

func init() {
	answerCmd.Flags().IntVar(&answerMaxLoops, "max-loops", 3, "Maximum code-review iterations")
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

	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("task #%d not found", id)
	}
	if task.Status != store.StatusBlocked {
		return fmt.Errorf("task #%d is not blocked (status: %s)", id, task.Status)
	}

	answer := strings.Join(args[1:], " ")

	// Special: "skip" cancels the task.
	if strings.ToLower(strings.TrimSpace(answer)) == "skip" {
		s.UpdateTaskStatus(id, store.StatusCancelled)
		s.AddEvent(id, "user", "cancelled", "User skipped blocked task")
		fmt.Printf("Cancelled task #%d — pipeline will skip it.\n", id)
		return nil
	}

	// Unblock.
	if err := s.UnblockTask(id, answer); err != nil {
		return err
	}

	fmt.Printf("Unblocked task #%d\n", id)
	fmt.Printf("  Question: %s\n", task.BlockedReason)
	fmt.Printf("  Answer:   %s\n\n", answer)

	// Load config.
	cfg, err := config.Load(hivePath("config.yaml"))
	if err != nil {
		fmt.Printf("  %s⚠ No config found — unblocked but not auto-running.%s\n", colorYellow, colorReset)
		return nil
	}

	workDir, _ := os.Getwd()

	// Ensure we're on the epic's safety branch.
	if task.ParentID != nil {
		epic, err := s.GetTask(*task.ParentID)
		if err == nil && epic.GitBranch != "" {
			safety := git.New(workDir)
			if safety.IsGitRepo() {
				current, _ := safety.CurrentBranch()
				if current != epic.GitBranch {
					safety.CreateBranch(epic.GitBranch)
				}
			}
		}
	}

	// Find agents.
	archName, archCfg := findAgentByRole(cfg, "architect")
	coderName, coderCfg := findAgentByRole(cfg, "coder")
	reviewerName, reviewerCfg := findAgentByRole(cfg, "reviewer")

	forceAutoAccept(&archCfg)
	forceAutoAccept(&coderCfg)
	forceAutoAccept(&reviewerCfg)

	// Re-fetch task after unblock.
	task, _ = s.GetTask(id)

	// Determine what blocked: check if architect_spec event exists.
	hasArchSpec := false
	events, _ := s.GetEvents(task.ID)
	for _, e := range events {
		if e.Type == "architect_spec" {
			hasArchSpec = true
		}
	}

	ctxBuilder := agentctx.New(s)

	// Step 1: If no architect spec yet, run architect first.
	if !hasArchSpec && archName != "" {
		fmt.Printf("  Running %s%s%s (architect)... ", colorCyan, archName, colorReset)

		archRunner, err := agent.NewRunner(archName, archCfg)
		if err != nil {
			return fmt.Errorf("create architect runner: %w", err)
		}

		archPrompt, _ := ctxBuilder.BuildPrompt(task, "architect")
		resp, err := archRunner.Run(context.Background(), agent.Request{
			TaskID: task.ID, Prompt: archPrompt, WorkDir: workDir,
			TimeoutSec: archCfg.DefaultTimeout(),
		})
		if err != nil {
			fmt.Printf("%s✗ error%s\n", colorRed, colorReset)
			return fmt.Errorf("architect failed: %w", err)
		}

		// Save artifact.
		artifactPath := hivePath("runs", fmt.Sprintf("task-%d-architect.md", task.ID))
		os.MkdirAll(hivePath("runs"), 0755)
		os.WriteFile(artifactPath, []byte(resp.Output), 0644)
		s.AddArtifact(task.ID, "architect", artifactPath)

		// Check if architect blocked again.
		if b := agent.ParseBlocked(resp.Output); b != "" {
			s.BlockTask(task.ID, b)
			fmt.Printf("%s⚠ BLOCKED again%s\n", colorYellow, colorReset)
			fmt.Printf("    %s\n", b)
			fmt.Printf("    → %shive answer %d \"...\"%s\n", colorCyan, task.ID, colorReset)
			return nil
		}

		// Save architect spec.
		spec := resp.Output
		if len(spec) > 4000 {
			spec = spec[:4000] + "\n\n... (spec truncated)"
		}
		s.AddEvent(task.ID, archName, "architect_spec", spec)
		fmt.Printf("%s✓ spec written%s (%.1fs)\n", colorGreen, colorReset, resp.Duration)
	}

	// Step 2: Code → review loop.
	if coderName == "" {
		fmt.Printf("  %s⚠ No coder agent configured.%s\n", colorYellow, colorReset)
		return nil
	}

	fmt.Printf("  Starting code → review loop (max %d iterations)\n\n", answerMaxLoops)

	result := autoFixLoop(s, cfg, task, coderName, coderCfg, reviewerName, reviewerCfg, workDir, answerMaxLoops)

	switch result {
	case "done":
		// Commit on safety branch.
		safety := git.New(workDir)
		if safety.IsGitRepo() {
			msg := fmt.Sprintf("hive: task #%d — %s", task.ID, task.Title)
			if committed, err := safety.CommitAll(msg); err == nil && committed {
				fmt.Printf("    %scommitted%s\n", colorDim, colorReset)
			}
		}
	case "blocked":
		fmt.Printf("  Coder blocked again — answer and re-run.\n")
	case "failed":
		fmt.Printf("  Task failed after %d iterations.\n", answerMaxLoops)
	}

	return nil
}
