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
	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run [task-id]",
	Short: "Run an agent on a task",
	Long: `Picks up a task from the board and runs the assigned agent on it.
The agent receives the full task context as a prompt.

If no task ID is given, picks the highest-priority backlog task
that has an assigned agent.`,
	RunE: runRun,
}

var (
	runAgent string // override agent name
	runDry   bool   // dry-run: show prompt without executing
)

func init() {
	runCmd.Flags().StringVarP(&runAgent, "agent", "a", "", "Override which agent to use")
	runCmd.Flags().BoolVar(&runDry, "dry", false, "Show the prompt that would be sent without executing")

	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	// Load config.
	cfg, err := config.Load(hivePath("config.yaml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Find the task to run.
	var task *store.Task

	if len(args) > 0 {
		// Explicit task ID.
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid task ID: %s", args[0])
		}
		task, err = s.GetTask(id)
		if err != nil {
			return fmt.Errorf("task #%d not found", id)
		}
	} else {
		// Find highest-priority assigned backlog task.
		task, err = findNextTask(s)
		if err != nil {
			return err
		}
	}

	// Determine which agent to use.
	agentName := task.AssignedAgent
	if runAgent != "" {
		agentName = runAgent
	}
	if agentName == "" {
		return fmt.Errorf("task #%d has no assigned agent. Use: hive task assign %d <agent>", task.ID, task.ID)
	}

	agentCfg, ok := cfg.Agents[agentName]
	if !ok {
		return fmt.Errorf("agent %q not found in config. Available: %s", agentName, availableAgents(cfg))
	}

	// Determine role.
	role := task.Role
	if role == "" {
		role = agentCfg.Role
	}

	// Build context/prompt.
	ctxBuilder := agentctx.New(s)
	prompt, err := ctxBuilder.BuildPrompt(task, role)
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	// Dry-run mode: just show the prompt.
	if runDry {
		fmt.Printf("=== DRY RUN: Task #%d -> Agent: %s (role: %s) ===\n\n", task.ID, agentName, role)
		fmt.Println(prompt)
		fmt.Printf("\n=== END PROMPT (%d chars) ===\n", len(prompt))
		return nil
	}

	// Create the runner.
	runner, err := agent.NewRunner(agentName, agentCfg)
	if err != nil {
		return fmt.Errorf("create agent runner: %w", err)
	}

	// Update task status to in_progress.
	if err := s.UpdateTaskStatus(task.ID, store.StatusInProgress); err != nil {
		return fmt.Errorf("update task status: %w", err)
	}

	// Get working directory.
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	fmt.Printf("Running task #%d: %s\n", task.ID, task.Title)
	fmt.Printf("  Agent: %s (%s mode)\n", agentName, agentCfg.Mode)
	fmt.Printf("  Role:  %s\n", role)
	fmt.Printf("  Timeout: %ds\n\n", agentCfg.DefaultTimeout())

	// Execute.
	req := agent.Request{
		TaskID:     task.ID,
		Prompt:     prompt,
		WorkDir:    workDir,
		TimeoutSec: agentCfg.DefaultTimeout(),
	}

	resp, err := runner.Run(context.Background(), req)
	if err != nil {
		// Agent failed hard (couldn't even start).
		s.UpdateTaskStatus(task.ID, store.StatusFailed)
		return fmt.Errorf("agent execution failed: %w", err)
	}

	// Log the output as an event.
	outputPreview := resp.Output
	if len(outputPreview) > 500 {
		outputPreview = outputPreview[:500] + "... (truncated)"
	}
	s.AddEvent(task.ID, agentName, "agent_output", outputPreview)

	// Save full output as artifact.
	artifactPath := hivePath("runs", fmt.Sprintf("task-%d-%s-output.md", task.ID, agentName))
	os.MkdirAll(hivePath("runs"), 0755)
	if err := os.WriteFile(artifactPath, []byte(resp.Output), 0644); err != nil {
		fmt.Printf("Warning: could not save artifact: %v\n", err)
	} else {
		s.AddArtifact(task.ID, "output", artifactPath)
	}

	// Display result.
	fmt.Printf("--- Agent Output (%.1fs, exit code: %d) ---\n\n", resp.Duration, resp.ExitCode)
	fmt.Println(resp.Output)
	fmt.Println()

	// Check for BLOCKED pattern in output.
	if blocked := extractBlocked(resp.Output); blocked != "" {
		s.BlockTask(task.ID, blocked)
		fmt.Printf("Agent requested blocker: %s\n", blocked)
		fmt.Printf("Answer with: hive answer %d \"your answer\"\n", task.ID)
		return nil
	}

	// Update status based on exit code.
	if resp.ExitCode != 0 {
		s.UpdateTaskStatus(task.ID, store.StatusFailed)
		fmt.Printf("Task #%d failed (exit code %d)\n", task.ID, resp.ExitCode)
		if resp.Error != nil {
			fmt.Printf("Error: %v\n", resp.Error)
		}
	} else {
		// If this is a reviewer, check verdict.
		if role == "reviewer" {
			verdict := extractVerdict(resp.Output)
			switch verdict {
			case "REJECT":
				s.AddReview(task.ID, agentName, "reject", resp.Output)
				s.UpdateTaskStatus(task.ID, store.StatusBacklog)
				fmt.Printf("Review: REJECTED. Task moved back to backlog for fixes.\n")
			case "APPROVE":
				s.AddReview(task.ID, agentName, "approve", resp.Output)
				s.UpdateTaskStatus(task.ID, store.StatusDone)
				fmt.Printf("Review: APPROVED. Task done.\n")
			default:
				s.UpdateTaskStatus(task.ID, store.StatusReview)
				fmt.Printf("Review complete. Check output for verdict.\n")
			}
		} else {
			s.UpdateTaskStatus(task.ID, store.StatusDone)
			fmt.Printf("Task #%d completed successfully.\n", task.ID)
		}
	}

	return nil
}

// findNextTask returns the highest-priority backlog task with an assigned agent.
func findNextTask(s *store.Store) (*store.Task, error) {
	tasks, err := s.ListTasks("backlog")
	if err != nil {
		return nil, err
	}

	priorityOrder := map[string]int{"high": 0, "medium": 1, "low": 2}

	var best *store.Task
	bestPri := 999

	for i := range tasks {
		t := &tasks[i]
		if t.AssignedAgent == "" {
			continue
		}
		pri, ok := priorityOrder[t.Priority]
		if !ok {
			pri = 1
		}
		if pri < bestPri {
			best = t
			bestPri = pri
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no assigned backlog tasks found. Create and assign a task first")
	}
	return best, nil
}

// extractBlocked looks for a "BLOCKED:" pattern in agent output.
func extractBlocked(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "BLOCKED:") {
			return strings.TrimSpace(line[8:])
		}
	}
	return ""
}

// extractVerdict looks for a "VERDICT:" pattern in agent output.
func extractVerdict(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "VERDICT:") {
			verdict := strings.TrimSpace(strings.ToUpper(line[8:]))
			if strings.Contains(verdict, "APPROVE") {
				return "APPROVE"
			}
			if strings.Contains(verdict, "REJECT") {
				return "REJECT"
			}
		}
	}
	return ""
}

func availableAgents(cfg *config.Config) string {
	names := make([]string, 0, len(cfg.Agents))
	for name := range cfg.Agents {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
