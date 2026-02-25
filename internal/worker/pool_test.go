package worker

import (
	"testing"
	"time"

	"github.com/imkarma/hive/internal/store"
)

func TestTaskResult_Fields(t *testing.T) {
	r := TaskResult{
		TaskID:   42,
		Title:    "Test task",
		Status:   "done",
		Duration: 5 * time.Second,
		Log:      []string{"started", "completed"},
	}

	if r.TaskID != 42 {
		t.Errorf("expected TaskID 42, got %d", r.TaskID)
	}
	if r.Status != "done" {
		t.Errorf("expected status done, got %s", r.Status)
	}
	if len(r.Log) != 2 {
		t.Errorf("expected 2 log entries, got %d", len(r.Log))
	}
}

func TestPoolConfig_Defaults(t *testing.T) {
	pc := PoolConfig{
		MaxWorkers: 3,
		MaxLoops:   5,
		CoderName:  "claude",
		ReviewName: "gemini",
	}

	if pc.MaxWorkers != 3 {
		t.Errorf("expected 3 workers, got %d", pc.MaxWorkers)
	}
	if pc.MaxLoops != 5 {
		t.Errorf("expected 5 loops, got %d", pc.MaxLoops)
	}
}

func TestPool_RunSequential_EmptyTasks(t *testing.T) {
	pool := &Pool{
		maxWorkers: 1,
		maxLoops:   3,
	}

	results := pool.Run([]store.Task{})
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty tasks, got %d", len(results))
	}
}

func TestPool_RunParallel_SkipsDoneAndBlocked(t *testing.T) {
	pool := &Pool{
		maxWorkers: 3,
		maxLoops:   3,
	}

	tasks := []store.Task{
		{ID: 1, Title: "Done task", Status: store.StatusDone, AssignedAgent: "test"},
		{ID: 2, Title: "Blocked task", Status: store.StatusBlocked, BlockedReason: "need info", AssignedAgent: "test"},
		{ID: 3, Title: "Unassigned task", Status: store.StatusBacklog},
	}

	results := pool.runParallel(tasks)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].Status != "done" {
		t.Errorf("task 1: expected done, got %s", results[0].Status)
	}
	if results[1].Status != "blocked" {
		t.Errorf("task 2: expected blocked, got %s", results[1].Status)
	}
	if results[2].Status != "failed" {
		t.Errorf("task 3: expected failed (no agent), got %s", results[2].Status)
	}
}
