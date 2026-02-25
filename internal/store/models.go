package store

import "time"

// TaskStatus represents the current state of a task on the board.
type TaskStatus string

const (
	StatusBacklog    TaskStatus = "backlog"
	StatusInProgress TaskStatus = "in_progress"
	StatusBlocked    TaskStatus = "blocked"
	StatusReview     TaskStatus = "review"
	StatusDone       TaskStatus = "done"
	StatusFailed     TaskStatus = "failed"
)

// TaskKind distinguishes epics (user-created) from tasks (PM-generated).
type TaskKind string

const (
	KindEpic TaskKind = "epic" // User-created high-level work item
	KindTask TaskKind = "task" // PM-generated or manually-created actionable task
)

// Task represents a unit of work on the kanban board.
// An epic is a high-level item the user creates; tasks are what the PM agent
// breaks it into. Both share the same struct — Kind distinguishes them.
type Task struct {
	ID            int64      `json:"id"`
	ParentID      *int64     `json:"parent_id,omitempty"` // For tasks: points to the epic
	Kind          TaskKind   `json:"kind"`                // "epic" or "task"
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Status        TaskStatus `json:"status"`
	AssignedAgent string     `json:"assigned_agent,omitempty"`
	Role          string     `json:"role,omitempty"`
	Priority      string     `json:"priority,omitempty"` // high, medium, low
	BlockedReason string     `json:"blocked_reason,omitempty"`
	GitBranch     string     `json:"git_branch,omitempty"` // Safety branch for this epic/task
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Event represents something that happened to a task.
type Event struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Agent     string    `json:"agent,omitempty"`
	Type      string    `json:"event_type"` // created, started, blocked, unblocked, completed, failed, comment
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Artifact represents a file produced during task execution.
type Artifact struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Type      string    `json:"type"` // diff, plan, log, review
	FilePath  string    `json:"file_path"`
	Timestamp time.Time `json:"timestamp"`
}

// Review represents a code review verdict.
type Review struct {
	ID            int64     `json:"id"`
	TaskID        int64     `json:"task_id"`
	ReviewerAgent string    `json:"reviewer_agent"`
	Verdict       string    `json:"verdict"` // approve, reject
	Comments      string    `json:"comments"`
	Timestamp     time.Time `json:"timestamp"`
}

// PipelineRun tracks an auto pipeline execution for resume-after-crash.
type PipelineRun struct {
	ID        int64     `json:"id"`
	EpicID    int64     `json:"epic_id"`
	Status    string    `json:"status"` // running, completed, failed, interrupted
	MaxLoops  int       `json:"max_loops"`
	Parallel  int       `json:"parallel"`
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at,omitempty"`
}
