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

// Task represents a unit of work on the kanban board.
type Task struct {
	ID            int64      `json:"id"`
	ParentID      *int64     `json:"parent_id,omitempty"`
	Title         string     `json:"title"`
	Description   string     `json:"description,omitempty"`
	Status        TaskStatus `json:"status"`
	AssignedAgent string     `json:"assigned_agent,omitempty"`
	Role          string     `json:"role,omitempty"`
	Priority      string     `json:"priority,omitempty"` // high, medium, low
	BlockedReason string     `json:"blocked_reason,omitempty"`
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
