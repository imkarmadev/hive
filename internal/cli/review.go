package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/imkarma/hive/internal/agent"
	"github.com/imkarma/hive/internal/config"
	agentctx "github.com/imkarma/hive/internal/context"
	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review [task-id]",
	Short: "Run cross-model code review on a task",
	Long: `Sends the task to a reviewer-role agent for code review.
The reviewer sees the task context, git diff, and any artifacts.

If the review is REJECT, the task moves back to backlog for fixes.
If APPROVE, the task is marked as done.`,
	Args: cobra.ExactArgs(1),
	RunE: runReview,
}

var reviewAgent string

func init() {
	reviewCmd.Flags().StringVarP(&reviewAgent, "agent", "a", "", "Override reviewer agent name")
	rootCmd.AddCommand(reviewCmd)
}

func runReview(cmd *cobra.Command, args []string) error {
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

	// Find reviewer agent.
	agentName := reviewAgent
	if agentName == "" {
		reviewers := cfg.AgentsByRole("reviewer")
		for name := range reviewers {
			agentName = name
			break
		}
	}
	if agentName == "" {
		return fmt.Errorf("no reviewer agent configured. Add an agent with role: reviewer in .hive/config.yaml")
	}

	agentCfg, ok := cfg.Agents[agentName]
	if !ok {
		return fmt.Errorf("agent %q not found in config", agentName)
	}

	// Build review context with git diff.
	ctxBuilder := agentctx.New(s)
	prompt, err := ctxBuilder.BuildReviewPrompt(task)
	if err != nil {
		return fmt.Errorf("build review context: %w", err)
	}

	// Create runner.
	runner, err := agent.NewRunner(agentName, agentCfg)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	workDir, _ := os.Getwd()

	// Move task to review status.
	s.UpdateTaskStatus(task.ID, store.StatusReview)

	fmt.Printf("Reviewing task #%d: %s\n", task.ID, task.Title)
	fmt.Printf("  Reviewer: %s\n\n", agentName)

	// Run reviewer.
	resp, err := runner.Run(context.Background(), agent.Request{
		TaskID:     task.ID,
		Prompt:     prompt,
		WorkDir:    workDir,
		TimeoutSec: agentCfg.DefaultTimeout(),
	})
	if err != nil {
		s.UpdateTaskStatus(task.ID, store.StatusFailed)
		return fmt.Errorf("reviewer failed: %w", err)
	}

	// Save output as artifact.
	artifactPath := hivePath("runs", fmt.Sprintf("task-%d-review.md", task.ID))
	os.MkdirAll(hivePath("runs"), 0755)
	os.WriteFile(artifactPath, []byte(resp.Output), 0644)
	s.AddArtifact(task.ID, "review", artifactPath)

	// Parse review verdict.
	review := agent.ParseReview(resp.Output)

	switch review.Verdict {
	case "APPROVE":
		s.AddReview(task.ID, agentName, "approve", resp.Output)
		s.UpdateTaskStatus(task.ID, store.StatusDone)
		fmt.Printf("%s✓ APPROVED%s\n", colorGreen+colorBold, colorReset)
		if len(review.Comments) > 0 {
			fmt.Println("\nComments:")
			for _, c := range review.Comments {
				fmt.Printf("  %s•%s %s\n", colorGreen, colorReset, c)
			}
		}
		fmt.Printf("\nTask #%d marked as done.\n", task.ID)

	case "REJECT":
		s.AddReview(task.ID, agentName, "reject", resp.Output)
		s.UpdateTaskStatus(task.ID, store.StatusBacklog)
		fmt.Printf("%s✗ REJECTED%s\n", colorRed+colorBold, colorReset)
		if len(review.Comments) > 0 {
			fmt.Println("\nIssues to fix:")
			for _, c := range review.Comments {
				fmt.Printf("  %s•%s %s\n", colorRed, colorReset, c)
			}
		}
		fmt.Printf("\nTask #%d moved back to backlog.\n", task.ID)
		fmt.Printf("Fix and re-run: %shive run %d && hive review %d%s\n", colorCyan, task.ID, task.ID, colorReset)

	default:
		s.AddEvent(task.ID, agentName, "reviewed", "No clear verdict")
		fmt.Println("Reviewer didn't return a clear verdict.")
		fmt.Println("\nRaw output:")
		fmt.Println(resp.Output)
	}

	return nil
}
