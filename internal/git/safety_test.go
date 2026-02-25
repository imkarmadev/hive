package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initTestRepo creates a temporary git repo with an initial commit.
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s failed: %s\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	// Create initial commit.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test\n"), 0644)
	run("add", ".")
	run("commit", "-m", "initial commit")

	return dir
}

func TestIsGitRepo(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	if !s.IsGitRepo() {
		t.Fatal("expected IsGitRepo to return true")
	}

	// Non-git directory.
	tmpDir := t.TempDir()
	s2 := New(tmpDir)
	if s2.IsGitRepo() {
		t.Fatal("expected IsGitRepo to return false for non-git dir")
	}
}

func TestCurrentBranch(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	branch, err := s.CurrentBranch()
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("expected 'main', got %q", branch)
	}
}

func TestBaseBranch(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	base, err := s.BaseBranch()
	if err != nil {
		t.Fatalf("BaseBranch: %v", err)
	}
	if base != "main" {
		t.Fatalf("expected 'main', got %q", base)
	}
}

func TestBranchName(t *testing.T) {
	got := BranchName(42)
	if got != "hive/epic-42" {
		t.Fatalf("expected 'hive/epic-42', got %q", got)
	}
}

func TestCreateBranch_AndCheckout(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	err := s.CreateBranch("hive/epic-1")
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}

	branch, _ := s.CurrentBranch()
	if branch != "hive/epic-1" {
		t.Fatalf("expected 'hive/epic-1', got %q", branch)
	}

	// Switch back to main.
	if err := s.Checkout("main"); err != nil {
		t.Fatalf("Checkout main: %v", err)
	}
	branch, _ = s.CurrentBranch()
	if branch != "main" {
		t.Fatalf("expected 'main' after checkout, got %q", branch)
	}

	// CreateBranch on existing branch should just switch to it.
	if err := s.CreateBranch("hive/epic-1"); err != nil {
		t.Fatalf("CreateBranch existing: %v", err)
	}
	branch, _ = s.CurrentBranch()
	if branch != "hive/epic-1" {
		t.Fatalf("expected 'hive/epic-1' after re-create, got %q", branch)
	}
}

func TestBranchExists(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	if !s.BranchExists("main") {
		t.Fatal("expected 'main' to exist")
	}
	if s.BranchExists("nonexistent") {
		t.Fatal("expected 'nonexistent' to not exist")
	}
}

func TestHasUncommittedChanges(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Clean repo — no changes.
	if s.HasUncommittedChanges() {
		t.Fatal("expected no uncommitted changes in fresh repo")
	}

	// Create a file — now there are changes.
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("content"), 0644)
	if !s.HasUncommittedChanges() {
		t.Fatal("expected uncommitted changes after creating a file")
	}
}

func TestCommitAll(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Nothing to commit.
	committed, err := s.CommitAll("empty commit")
	if err != nil {
		t.Fatalf("CommitAll empty: %v", err)
	}
	if committed {
		t.Fatal("expected no commit when nothing changed")
	}

	// Create a file and commit.
	os.WriteFile(filepath.Join(dir, "code.go"), []byte("package main\n"), 0644)

	committed, err = s.CommitAll("add code.go")
	if err != nil {
		t.Fatalf("CommitAll: %v", err)
	}
	if !committed {
		t.Fatal("expected a commit to be made")
	}

	// Verify no uncommitted changes.
	if s.HasUncommittedChanges() {
		t.Fatal("expected clean state after commit")
	}
}

func TestDiff_And_DiffStat(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create branch and make changes.
	s.CreateBranch("hive/epic-1")
	os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package feature\n"), 0644)
	s.CommitAll("add feature")

	// Diff from main.
	diff, err := s.Diff("main", "hive/epic-1")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "feature.go") {
		t.Fatalf("expected diff to contain 'feature.go', got: %s", diff)
	}

	// DiffStat.
	stat, err := s.DiffStat("main", "hive/epic-1")
	if err != nil {
		t.Fatalf("DiffStat: %v", err)
	}
	if !strings.Contains(stat, "feature.go") {
		t.Fatalf("expected stat to contain 'feature.go', got: %s", stat)
	}
}

func TestMergeBranch(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create branch and make changes.
	s.CreateBranch("hive/epic-1")
	os.WriteFile(filepath.Join(dir, "feature.go"), []byte("package feature\n"), 0644)
	s.CommitAll("add feature")

	// Merge back to main.
	err := s.MergeBranch("main", "hive/epic-1")
	if err != nil {
		t.Fatalf("MergeBranch: %v", err)
	}

	// Verify we're on main.
	branch, _ := s.CurrentBranch()
	if branch != "main" {
		t.Fatalf("expected 'main' after merge, got %q", branch)
	}

	// Verify the file exists on main.
	if _, err := os.Stat(filepath.Join(dir, "feature.go")); os.IsNotExist(err) {
		t.Fatal("expected feature.go to exist on main after merge")
	}
}

func TestDeleteBranch(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	s.CreateBranch("hive/epic-1")
	s.Checkout("main")

	err := s.DeleteBranch("hive/epic-1", false)
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}

	if s.BranchExists("hive/epic-1") {
		t.Fatal("expected branch to be deleted")
	}
}

func TestRejectBranch(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create branch, make changes, then reject.
	s.CreateBranch("hive/epic-1")
	os.WriteFile(filepath.Join(dir, "bad-code.go"), []byte("package bad\n"), 0644)
	s.CommitAll("bad changes")

	err := s.RejectBranch("main", "hive/epic-1")
	if err != nil {
		t.Fatalf("RejectBranch: %v", err)
	}

	// Verify we're on main.
	branch, _ := s.CurrentBranch()
	if branch != "main" {
		t.Fatalf("expected 'main' after reject, got %q", branch)
	}

	// Verify the bad file doesn't exist on main.
	if _, err := os.Stat(filepath.Join(dir, "bad-code.go")); !os.IsNotExist(err) {
		t.Fatal("expected bad-code.go to NOT exist on main after reject")
	}

	// Branch should be deleted.
	if s.BranchExists("hive/epic-1") {
		t.Fatal("expected branch to be deleted after reject")
	}
}

func TestLogCommits(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	s.CreateBranch("hive/epic-1")
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("a\n"), 0644)
	s.CommitAll("first task")
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("b\n"), 0644)
	s.CommitAll("second task")

	log, err := s.LogCommits("main", "hive/epic-1")
	if err != nil {
		t.Fatalf("LogCommits: %v", err)
	}
	lines := strings.Split(log, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 commits, got %d: %s", len(lines), log)
	}
}

func TestWorktreePath(t *testing.T) {
	got := WorktreePath("/home/user/project", 42)
	expected := "/home/user/project/.hive/worktrees/task-42"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestAddAndRemoveWorktree(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create a branch first.
	s.CreateBranch("hive/epic-1")
	s.Checkout("main")

	// Add worktree.
	wtPath := filepath.Join(dir, "worktree-task-1")
	err := s.AddWorktree(wtPath, "hive/epic-1")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// Verify worktree exists and has files.
	if _, err := os.Stat(filepath.Join(wtPath, "README.md")); os.IsNotExist(err) {
		t.Fatal("expected README.md in worktree")
	}

	// Verify it shows up in ListWorktrees.
	worktrees, err := s.ListWorktrees()
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	// On macOS, /tmp is a symlink to /private/tmp, so paths may differ.
	// Check that at least one worktree path ends with our relative suffix.
	found := false
	suffix := "worktree-task-1"
	for _, wt := range worktrees {
		if strings.HasSuffix(wt, suffix) {
			found = true
		}
	}
	if !found {
		t.Fatalf("worktree ending with %q not found in list: %v", suffix, worktrees)
	}

	// Remove worktree.
	err = s.RemoveWorktree(wtPath)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
}

func TestWorktreeParallelWork(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create epic branch.
	s.CreateBranch("hive/epic-1")
	os.WriteFile(filepath.Join(dir, "base.go"), []byte("package base\n"), 0644)
	s.CommitAll("base file")
	s.Checkout("main")

	// Create two worktrees simulating parallel agents.
	wt1Path := filepath.Join(dir, "wt-task-1")
	wt2Path := filepath.Join(dir, "wt-task-2")

	// Create a second branch for the second worktree (can't have two worktrees on same branch).
	s.CreateBranch("hive/task-1-work")
	s.Checkout("main")
	s.CreateBranch("hive/task-2-work")
	s.Checkout("main")

	err := s.AddWorktree(wt1Path, "hive/task-1-work")
	if err != nil {
		t.Fatalf("AddWorktree 1: %v", err)
	}
	defer func() {
		s.RemoveWorktree(wt1Path)
		os.RemoveAll(wt1Path)
	}()

	err = s.AddWorktree(wt2Path, "hive/task-2-work")
	if err != nil {
		t.Fatalf("AddWorktree 2: %v", err)
	}
	defer func() {
		s.RemoveWorktree(wt2Path)
		os.RemoveAll(wt2Path)
	}()

	// Simulate work in each worktree independently.
	os.WriteFile(filepath.Join(wt1Path, "task1.go"), []byte("package task1\n"), 0644)
	wt1 := New(wt1Path)
	committed1, err := wt1.CommitAll("task 1 work")
	if err != nil {
		t.Fatalf("commit in wt1: %v", err)
	}
	if !committed1 {
		t.Fatal("expected commit in wt1")
	}

	os.WriteFile(filepath.Join(wt2Path, "task2.go"), []byte("package task2\n"), 0644)
	wt2 := New(wt2Path)
	committed2, err := wt2.CommitAll("task 2 work")
	if err != nil {
		t.Fatalf("commit in wt2: %v", err)
	}
	if !committed2 {
		t.Fatal("expected commit in wt2")
	}

	// Verify files exist in respective worktrees but not in each other.
	if _, err := os.Stat(filepath.Join(wt1Path, "task1.go")); os.IsNotExist(err) {
		t.Fatal("task1.go should exist in wt1")
	}
	if _, err := os.Stat(filepath.Join(wt2Path, "task2.go")); os.IsNotExist(err) {
		t.Fatal("task2.go should exist in wt2")
	}
	if _, err := os.Stat(filepath.Join(wt1Path, "task2.go")); !os.IsNotExist(err) {
		t.Fatal("task2.go should NOT exist in wt1")
	}
}

func TestMergeWorktreeChanges(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Create epic branch and switch to it.
	s.CreateBranch("hive/epic-1")

	// Create a task branch for the worktree.
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("branch", "hive/task-work")

	// Create worktree on the task branch.
	wtPath := filepath.Join(dir, "wt-merge-test")
	err := s.AddWorktree(wtPath, "hive/task-work")
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	defer func() {
		s.RemoveWorktree(wtPath)
		os.RemoveAll(wtPath)
	}()

	// Do work in worktree.
	os.WriteFile(filepath.Join(wtPath, "feature.go"), []byte("package feature\n"), 0644)

	// Merge worktree changes into epic branch.
	err = s.MergeWorktreeChanges(wtPath, 1, "add feature")
	if err != nil {
		t.Fatalf("MergeWorktreeChanges: %v", err)
	}

	// Verify the file exists on the epic branch.
	if _, err := os.Stat(filepath.Join(dir, "feature.go")); os.IsNotExist(err) {
		t.Fatal("feature.go should exist on epic branch after merge")
	}
}

func TestFullWorkflow_CreateWorkAcceptReject(t *testing.T) {
	dir := initTestRepo(t)
	s := New(dir)

	// Simulate epic workflow:
	// 1. Create safety branch
	branch := BranchName(1)
	if err := s.CreateBranch(branch); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	// 2. Agent does work (task 1)
	os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package auth\n"), 0644)
	committed, _ := s.CommitAll("hive: task #1 — auth module")
	if !committed {
		t.Fatal("expected commit for task 1")
	}

	// 3. Agent does more work (task 2)
	os.WriteFile(filepath.Join(dir, "auth_test.go"), []byte("package auth\n"), 0644)
	committed, _ = s.CommitAll("hive: task #2 — auth tests")
	if !committed {
		t.Fatal("expected commit for task 2")
	}

	// 4. User reviews diff
	diff, _ := s.Diff("main", branch)
	if !strings.Contains(diff, "auth.go") || !strings.Contains(diff, "auth_test.go") {
		t.Fatalf("diff should contain both files, got: %s", diff)
	}

	// 5. User accepts — merge into main
	if err := s.MergeBranch("main", branch); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// Verify files exist on main.
	for _, f := range []string{"auth.go", "auth_test.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); os.IsNotExist(err) {
			t.Fatalf("expected %s on main after accept", f)
		}
	}

	// 6. Now test reject flow with a new epic
	branch2 := BranchName(2)
	s.CreateBranch(branch2)
	os.WriteFile(filepath.Join(dir, "bad.go"), []byte("bad\n"), 0644)
	s.CommitAll("hive: task #3 — bad code")

	s.RejectBranch("main", branch2)

	// bad.go should not exist on main.
	if _, err := os.Stat(filepath.Join(dir, "bad.go")); !os.IsNotExist(err) {
		t.Fatal("bad.go should not exist on main after reject")
	}
}
