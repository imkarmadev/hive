package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
	"github.com/spf13/cobra"
)

var (
	epicPriority    string
	epicDescription string
)

var epicCmd = &cobra.Command{
	Use:   "epic",
	Short: "Create and manage epics",
	Long: `Epics are high-level work items that you create as the PM.
When you run 'hive plan' on an epic, the PM agent breaks it into tasks.
All agent work on an epic happens on a git safety branch.`,
}

var epicCreateCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new epic",
	Long: `Creates a new epic on the board and (if in a git repo) creates
a safety branch for all work related to this epic.

Example:
  hive epic create "Add JWT authentication" -p high -d "With refresh tokens"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runEpicCreate,
}

var epicListCmd = &cobra.Command{
	Use:   "list [status]",
	Short: "List all epics",
	RunE:  runEpicList,
}

var epicShowCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show epic details and its tasks",
	Args:  cobra.ExactArgs(1),
	RunE:  runEpicShow,
}

var epicAcceptCmd = &cobra.Command{
	Use:   "accept [id]",
	Short: "Accept an epic — merge its safety branch into the base branch",
	Long: `Reviews the total diff of all agent work on this epic,
then merges the safety branch into your main branch.

All tasks under the epic must be done before accepting.`,
	Args: cobra.ExactArgs(1),
	RunE: runEpicAccept,
}

var epicRejectCmd = &cobra.Command{
	Use:   "reject [id]",
	Short: "Reject an epic — discard all agent work on this epic",
	Long: `Switches back to the base branch and deletes the epic's
safety branch, discarding all agent changes.`,
	Args: cobra.ExactArgs(1),
	RunE: runEpicReject,
}

var epicDiffCmd = &cobra.Command{
	Use:   "diff [id]",
	Short: "Show the total diff for an epic",
	Long:  `Shows all changes made by agents on this epic's safety branch.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runEpicDiff,
}

func init() {
	epicCreateCmd.Flags().StringVarP(&epicPriority, "priority", "p", "medium", "Priority: high, medium, low")
	epicCreateCmd.Flags().StringVarP(&epicDescription, "desc", "d", "", "Epic description / acceptance criteria")

	epicCmd.AddCommand(epicCreateCmd)
	epicCmd.AddCommand(epicListCmd)
	epicCmd.AddCommand(epicShowCmd)
	epicCmd.AddCommand(epicAcceptCmd)
	epicCmd.AddCommand(epicRejectCmd)
	epicCmd.AddCommand(epicDiffCmd)

	rootCmd.AddCommand(epicCmd)
}

func runEpicCreate(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	title := strings.Join(args, " ")

	epic, err := s.CreateEpic(title, epicDescription, epicPriority)
	if err != nil {
		return err
	}

	fmt.Printf("Created epic %s#%d%s: %s [%s]\n", colorYellow, epic.ID, colorReset, epic.Title, epic.Priority)

	// Create git safety branch if in a git repo.
	workDir, _ := os.Getwd()
	safety := git.New(workDir)

	if safety.IsGitRepo() {
		branch := git.BranchName(epic.ID)

		if safety.HasUncommittedChanges() {
			fmt.Printf("\n%s⚠  Uncommitted changes detected.%s\n", colorYellow, colorReset)
			fmt.Printf("  Commit or stash them before starting work on this epic.\n")
			fmt.Printf("  Safety branch %s%s%s will be created when you run %shive plan %d%s\n",
				colorCyan, branch, colorReset, colorCyan, epic.ID, colorReset)
		} else {
			if err := safety.CreateBranch(branch); err != nil {
				fmt.Printf("\n%s⚠  Could not create safety branch: %v%s\n", colorYellow, err, colorReset)
			} else {
				s.SetGitBranch(epic.ID, branch)
				fmt.Printf("  Branch: %s%s%s (safety net — all agent work happens here)\n", colorCyan, branch, colorReset)
			}
		}
	}

	fmt.Printf("\nNext: %shive plan %d%s to break it into tasks\n", colorCyan, epic.ID, colorReset)
	return nil
}

func runEpicList(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	status := ""
	if len(args) > 0 {
		status = args[0]
	}

	epics, err := s.ListEpics(status)
	if err != nil {
		return err
	}

	if len(epics) == 0 {
		fmt.Println("No epics found. Create one: hive epic create \"description\"")
		return nil
	}

	for _, e := range epics {
		statusColor := statusToColor(e.Status)
		priColor := priorityColor(e.Priority)

		// Count tasks under this epic.
		tasks, _ := s.ListTasksByEpic(e.ID)
		done := 0
		total := len(tasks)
		for _, t := range tasks {
			if t.Status == store.StatusDone {
				done++
			}
		}

		progress := ""
		if total > 0 {
			progress = fmt.Sprintf(" [%d/%d tasks]", done, total)
		}

		branch := ""
		if e.GitBranch != "" {
			branch = fmt.Sprintf(" %s(%s)%s", colorDim, e.GitBranch, colorReset)
		}

		fmt.Printf("%s#%-4d%s %s%-12s%s %s%-6s%s %s%s%s\n",
			colorYellow, e.ID, colorReset,
			statusColor, e.Status, colorReset,
			priColor, e.Priority, colorReset,
			e.Title, progress, branch)
	}
	return nil
}

func runEpicShow(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid epic ID: %s", args[0])
	}

	epic, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("epic #%d not found", id)
	}
	if epic.Kind != store.KindEpic {
		return fmt.Errorf("#%d is a task, not an epic. Use 'hive task show %d'", id, id)
	}

	fmt.Printf("%sEpic #%d%s\n", colorBold, epic.ID, colorReset)
	fmt.Printf("  Title:    %s\n", epic.Title)
	fmt.Printf("  Status:   %s\n", epic.Status)
	fmt.Printf("  Priority: %s\n", epic.Priority)
	if epic.Description != "" {
		fmt.Printf("  Desc:     %s\n", epic.Description)
	}
	if epic.GitBranch != "" {
		fmt.Printf("  Branch:   %s%s%s\n", colorCyan, epic.GitBranch, colorReset)
	}
	fmt.Printf("  Created:  %s\n", epic.CreatedAt.Format("2006-01-02 15:04"))

	// Show tasks under this epic.
	tasks, _ := s.ListTasksByEpic(epic.ID)
	if len(tasks) > 0 {
		fmt.Printf("\n  %sTasks (%d):%s\n", colorBold, len(tasks), colorReset)
		for _, t := range tasks {
			statusColor := statusToColor(t.Status)
			agent := ""
			if t.AssignedAgent != "" {
				agent = fmt.Sprintf(" %s[%s]%s", colorCyan, t.AssignedAgent, colorReset)
			}
			blocked := ""
			if t.Status == store.StatusBlocked {
				blocked = fmt.Sprintf(" %s⚠ %s%s", colorRed, t.BlockedReason, colorReset)
			}
			fmt.Printf("    %s#%-4d%s %s%-12s%s %s%s%s\n",
				colorYellow, t.ID, colorReset,
				statusColor, t.Status, colorReset,
				t.Title, agent, blocked)
		}
	} else {
		fmt.Printf("\n  No tasks yet. Run: %shive plan %d%s\n", colorCyan, epic.ID, colorReset)
	}

	// Show git diff stat if available.
	if epic.GitBranch != "" {
		workDir, _ := os.Getwd()
		safety := git.New(workDir)
		baseBranch, err := safety.BaseBranch()
		if err == nil {
			stat, err := safety.DiffStat(baseBranch, epic.GitBranch)
			if err == nil && stat != "" {
				fmt.Printf("\n  %sChanges:%s\n", colorBold, colorReset)
				for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
					fmt.Printf("    %s\n", line)
				}
			}
		}
	}

	return nil
}

func runEpicAccept(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid epic ID: %s", args[0])
	}

	epic, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("epic #%d not found", id)
	}
	if epic.Kind != store.KindEpic {
		return fmt.Errorf("#%d is a task, not an epic", id)
	}
	if epic.GitBranch == "" {
		return fmt.Errorf("epic #%d has no safety branch", id)
	}

	workDir, _ := os.Getwd()
	safety := git.New(workDir)

	baseBranch, err := safety.BaseBranch()
	if err != nil {
		return fmt.Errorf("detect base branch: %w", err)
	}

	// Show what will be merged.
	stat, err := safety.DiffStat(baseBranch, epic.GitBranch)
	if err != nil {
		return fmt.Errorf("diff stat: %w", err)
	}

	if stat == "" {
		fmt.Println("No changes to merge.")
		return nil
	}

	commits, _ := safety.LogCommits(baseBranch, epic.GitBranch)

	fmt.Printf("%s═══ Accept Epic #%d: %s ═══%s\n\n", colorBold, epic.ID, epic.Title, colorReset)
	fmt.Printf("  Branch: %s%s%s → %s%s%s\n\n", colorCyan, epic.GitBranch, colorReset, colorGreen, baseBranch, colorReset)

	if commits != "" {
		fmt.Printf("  %sCommits:%s\n", colorBold, colorReset)
		for _, line := range strings.Split(commits, "\n") {
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()
	}

	fmt.Printf("  %sChanges:%s\n", colorBold, colorReset)
	for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
		fmt.Printf("    %s\n", line)
	}
	fmt.Println()

	// Commit any uncommitted work on the epic branch first.
	if safety.HasUncommittedChanges() {
		committed, err := safety.CommitAll(fmt.Sprintf("hive: final changes for epic #%d", epic.ID))
		if err != nil {
			return fmt.Errorf("commit pending changes: %w", err)
		}
		if committed {
			fmt.Printf("  Committed pending changes.\n")
		}
	}

	// Merge.
	if err := safety.MergeBranch(baseBranch, epic.GitBranch); err != nil {
		return fmt.Errorf("merge failed: %w", err)
	}

	// Clean up branch.
	safety.DeleteBranch(epic.GitBranch, false)

	// Mark epic as done.
	s.UpdateTaskStatus(epic.ID, store.StatusDone)
	s.AddEvent(epic.ID, "user", "accepted", fmt.Sprintf("Merged %s into %s", epic.GitBranch, baseBranch))

	fmt.Printf("  %s✓ Merged into %s%s\n", colorGreen+colorBold, baseBranch, colorReset)
	fmt.Printf("  %s✓ Epic #%d done%s\n", colorGreen+colorBold, epic.ID, colorReset)

	return nil
}

func runEpicReject(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid epic ID: %s", args[0])
	}

	epic, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("epic #%d not found", id)
	}
	if epic.Kind != store.KindEpic {
		return fmt.Errorf("#%d is a task, not an epic", id)
	}
	if epic.GitBranch == "" {
		return fmt.Errorf("epic #%d has no safety branch", id)
	}

	workDir, _ := os.Getwd()
	safety := git.New(workDir)

	baseBranch, err := safety.BaseBranch()
	if err != nil {
		return fmt.Errorf("detect base branch: %w", err)
	}

	// Show what will be discarded.
	stat, _ := safety.DiffStat(baseBranch, epic.GitBranch)
	if stat != "" {
		fmt.Printf("%s═══ Reject Epic #%d: %s ═══%s\n\n", colorBold, epic.ID, epic.Title, colorReset)
		fmt.Printf("  %sDiscarding changes:%s\n", colorRed, colorReset)
		for _, line := range strings.Split(strings.TrimSpace(stat), "\n") {
			fmt.Printf("    %s\n", line)
		}
		fmt.Println()
	}

	if err := safety.RejectBranch(baseBranch, epic.GitBranch); err != nil {
		return fmt.Errorf("reject failed: %w", err)
	}

	s.UpdateTaskStatus(epic.ID, store.StatusFailed)
	s.AddEvent(epic.ID, "user", "rejected", fmt.Sprintf("Discarded branch %s", epic.GitBranch))

	// Mark all tasks as failed too.
	tasks, _ := s.ListTasksByEpic(epic.ID)
	for _, t := range tasks {
		if t.Status != store.StatusDone && t.Status != store.StatusFailed {
			s.UpdateTaskStatus(t.ID, store.StatusFailed)
		}
	}

	fmt.Printf("  %s✗ Discarded all changes%s\n", colorRed+colorBold, colorReset)
	fmt.Printf("  Back on %s%s%s\n", colorCyan, baseBranch, colorReset)

	return nil
}

func runEpicDiff(cmd *cobra.Command, args []string) error {
	s, err := mustStore()
	if err != nil {
		return err
	}
	defer s.Close()

	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid epic ID: %s", args[0])
	}

	epic, err := s.GetTask(id)
	if err != nil {
		return fmt.Errorf("epic #%d not found", id)
	}
	if epic.GitBranch == "" {
		return fmt.Errorf("epic #%d has no safety branch", id)
	}

	workDir, _ := os.Getwd()
	safety := git.New(workDir)

	baseBranch, err := safety.BaseBranch()
	if err != nil {
		return fmt.Errorf("detect base branch: %w", err)
	}

	// First show stat summary.
	stat, _ := safety.DiffStat(baseBranch, epic.GitBranch)
	if stat != "" {
		fmt.Printf("%s═══ Epic #%d: %s ═══%s\n\n", colorBold, epic.ID, epic.Title, colorReset)
		fmt.Printf("%s%s%s\n", colorDim, strings.TrimSpace(stat), colorReset)
		fmt.Println()
	}

	// Then show full diff.
	diff, err := safety.Diff(baseBranch, epic.GitBranch)
	if err != nil {
		return fmt.Errorf("diff: %w", err)
	}

	if diff == "" {
		fmt.Println("No changes.")
		return nil
	}

	fmt.Print(diff)
	return nil
}

// statusToColor returns an ANSI color code for a task status.
func statusToColor(status store.TaskStatus) string {
	switch status {
	case store.StatusBacklog:
		return colorWhite
	case store.StatusInProgress:
		return colorBlue
	case store.StatusBlocked:
		return colorRed
	case store.StatusReview:
		return colorMagenta
	case store.StatusDone:
		return colorGreen
	case store.StatusFailed:
		return colorRed + colorBold
	default:
		return ""
	}
}
