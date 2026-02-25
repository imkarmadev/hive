package context

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/imkarma/hive/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestBuildPrompt_BasicTask(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Implement login", "Create POST /auth/login endpoint", "high", nil)

	prompt, err := b.BuildPrompt(task, "coder")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// Should contain role header.
	if !strings.Contains(prompt, "Software Developer") {
		t.Error("prompt missing coder role header")
	}

	// Should contain task title.
	if !strings.Contains(prompt, "Implement login") {
		t.Error("prompt missing task title")
	}

	// Should contain description.
	if !strings.Contains(prompt, "POST /auth/login") {
		t.Error("prompt missing task description")
	}

	// Should contain role-specific instructions.
	if !strings.Contains(prompt, "BLOCKED:") {
		t.Error("prompt missing BLOCKED instruction for coder")
	}
}

func TestBuildPrompt_ReviewerRole(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Review auth", "", "high", nil)

	prompt, err := b.BuildPrompt(task, "reviewer")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if !strings.Contains(prompt, "Code Reviewer") {
		t.Error("prompt missing reviewer role header")
	}
	if !strings.Contains(prompt, "VERDICT:") {
		t.Error("prompt missing VERDICT format for reviewer")
	}
}

func TestBuildPrompt_PMRole(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Plan feature", "", "high", nil)

	prompt, err := b.BuildPrompt(task, "pm")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	if !strings.Contains(prompt, "Project Manager") {
		t.Error("prompt missing PM role header")
	}
	if !strings.Contains(prompt, "SUBTASKS:") {
		t.Error("prompt missing SUBTASKS format for PM")
	}
}

func TestBuildPrompt_WithParentContext(t *testing.T) {
	s := testStore(t)
	b := New(s)

	parent, _ := s.CreateTask("Add authentication", "Full JWT auth system", "high", nil)
	parentID := parent.ID
	child, _ := s.CreateTask("Write login tests", "Unit tests for login", "medium", &parentID)

	prompt, err := b.BuildPrompt(child, "tester")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// Should contain parent context.
	if !strings.Contains(prompt, "Parent Task") {
		t.Error("prompt missing parent task section")
	}
	if !strings.Contains(prompt, "Add authentication") {
		t.Error("prompt missing parent task title")
	}
	if !strings.Contains(prompt, "Full JWT auth system") {
		t.Error("prompt missing parent task description")
	}

	// Should also contain the child task.
	if !strings.Contains(prompt, "Write login tests") {
		t.Error("prompt missing child task title")
	}
}

func TestBuildPrompt_WithEventHistory(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Task with history", "", "high", nil)

	// Block and unblock to create history.
	s.BlockTask(task.ID, "REST or GraphQL?")
	s.UnblockTask(task.ID, "Use REST with OpenAPI")

	// Re-fetch task after status changes.
	task, _ = s.GetTask(task.ID)

	prompt, err := b.BuildPrompt(task, "coder")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// Should contain the user's answer in history.
	if !strings.Contains(prompt, "History") {
		t.Error("prompt missing history section")
	}
	if !strings.Contains(prompt, "Use REST with OpenAPI") {
		t.Error("prompt missing user answer in history")
	}
}

func TestBuildPrompt_NoHistoryForFreshTask(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Fresh task", "", "high", nil)

	prompt, err := b.BuildPrompt(task, "coder")
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}

	// "created" event is not a relevant event for context, so no History section.
	if strings.Contains(prompt, "History") {
		t.Error("prompt should not have History section for fresh task")
	}
}

func TestBuildPrompt_AllRoles(t *testing.T) {
	s := testStore(t)
	b := New(s)

	task, _ := s.CreateTask("Test all roles", "", "high", nil)

	roles := []struct {
		role     string
		contains string
	}{
		{"pm", "Project Manager"},
		{"coder", "Software Developer"},
		{"reviewer", "Code Reviewer"},
		{"tester", "QA Engineer"},
		{"analyst", "Technical Analyst"},
		{"custom-role", "custom-role"},
	}

	for _, tc := range roles {
		prompt, err := b.BuildPrompt(task, tc.role)
		if err != nil {
			t.Fatalf("BuildPrompt for %s: %v", tc.role, err)
		}
		if !strings.Contains(prompt, tc.contains) {
			t.Errorf("role %s: prompt missing %q", tc.role, tc.contains)
		}
	}
}
