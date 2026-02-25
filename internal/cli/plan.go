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

var planCmd = &cobra.Command{
	Use:   "plan [epic-id]",
	Short: "Break an epic into tasks using a PM agent",
	Long: `Runs the PM-role agent on an epic (or task) to break it into subtasks.
Tasks are automatically created on the board under the epic.

If the target is an epic and it has a safety branch, hive switches
to that branch before running the PM agent.

The PM agent is the first agent with role "pm" in your config.
Override with --agent flag.`,
	Args: cobra.ExactArgs(1),
	RunE: runPlan,
}

var planAgent string

func init() {
	planCmd.Flags().StringVarP(&planAgent, "agent", "a", "", "Override PM agent name")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	cfg, err := config.Load(hivePath("config.yaml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Get the epic/task.
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid ID: %s", args[0])
	}
	task, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("#%d not found", id)
	}

	workDir, _ := os.Getwd()

	// If this is an epic with a safety branch, ensure we're on it.
	if task.Kind == store.KindEpic && task.GitBranch != "" {
		safety := git.New(workDir)
		if safety.IsGitRepo() {
			current, _ := safety.CurrentBranch()
			if current != task.GitBranch {
				// Create/switch to the safety branch.
				if err := safety.CreateBranch(task.GitBranch); err != nil {
					fmt.Printf("%s⚠  Could not switch to safety branch %s: %v%s\n",
						colorYellow, task.GitBranch, err, colorReset)
				} else {
					fmt.Printf("  Switched to safety branch %s%s%s\n\n", colorCyan, task.GitBranch, colorReset)
				}
			}
		}
	} else if task.Kind == store.KindEpic && task.GitBranch == "" {
		// Epic without a branch — create one now.
		safety := git.New(workDir)
		if safety.IsGitRepo() && !safety.HasUncommittedChanges() {
			branch := git.BranchName(task.ID)
			if err := safety.CreateBranch(branch); err == nil {
				s.SetGitBranch(task.ID, branch)
				task.GitBranch = branch
				fmt.Printf("  Created safety branch %s%s%s\n\n", colorCyan, branch, colorReset)
			}
		}
	}

	// Find PM agent.
	agentName := planAgent
	if agentName == "" {
		pmAgents := cfg.AgentsByRole("pm")
		for name := range pmAgents {
			agentName = name
			break
		}
	}
	if agentName == "" {
		return fmt.Errorf("no PM agent configured. Add an agent with role: pm in .hive/config.yaml")
	}

	agentCfg, ok := cfg.Agents[agentName]
	if !ok {
		return fmt.Errorf("agent %q not found in config", agentName)
	}

	// Build prompt.
	ctxBuilder := agentctx.New(s)
	prompt, err := ctxBuilder.BuildPrompt(task, "pm")
	if err != nil {
		return fmt.Errorf("build context: %w", err)
	}

	// Create runner.
	runner, err := agent.NewRunner(agentName, agentCfg)
	if err != nil {
		return fmt.Errorf("create agent: %w", err)
	}

	label := "task"
	if task.Kind == store.KindEpic {
		label = "epic"
	}
	fmt.Printf("Planning %s #%d: %s\n", label, task.ID, task.Title)
	fmt.Printf("  PM Agent: %s\n\n", agentName)

	// Run PM agent.
	resp, err := runner.Run(context.Background(), agent.Request{
		TaskID:     task.ID,
		Prompt:     prompt,
		WorkDir:    workDir,
		TimeoutSec: agentCfg.DefaultTimeout(),
	})
	if err != nil {
		return fmt.Errorf("PM agent failed: %w", err)
	}

	// Save output as artifact.
	artifactPath := hivePath("runs", fmt.Sprintf("task-%d-plan.md", task.ID))
	os.MkdirAll(hivePath("runs"), 0755)
	os.WriteFile(artifactPath, []byte(resp.Output), 0644)
	s.AddArtifact(task.ID, "plan", artifactPath)

	// Check for blocker.
	if blocked := agent.ParseBlocked(resp.Output); blocked != "" {
		s.BlockTask(task.ID, blocked)
		fmt.Printf("%s⚠  PM needs your input:%s %s\n", colorRed+colorBold, colorReset, blocked)
		fmt.Printf("   → %shive answer %d \"your answer\"%s\n", colorCyan, task.ID, colorReset)
		return nil
	}

	// Parse subtasks from output.
	subtasks := agent.ParseSubtasks(resp.Output)

	if len(subtasks) == 0 {
		fmt.Println("PM agent didn't return structured subtasks.")
		fmt.Println("Raw output:")
		fmt.Println(resp.Output)
		return nil
	}

	// Create subtasks on the board.
	fmt.Printf("%sCreated %d tasks:%s\n\n", colorBold, len(subtasks), colorReset)

	for _, sub := range subtasks {
		parentID := task.ID
		created, err := s.CreateTask(sub.Title, sub.Description, sub.Priority, &parentID)
		if err != nil {
			fmt.Printf("  %s✗%s Failed to create: %s (%v)\n", colorRed, colorReset, sub.Title, err)
			continue
		}
		priColor := priorityColor(sub.Priority)
		fmt.Printf("  %s#%d%s %s%s%s", colorYellow, created.ID, colorReset, priColor, sub.Title, colorReset)
		if sub.Description != "" {
			fmt.Printf(" %s— %s%s", colorDim, sub.Description, colorReset)
		}
		fmt.Printf(" [%s]\n", sub.Priority)
	}

	fmt.Printf("\nNext: %shive auto %d%s to run the full pipeline, or assign agents manually\n", colorCyan, task.ID, colorReset)

	s.AddEvent(task.ID, agentName, "planned", fmt.Sprintf("Created %d tasks", len(subtasks)))

	return nil
}
