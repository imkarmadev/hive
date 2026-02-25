// Package git provides git-based safety net for hive agent operations.
// Before an agent works, we create a branch. After it finishes, the user
// can accept (merge) or reject (delete branch) the changes.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Safety provides git branch management for safe agent execution.
// Each epic gets its own branch. All task work within that epic
// happens on that branch. The user reviews the total diff and
// accepts or rejects at the epic level.
type Safety struct {
	workDir string
}

// New creates a Safety instance for the given working directory.
func New(workDir string) *Safety {
	return &Safety{workDir: workDir}
}

// IsGitRepo checks if the working directory is a git repository.
func (s *Safety) IsGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// CurrentBranch returns the name of the current git branch.
func (s *Safety) CurrentBranch() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// BaseBranch detects the main/master branch name.
func (s *Safety) BaseBranch() (string, error) {
	// Try common names.
	for _, name := range []string{"main", "master"} {
		cmd := exec.Command("git", "rev-parse", "--verify", name)
		cmd.Dir = s.workDir
		if err := cmd.Run(); err == nil {
			return name, nil
		}
	}
	// Fall back to current branch.
	return s.CurrentBranch()
}

// BranchName generates the safety branch name for an epic.
// Format: hive/epic-{id}
func BranchName(epicID int64) string {
	return fmt.Sprintf("hive/epic-%d", epicID)
}

// BranchExists checks if a branch exists.
func (s *Safety) BranchExists(branch string) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", branch)
	cmd.Dir = s.workDir
	return cmd.Run() == nil
}

// HasUncommittedChanges checks if there are uncommitted changes in the working tree.
func (s *Safety) HasUncommittedChanges() bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// CreateBranch creates a new branch from the current HEAD and switches to it.
// If the branch already exists, it just switches to it.
func (s *Safety) CreateBranch(branch string) error {
	if s.BranchExists(branch) {
		return s.Checkout(branch)
	}

	cmd := exec.Command("git", "checkout", "-b", branch)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("create branch %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

// Checkout switches to an existing branch.
func (s *Safety) Checkout(branch string) error {
	cmd := exec.Command("git", "checkout", branch)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("checkout %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

// CommitAll stages all changes and commits with the given message.
// Returns true if a commit was made, false if there was nothing to commit.
func (s *Safety) CommitAll(message string) (bool, error) {
	// Stage all changes.
	addCmd := exec.Command("git", "add", "-A")
	addCmd.Dir = s.workDir
	if out, err := addCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("git add: %s", strings.TrimSpace(string(out)))
	}

	// Check if there are staged changes.
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = s.workDir
	if err := diffCmd.Run(); err == nil {
		// No staged changes — nothing to commit.
		return false, nil
	}

	// Commit.
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = s.workDir
	out, err := commitCmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git commit: %s", strings.TrimSpace(string(out)))
	}
	return true, nil
}

// Diff returns the diff between the base branch and the given branch.
// This shows all changes the epic introduced.
func (s *Safety) Diff(baseBranch, epicBranch string) (string, error) {
	cmd := exec.Command("git", "diff", baseBranch+"..."+epicBranch)
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

// DiffStat returns a summary of changes (files changed, insertions, deletions).
func (s *Safety) DiffStat(baseBranch, epicBranch string) (string, error) {
	cmd := exec.Command("git", "diff", "--stat", baseBranch+"..."+epicBranch)
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git diff --stat: %w", err)
	}
	return string(out), nil
}

// MergeBranch merges the epic branch into the base branch (fast-forward if possible).
// This is the "accept" action.
func (s *Safety) MergeBranch(baseBranch, epicBranch string) error {
	// Switch to base branch.
	if err := s.Checkout(baseBranch); err != nil {
		return err
	}

	// Merge.
	cmd := exec.Command("git", "merge", epicBranch, "--no-ff",
		"-m", fmt.Sprintf("Merge %s", epicBranch))
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("merge %s: %s", epicBranch, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteBranch deletes a branch. This is part of the "reject" cleanup
// or post-merge cleanup.
func (s *Safety) DeleteBranch(branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	cmd := exec.Command("git", "branch", flag, branch)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("delete branch %s: %s", branch, strings.TrimSpace(string(out)))
	}
	return nil
}

// RejectBranch switches back to the base branch and force-deletes the epic branch.
// This is the "reject" action — discard all agent work.
func (s *Safety) RejectBranch(baseBranch, epicBranch string) error {
	if err := s.Checkout(baseBranch); err != nil {
		return err
	}
	return s.DeleteBranch(epicBranch, true)
}

// LogCommits returns the commit log for the epic branch since it diverged from base.
func (s *Safety) LogCommits(baseBranch, epicBranch string) (string, error) {
	cmd := exec.Command("git", "log", "--oneline", baseBranch+".."+epicBranch)
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git log: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// --- Worktree support for parallel execution ---

// WorktreePath returns the path for a task-specific worktree.
func WorktreePath(baseDir string, taskID int64) string {
	return fmt.Sprintf("%s/.hive/worktrees/task-%d", baseDir, taskID)
}

// AddWorktree creates a git worktree for a task on the given branch.
// Each worktree is an independent working directory sharing the same git repo,
// so multiple CLI agents can work in parallel without file conflicts.
func (s *Safety) AddWorktree(path, branch string) error {
	cmd := exec.Command("git", "worktree", "add", path, branch)
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("add worktree: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveWorktree removes a git worktree.
func (s *Safety) RemoveWorktree(path string) error {
	cmd := exec.Command("git", "worktree", "remove", path, "--force")
	cmd.Dir = s.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("remove worktree: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// MergeWorktreeChanges commits changes from a worktree directory,
// then cherry-picks or merges them into the epic branch in the main workdir.
// This is used after a parallel task completes in its worktree.
func (s *Safety) MergeWorktreeChanges(worktreePath string, taskID int64, taskTitle string) error {
	wt := New(worktreePath)

	// Commit all changes in the worktree.
	msg := fmt.Sprintf("hive: task #%d — %s", taskID, taskTitle)
	committed, err := wt.CommitAll(msg)
	if err != nil {
		return fmt.Errorf("commit in worktree: %w", err)
	}
	if !committed {
		return nil // Nothing to merge.
	}

	// Get the commit hash.
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = worktreePath
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("get commit hash: %w", err)
	}
	commitHash := strings.TrimSpace(string(out))

	// Cherry-pick the commit into the main workdir (which is on the epic branch).
	cpCmd := exec.Command("git", "cherry-pick", commitHash)
	cpCmd.Dir = s.workDir
	cpOut, err := cpCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cherry-pick: %s", strings.TrimSpace(string(cpOut)))
	}

	return nil
}

// ListWorktrees returns all active worktrees.
func (s *Safety) ListWorktrees() ([]string, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = s.workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}

	var paths []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths, nil
}

// PruneWorktrees removes stale worktree references.
func (s *Safety) PruneWorktrees() error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = s.workDir
	_, err := cmd.CombinedOutput()
	return err
}
