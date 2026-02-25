package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides access to the hive database.
type Store struct {
	db *sql.DB
}

// New opens (or creates) the SQLite database at the given path.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent access.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS tasks (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		parent_id       INTEGER REFERENCES tasks(id),
		kind            TEXT NOT NULL DEFAULT 'task',
		title           TEXT NOT NULL,
		description     TEXT DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'backlog',
		assigned_agent  TEXT DEFAULT '',
		role            TEXT DEFAULT '',
		priority        TEXT DEFAULT 'medium',
		blocked_reason  TEXT DEFAULT '',
		git_branch      TEXT DEFAULT '',
		created_at      DATETIME NOT NULL,
		updated_at      DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS events (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id     INTEGER NOT NULL REFERENCES tasks(id),
		agent       TEXT DEFAULT '',
		event_type  TEXT NOT NULL,
		content     TEXT DEFAULT '',
		timestamp   DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS artifacts (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id     INTEGER NOT NULL REFERENCES tasks(id),
		type        TEXT NOT NULL,
		file_path   TEXT NOT NULL,
		timestamp   DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS reviews (
		id              INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id         INTEGER NOT NULL REFERENCES tasks(id),
		reviewer_agent  TEXT NOT NULL,
		verdict         TEXT NOT NULL,
		comments        TEXT DEFAULT '',
		timestamp       DATETIME NOT NULL
	);
	`
	if _, err := s.db.Exec(schema); err != nil {
		return err
	}

	// Pipeline runs table for resume-after-crash.
	_, _ = s.db.Exec(`
	CREATE TABLE IF NOT EXISTS pipeline_runs (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		epic_id     INTEGER NOT NULL REFERENCES tasks(id),
		status      TEXT NOT NULL DEFAULT 'running',
		max_loops   INTEGER NOT NULL DEFAULT 3,
		parallel    INTEGER NOT NULL DEFAULT 1,
		started_at  DATETIME NOT NULL,
		ended_at    DATETIME
	);
	`)

	// Migrate existing databases: add new columns if missing.
	s.addColumnIfMissing("tasks", "kind", "TEXT NOT NULL DEFAULT 'task'")
	s.addColumnIfMissing("tasks", "git_branch", "TEXT DEFAULT ''")

	return nil
}

// addColumnIfMissing adds a column to a table if it doesn't exist yet.
// Used for schema migrations on existing databases.
func (s *Store) addColumnIfMissing(table, column, colDef string) {
	// Check if column exists via PRAGMA.
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dfltValue *string
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return
		}
		if name == column {
			return // Column already exists.
		}
	}

	// Column doesn't exist — add it.
	s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + colDef)
}

// CreateTask inserts a new task (kind=task) and returns it with the generated ID.
func (s *Store) CreateTask(title, description, priority string, parentID *int64) (*Task, error) {
	return s.createItem(KindTask, title, description, priority, parentID)
}

// CreateEpic inserts a new epic (kind=epic) and returns it with the generated ID.
func (s *Store) CreateEpic(title, description, priority string) (*Task, error) {
	return s.createItem(KindEpic, title, description, priority, nil)
}

// createItem is the shared insert logic for both epics and tasks.
func (s *Store) createItem(kind TaskKind, title, description, priority string, parentID *int64) (*Task, error) {
	now := time.Now().UTC()
	if priority == "" {
		priority = "medium"
	}

	res, err := s.db.Exec(
		`INSERT INTO tasks (kind, title, description, status, priority, parent_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		string(kind), title, description, string(StatusBacklog), priority, parentID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert %s: %w", kind, err)
	}

	id, _ := res.LastInsertId()

	label := "Task"
	if kind == KindEpic {
		label = "Epic"
	}
	s.AddEvent(id, "", "created", fmt.Sprintf("%s created: %s", label, title))

	return &Task{
		ID:          id,
		ParentID:    parentID,
		Kind:        kind,
		Title:       title,
		Description: description,
		Status:      StatusBacklog,
		Priority:    priority,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// taskColumns is the standard column list for task queries.
const taskColumns = `id, parent_id, kind, title, description, status, assigned_agent, role, priority, blocked_reason, git_branch, created_at, updated_at`

// GetTask returns a single task or epic by ID.
func (s *Store) GetTask(id int64) (*Task, error) {
	row := s.db.QueryRow(
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

// ListTasks returns all items (epics + tasks), optionally filtered by status.
func (s *Store) ListTasks(status string) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks`
	var args []any
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id`

	return s.queryTasks(query, args...)
}

// ListEpics returns all epics, optionally filtered by status.
func (s *Store) ListEpics(status string) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE kind = 'epic'`
	var args []any
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id`

	return s.queryTasks(query, args...)
}

// ListTasksByEpic returns all tasks belonging to an epic.
func (s *Store) ListTasksByEpic(epicID int64) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE parent_id = ? ORDER BY id`
	return s.queryTasks(query, epicID)
}

// ListOnlyTasks returns items with kind='task' (no epics), optionally filtered by status.
func (s *Store) ListOnlyTasks(status string) ([]Task, error) {
	query := `SELECT ` + taskColumns + ` FROM tasks WHERE kind = 'task'`
	var args []any
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id`

	return s.queryTasks(query, args...)
}

// queryTasks is a shared helper for running task-list queries.
func (s *Store) queryTasks(query string, args ...any) ([]Task, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		t, err := scanTaskRows(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

// UpdateTaskStatus changes the status of a task.
func (s *Store) UpdateTaskStatus(id int64, status TaskStatus) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), now, id,
	)
	if err != nil {
		return fmt.Errorf("update task status: %w", err)
	}
	s.AddEvent(id, "", "status_changed", fmt.Sprintf("Status changed to %s", status))
	return nil
}

// AssignTask assigns an agent and role to a task.
func (s *Store) AssignTask(id int64, agent, role string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET assigned_agent = ?, role = ?, updated_at = ? WHERE id = ?`,
		agent, role, now, id,
	)
	if err != nil {
		return fmt.Errorf("assign task: %w", err)
	}
	s.AddEvent(id, agent, "assigned", fmt.Sprintf("Assigned to %s (role: %s)", agent, role))
	return nil
}

// BlockTask marks a task as blocked with a reason.
func (s *Store) BlockTask(id int64, reason string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, blocked_reason = ?, updated_at = ? WHERE id = ?`,
		string(StatusBlocked), reason, now, id,
	)
	if err != nil {
		return fmt.Errorf("block task: %w", err)
	}
	s.AddEvent(id, "", "blocked", reason)
	return nil
}

// UnblockTask resolves a blocker with the user's answer.
func (s *Store) UnblockTask(id int64, answer string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET status = ?, blocked_reason = '', updated_at = ? WHERE id = ?`,
		string(StatusBacklog), now, id,
	)
	if err != nil {
		return fmt.Errorf("unblock task: %w", err)
	}
	s.AddEvent(id, "user", "unblocked", fmt.Sprintf("User answered: %s", answer))
	return nil
}

// GetEvents returns all events for a task.
func (s *Store) GetEvents(taskID int64) ([]Event, error) {
	rows, err := s.db.Query(
		`SELECT id, task_id, agent, event_type, content, timestamp FROM events WHERE task_id = ? ORDER BY timestamp`,
		taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.TaskID, &e.Agent, &e.Type, &e.Content, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// AddArtifact records an artifact for a task.
func (s *Store) AddArtifact(taskID int64, artifactType, filePath string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO artifacts (task_id, type, file_path, timestamp) VALUES (?, ?, ?, ?)`,
		taskID, artifactType, filePath, now,
	)
	return err
}

// AddReview records a review verdict.
func (s *Store) AddReview(taskID int64, reviewerAgent, verdict, comments string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`INSERT INTO reviews (task_id, reviewer_agent, verdict, comments, timestamp) VALUES (?, ?, ?, ?, ?)`,
		taskID, reviewerAgent, verdict, comments, now,
	)
	if err != nil {
		return err
	}
	s.AddEvent(taskID, reviewerAgent, "reviewed", fmt.Sprintf("Verdict: %s", verdict))
	return nil
}

// SetGitBranch records the git safety branch for an epic or task.
func (s *Store) SetGitBranch(id int64, branch string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE tasks SET git_branch = ?, updated_at = ? WHERE id = ?`,
		branch, now, id,
	)
	if err != nil {
		return fmt.Errorf("set git branch: %w", err)
	}
	return nil
}

// --- Pipeline run tracking ---

// StartPipelineRun records a new pipeline run.
func (s *Store) StartPipelineRun(epicID int64, maxLoops, parallel int) (int64, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`INSERT INTO pipeline_runs (epic_id, status, max_loops, parallel, started_at)
		 VALUES (?, 'running', ?, ?, ?)`,
		epicID, maxLoops, parallel, now,
	)
	if err != nil {
		return 0, fmt.Errorf("start pipeline run: %w", err)
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// EndPipelineRun marks a pipeline run as completed or failed.
func (s *Store) EndPipelineRun(runID int64, status string) error {
	now := time.Now().UTC()
	_, err := s.db.Exec(
		`UPDATE pipeline_runs SET status = ?, ended_at = ? WHERE id = ?`,
		status, now, runID,
	)
	return err
}

// GetActivePipelineRun returns the most recent running pipeline for an epic,
// or nil if none is active.
func (s *Store) GetActivePipelineRun(epicID int64) (*PipelineRun, error) {
	row := s.db.QueryRow(
		`SELECT id, epic_id, status, max_loops, parallel, started_at, ended_at
		 FROM pipeline_runs
		 WHERE epic_id = ? AND status = 'running'
		 ORDER BY id DESC LIMIT 1`, epicID,
	)
	var r PipelineRun
	var endedAt sql.NullTime
	err := row.Scan(&r.ID, &r.EpicID, &r.Status, &r.MaxLoops, &r.Parallel, &r.StartedAt, &endedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active pipeline: %w", err)
	}
	if endedAt.Valid {
		r.EndedAt = endedAt.Time
	}
	return &r, nil
}

// ListInterruptedRuns returns all pipeline runs with status='running'
// (these were interrupted by a crash).
func (s *Store) ListInterruptedRuns() ([]PipelineRun, error) {
	rows, err := s.db.Query(
		`SELECT id, epic_id, status, max_loops, parallel, started_at, ended_at
		 FROM pipeline_runs WHERE status = 'running' ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list interrupted runs: %w", err)
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var endedAt sql.NullTime
		if err := rows.Scan(&r.ID, &r.EpicID, &r.Status, &r.MaxLoops, &r.Parallel, &r.StartedAt, &endedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		if endedAt.Valid {
			r.EndedAt = endedAt.Time
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ResetStaleTasks finds tasks stuck in in_progress or review status
// (likely from a crash) and resets them to backlog.
func (s *Store) ResetStaleTasks(epicID int64) (int, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		`UPDATE tasks SET status = ?, updated_at = ?
		 WHERE parent_id = ? AND status IN (?, ?)`,
		string(StatusBacklog), now, epicID,
		string(StatusInProgress), string(StatusReview),
	)
	if err != nil {
		return 0, fmt.Errorf("reset stale tasks: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// AddEvent records an event for a task.
func (s *Store) AddEvent(taskID int64, agent, eventType, content string) {
	now := time.Now().UTC()
	s.db.Exec(
		`INSERT INTO events (task_id, agent, event_type, content, timestamp) VALUES (?, ?, ?, ?, ?)`,
		taskID, agent, eventType, content, now,
	)
}

// scanTask scans a single task from a *sql.Row.
func scanTask(row *sql.Row) (*Task, error) {
	var t Task
	var parentID sql.NullInt64
	err := row.Scan(
		&t.ID, &parentID, &t.Kind, &t.Title, &t.Description, &t.Status,
		&t.AssignedAgent, &t.Role, &t.Priority, &t.BlockedReason,
		&t.GitBranch, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	if parentID.Valid {
		t.ParentID = &parentID.Int64
	}
	return &t, nil
}

// scanTaskRows scans a single task from *sql.Rows.
func scanTaskRows(rows *sql.Rows) (*Task, error) {
	var t Task
	var parentID sql.NullInt64
	err := rows.Scan(
		&t.ID, &parentID, &t.Kind, &t.Title, &t.Description, &t.Status,
		&t.AssignedAgent, &t.Role, &t.Priority, &t.BlockedReason,
		&t.GitBranch, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	if parentID.Valid {
		t.ParentID = &parentID.Int64
	}
	return &t, nil
}
