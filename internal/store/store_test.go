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

// --- Epic/Task Kind tests ---

func TestCreateEpic(t *testing.T) {
	s := testStore(t)

	epic, err := s.CreateEpic("Add JWT auth", "Full JWT authentication with refresh tokens", "high")
	if err != nil {
		t.Fatalf("CreateEpic: %v", err)
	}
	if epic.Kind != KindEpic {
		t.Errorf("expected kind epic, got %s", epic.Kind)
	}
	if epic.ParentID != nil {
		t.Errorf("expected nil parent for epic, got %v", epic.ParentID)
	}
	if epic.Title != "Add JWT auth" {
		t.Errorf("expected title 'Add JWT auth', got %q", epic.Title)
	}
}

func TestCreateTask_HasKindTask(t *testing.T) {
	s := testStore(t)

	task, err := s.CreateTask("Implement endpoint", "", "medium", nil)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.Kind != KindTask {
		t.Errorf("expected kind task, got %s", task.Kind)
	}
}

func TestListEpics(t *testing.T) {
	s := testStore(t)

	s.CreateEpic("Epic 1", "", "high")
	s.CreateEpic("Epic 2", "", "medium")
	s.CreateTask("Task under no epic", "", "low", nil)

	epics, err := s.ListEpics("")
	if err != nil {
		t.Fatalf("ListEpics: %v", err)
	}
	if len(epics) != 2 {
		t.Fatalf("expected 2 epics, got %d", len(epics))
	}
	for _, e := range epics {
		if e.Kind != KindEpic {
			t.Errorf("ListEpics returned non-epic: kind=%s", e.Kind)
		}
	}
}

func TestListEpics_FilterByStatus(t *testing.T) {
	s := testStore(t)

	e1, _ := s.CreateEpic("Backlog epic", "", "high")
	e2, _ := s.CreateEpic("Done epic", "", "medium")
	s.UpdateTaskStatus(e2.ID, StatusDone)
	_ = e1

	epics, err := s.ListEpics("done")
	if err != nil {
		t.Fatalf("ListEpics done: %v", err)
	}
	if len(epics) != 1 {
		t.Fatalf("expected 1 done epic, got %d", len(epics))
	}
}

func TestListTasksByEpic(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Big feature", "", "high")
	epicID := epic.ID

	s.CreateTask("Sub 1", "", "high", &epicID)
	s.CreateTask("Sub 2", "", "medium", &epicID)
	s.CreateTask("Unrelated task", "", "low", nil)

	tasks, err := s.ListTasksByEpic(epicID)
	if err != nil {
		t.Fatalf("ListTasksByEpic: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks under epic, got %d", len(tasks))
	}
	for _, t2 := range tasks {
		if t2.ParentID == nil || *t2.ParentID != epicID {
			t.Errorf("task %d has wrong parent: %v", t2.ID, t2.ParentID)
		}
	}
}

func TestListOnlyTasks(t *testing.T) {
	s := testStore(t)

	s.CreateEpic("An epic", "", "high")
	s.CreateTask("A task", "", "medium", nil)
	s.CreateTask("Another task", "", "low", nil)

	tasks, err := s.ListOnlyTasks("")
	if err != nil {
		t.Fatalf("ListOnlyTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	for _, t2 := range tasks {
		if t2.Kind != KindTask {
			t.Errorf("ListOnlyTasks returned non-task: kind=%s", t2.Kind)
		}
	}
}

func TestSetGitBranch(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Branch test", "", "high")

	err := s.SetGitBranch(epic.ID, "hive/epic-1")
	if err != nil {
		t.Fatalf("SetGitBranch: %v", err)
	}

	got, _ := s.GetTask(epic.ID)
	if got.GitBranch != "hive/epic-1" {
		t.Errorf("expected git_branch 'hive/epic-1', got %q", got.GitBranch)
	}
}

func TestGetTask_ReturnsKind(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Epic with kind", "", "high")
	got, err := s.GetTask(epic.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Kind != KindEpic {
		t.Errorf("expected kind epic from GetTask, got %s", got.Kind)
	}
}

func TestEpicEvent_SaysEpic(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Event test", "", "high")
	events, err := s.GetEvents(epic.ID)
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least 1 event")
	}
	if events[0].Type != "created" {
		t.Errorf("expected 'created' event, got %q", events[0].Type)
	}
	if events[0].Content != "Epic created: Event test" {
		t.Errorf("expected 'Epic created: Event test', got %q", events[0].Content)
	}
}

// --- Pipeline run tests ---

func TestStartPipelineRun(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Pipeline epic", "", "high")

	runID, err := s.StartPipelineRun(epic.ID, 5, 2)
	if err != nil {
		t.Fatalf("StartPipelineRun: %v", err)
	}
	if runID < 1 {
		t.Errorf("expected positive run ID, got %d", runID)
	}
}

func TestGetActivePipelineRun(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Active pipeline", "", "high")

	// No active runs initially.
	run, err := s.GetActivePipelineRun(epic.ID)
	if err != nil {
		t.Fatalf("GetActivePipelineRun: %v", err)
	}
	if run != nil {
		t.Fatal("expected nil for no active runs")
	}

	// Start a run.
	runID, _ := s.StartPipelineRun(epic.ID, 3, 1)

	run, err = s.GetActivePipelineRun(epic.ID)
	if err != nil {
		t.Fatalf("GetActivePipelineRun after start: %v", err)
	}
	if run == nil {
		t.Fatal("expected active run, got nil")
	}
	if run.ID != runID {
		t.Errorf("expected run ID %d, got %d", runID, run.ID)
	}
	if run.EpicID != epic.ID {
		t.Errorf("expected epic ID %d, got %d", epic.ID, run.EpicID)
	}
	if run.Status != "running" {
		t.Errorf("expected status 'running', got %q", run.Status)
	}
	if run.MaxLoops != 3 {
		t.Errorf("expected max_loops 3, got %d", run.MaxLoops)
	}
	if run.Parallel != 1 {
		t.Errorf("expected parallel 1, got %d", run.Parallel)
	}
}

func TestEndPipelineRun(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("End pipeline", "", "high")
	runID, _ := s.StartPipelineRun(epic.ID, 3, 1)

	// End it as completed.
	if err := s.EndPipelineRun(runID, "completed"); err != nil {
		t.Fatalf("EndPipelineRun: %v", err)
	}

	// Should no longer be active.
	run, _ := s.GetActivePipelineRun(epic.ID)
	if run != nil {
		t.Fatal("expected no active run after end")
	}
}

func TestEndPipelineRun_Failed(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Failed pipeline", "", "high")
	runID, _ := s.StartPipelineRun(epic.ID, 3, 1)

	if err := s.EndPipelineRun(runID, "failed"); err != nil {
		t.Fatalf("EndPipelineRun failed: %v", err)
	}

	// Should no longer be active.
	run, _ := s.GetActivePipelineRun(epic.ID)
	if run != nil {
		t.Fatal("expected no active run after failure")
	}
}

func TestListInterruptedRuns(t *testing.T) {
	s := testStore(t)

	e1, _ := s.CreateEpic("Epic A", "", "high")
	e2, _ := s.CreateEpic("Epic B", "", "medium")

	// Start two runs (simulating crashes — they stay "running").
	s.StartPipelineRun(e1.ID, 3, 1)
	s.StartPipelineRun(e2.ID, 5, 4)

	// Complete one of them.
	run3ID, _ := s.StartPipelineRun(e1.ID, 3, 1)
	s.EndPipelineRun(run3ID, "completed")

	runs, err := s.ListInterruptedRuns()
	if err != nil {
		t.Fatalf("ListInterruptedRuns: %v", err)
	}
	// Two "running" runs remain (the first two).
	if len(runs) != 2 {
		t.Fatalf("expected 2 interrupted runs, got %d", len(runs))
	}

	// Should be ordered by started_at DESC.
	if runs[0].EpicID != e2.ID {
		t.Errorf("expected most recent interrupted run for epic %d, got %d", e2.ID, runs[0].EpicID)
	}
}

func TestListInterruptedRuns_Empty(t *testing.T) {
	s := testStore(t)

	runs, err := s.ListInterruptedRuns()
	if err != nil {
		t.Fatalf("ListInterruptedRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("expected 0 interrupted runs, got %d", len(runs))
	}
}

func TestResetStaleTasks(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Reset epic", "", "high")
	epicID := epic.ID

	t1, _ := s.CreateTask("Stuck task 1", "", "high", &epicID)
	t2, _ := s.CreateTask("Stuck task 2", "", "medium", &epicID)
	t3, _ := s.CreateTask("Done task", "", "low", &epicID)
	t4, _ := s.CreateTask("Backlog task", "", "low", &epicID)

	// Simulate mid-pipeline state.
	s.UpdateTaskStatus(t1.ID, StatusInProgress)
	s.UpdateTaskStatus(t2.ID, StatusReview)
	s.UpdateTaskStatus(t3.ID, StatusDone)
	_ = t4 // stays in backlog

	count, err := s.ResetStaleTasks(epicID)
	if err != nil {
		t.Fatalf("ResetStaleTasks: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 reset tasks, got %d", count)
	}

	// Verify the tasks were reset.
	got1, _ := s.GetTask(t1.ID)
	if got1.Status != StatusBacklog {
		t.Errorf("expected task 1 to be backlog, got %s", got1.Status)
	}

	got2, _ := s.GetTask(t2.ID)
	if got2.Status != StatusBacklog {
		t.Errorf("expected task 2 to be backlog, got %s", got2.Status)
	}

	// Done and backlog tasks should be untouched.
	got3, _ := s.GetTask(t3.ID)
	if got3.Status != StatusDone {
		t.Errorf("expected task 3 to remain done, got %s", got3.Status)
	}

	got4, _ := s.GetTask(t4.ID)
	if got4.Status != StatusBacklog {
		t.Errorf("expected task 4 to remain backlog, got %s", got4.Status)
	}
}

func TestResetStaleTasks_NoneToReset(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Clean epic", "", "high")
	epicID := epic.ID

	s.CreateTask("Done task", "", "high", &epicID)
	t1, _ := s.GetTask(2) // The task we just created
	s.UpdateTaskStatus(t1.ID, StatusDone)

	count, err := s.ResetStaleTasks(epicID)
	if err != nil {
		t.Fatalf("ResetStaleTasks: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 reset tasks, got %d", count)
	}
}

func TestGetActivePipelineRun_ReturnsMostRecent(t *testing.T) {
	s := testStore(t)

	epic, _ := s.CreateEpic("Multi-run epic", "", "high")

	// Start two runs for the same epic (shouldn't happen normally, but test defensive behavior).
	s.StartPipelineRun(epic.ID, 3, 1)
	run2ID, _ := s.StartPipelineRun(epic.ID, 5, 2)

	run, err := s.GetActivePipelineRun(epic.ID)
	if err != nil {
		t.Fatalf("GetActivePipelineRun: %v", err)
	}
	if run == nil {
		t.Fatal("expected active run")
	}
	// Should return the most recent one (higher ID).
	if run.ID != run2ID {
		t.Errorf("expected most recent run ID %d, got %d", run2ID, run.ID)
	}
	if run.MaxLoops != 5 {
		t.Errorf("expected max_loops 5 from most recent, got %d", run.MaxLoops)
	}
	if run.Parallel != 2 {
		t.Errorf("expected parallel 2 from most recent, got %d", run.Parallel)
	}
}
