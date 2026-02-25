package store

import (
	"os"
	"path/filepath"
	"testing"
)

// testStore creates a temporary store for testing.
func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNew_CreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}
}

func TestCreateTask(t *testing.T) {
	s := testStore(t)

	task, err := s.CreateTask("Test task", "A description", "high", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.ID != 1 {
		t.Errorf("expected ID 1, got %d", task.ID)
	}
	if task.Title != "Test task" {
		t.Errorf("expected title 'Test task', got %q", task.Title)
	}
	if task.Description != "A description" {
		t.Errorf("expected description 'A description', got %q", task.Description)
	}
	if task.Status != StatusBacklog {
		t.Errorf("expected status backlog, got %s", task.Status)
	}
	if task.Priority != "high" {
		t.Errorf("expected priority high, got %s", task.Priority)
	}
	if task.ParentID != nil {
		t.Errorf("expected nil parent, got %v", task.ParentID)
	}
}

func TestCreateTask_DefaultPriority(t *testing.T) {
	s := testStore(t)

	task, err := s.CreateTask("No priority", "", "", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Priority != "medium" {
		t.Errorf("expected default priority 'medium', got %q", task.Priority)
	}
}

func TestCreateTask_WithParent(t *testing.T) {
	s := testStore(t)

	parent, _ := s.CreateTask("Parent", "", "high", nil)
	parentID := parent.ID

	child, err := s.CreateTask("Child", "", "low", &parentID)
	if err != nil {
		t.Fatalf("CreateTask child: %v", err)
	}
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Errorf("expected parent ID %d, got %v", parent.ID, child.ParentID)
	}
}

func TestGetTask(t *testing.T) {
	s := testStore(t)

	created, _ := s.CreateTask("Get me", "desc", "low", nil)
	got, err := s.GetTask(created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Title != "Get me" {
		t.Errorf("expected 'Get me', got %q", got.Title)
	}
	if got.Description != "desc" {
		t.Errorf("expected 'desc', got %q", got.Description)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	s := testStore(t)

	_, err := s.GetTask(999)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestListTasks(t *testing.T) {
	s := testStore(t)

	s.CreateTask("Task 1", "", "high", nil)
	s.CreateTask("Task 2", "", "medium", nil)
	s.CreateTask("Task 3", "", "low", nil)

	tasks, err := s.ListTasks("")
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestListTasks_FilterByStatus(t *testing.T) {
	s := testStore(t)

	t1, _ := s.CreateTask("Backlog task", "", "", nil)
	t2, _ := s.CreateTask("Done task", "", "", nil)
	s.UpdateTaskStatus(t2.ID, StatusDone)
	_ = t1 // keep in backlog

	backlog, err := s.ListTasks("backlog")
	if err != nil {
		t.Fatalf("ListTasks backlog: %v", err)
	}
	if len(backlog) != 1 {
		t.Errorf("expected 1 backlog task, got %d", len(backlog))
	}

	done, err := s.ListTasks("done")
	if err != nil {
		t.Fatalf("ListTasks done: %v", err)
	}
	if len(done) != 1 {
		t.Errorf("expected 1 done task, got %d", len(done))
	}
}

func TestUpdateTaskStatus(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Status test", "", "", nil)

	statuses := []TaskStatus{StatusInProgress, StatusReview, StatusDone}
	for _, status := range statuses {
		if err := s.UpdateTaskStatus(task.ID, status); err != nil {
			t.Fatalf("UpdateTaskStatus to %s: %v", status, err)
		}
		got, _ := s.GetTask(task.ID)
		if got.Status != status {
			t.Errorf("expected %s, got %s", status, got.Status)
		}
	}
}

func TestAssignTask(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Assign test", "", "", nil)
	if err := s.AssignTask(task.ID, "claude-dev", "coder"); err != nil {
		t.Fatalf("AssignTask: %v", err)
	}

	got, _ := s.GetTask(task.ID)
	if got.AssignedAgent != "claude-dev" {
		t.Errorf("expected agent 'claude-dev', got %q", got.AssignedAgent)
	}
	if got.Role != "coder" {
		t.Errorf("expected role 'coder', got %q", got.Role)
	}
}

func TestBlockAndUnblock(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Block test", "", "", nil)

	// Block.
	if err := s.BlockTask(task.ID, "Which DB to use?"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}
	got, _ := s.GetTask(task.ID)
	if got.Status != StatusBlocked {
		t.Errorf("expected blocked, got %s", got.Status)
	}
	if got.BlockedReason != "Which DB to use?" {
		t.Errorf("expected reason 'Which DB to use?', got %q", got.BlockedReason)
	}

	// Unblock.
	if err := s.UnblockTask(task.ID, "PostgreSQL"); err != nil {
		t.Fatalf("UnblockTask: %v", err)
	}
	got, _ = s.GetTask(task.ID)
	if got.Status != StatusBacklog {
		t.Errorf("expected backlog after unblock, got %s", got.Status)
	}
	if got.BlockedReason != "" {
		t.Errorf("expected empty reason after unblock, got %q", got.BlockedReason)
	}
}

func TestEvents(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Events test", "", "", nil)

	// CreateTask already adds a "created" event.
	events, err := s.GetEvents(task.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after create, got %d", len(events))
	}
	if events[0].Type != "created" {
		t.Errorf("expected 'created' event, got %q", events[0].Type)
	}

	// Block adds another event.
	s.BlockTask(task.ID, "need info")
	events, _ = s.GetEvents(task.ID)
	if len(events) != 2 {
		// created + blocked
		t.Errorf("expected 2 events after block, got %d", len(events))
	}

	// Unblock adds events too.
	s.UnblockTask(task.ID, "here is info")
	events, _ = s.GetEvents(task.ID)

	// Check last event is unblocked with user answer.
	last := events[len(events)-1]
	if last.Type != "unblocked" {
		t.Errorf("expected 'unblocked' event, got %q", last.Type)
	}
	if last.Agent != "user" {
		t.Errorf("expected agent 'user', got %q", last.Agent)
	}
}

func TestAddArtifact(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Artifact test", "", "", nil)
	if err := s.AddArtifact(task.ID, "diff", "/tmp/test.diff"); err != nil {
		t.Fatalf("AddArtifact: %v", err)
	}
}

func TestAddReview(t *testing.T) {
	s := testStore(t)

	task, _ := s.CreateTask("Review test", "", "", nil)
	if err := s.AddReview(task.ID, "gpt-reviewer", "approve", "Looks good"); err != nil {
		t.Fatalf("AddReview: %v", err)
	}

	// Should also create an event.
	events, _ := s.GetEvents(task.ID)
	found := false
	for _, e := range events {
		if e.Type == "reviewed" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'reviewed' event after AddReview")
	}
}
