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
		title           TEXT NOT NULL,
		description     TEXT DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'backlog',
		assigned_agent  TEXT DEFAULT '',
		role            TEXT DEFAULT '',
		priority        TEXT DEFAULT 'medium',
		blocked_reason  TEXT DEFAULT '',
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
	_, err := s.db.Exec(schema)
	return err
}

// CreateTask inserts a new task and returns it with the generated ID.
func (s *Store) CreateTask(title, description, priority string, parentID *int64) (*Task, error) {
	now := time.Now().UTC()
	if priority == "" {
		priority = "medium"
	}

	res, err := s.db.Exec(
		`INSERT INTO tasks (title, description, status, priority, parent_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		title, description, string(StatusBacklog), priority, parentID, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}

	id, _ := res.LastInsertId()

	// Log creation event.
	s.addEvent(id, "", "created", fmt.Sprintf("Task created: %s", title))

	return &Task{
		ID:          id,
		ParentID:    parentID,
		Title:       title,
		Description: description,
		Status:      StatusBacklog,
		Priority:    priority,
		CreatedAt:   now,
		UpdatedAt:   now,
	}, nil
}

// GetTask returns a single task by ID.
func (s *Store) GetTask(id int64) (*Task, error) {
	row := s.db.QueryRow(
		`SELECT id, parent_id, title, description, status, assigned_agent, role, priority, blocked_reason, created_at, updated_at
		 FROM tasks WHERE id = ?`, id,
	)
	return scanTask(row)
}

// ListTasks returns all tasks, optionally filtered by status.
func (s *Store) ListTasks(status string) ([]Task, error) {
	query := `SELECT id, parent_id, title, description, status, assigned_agent, role, priority, blocked_reason, created_at, updated_at FROM tasks`
	var args []any
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY id`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
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
	s.addEvent(id, "", "status_changed", fmt.Sprintf("Status changed to %s", status))
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
	s.addEvent(id, agent, "assigned", fmt.Sprintf("Assigned to %s (role: %s)", agent, role))
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
	s.addEvent(id, "", "blocked", reason)
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
	s.addEvent(id, "user", "unblocked", fmt.Sprintf("User answered: %s", answer))
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
	s.addEvent(taskID, reviewerAgent, "reviewed", fmt.Sprintf("Verdict: %s", verdict))
	return nil
}

func (s *Store) addEvent(taskID int64, agent, eventType, content string) {
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
		&t.ID, &parentID, &t.Title, &t.Description, &t.Status,
		&t.AssignedAgent, &t.Role, &t.Priority, &t.BlockedReason,
		&t.CreatedAt, &t.UpdatedAt,
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
		&t.ID, &parentID, &t.Title, &t.Description, &t.Status,
		&t.AssignedAgent, &t.Role, &t.Priority, &t.BlockedReason,
		&t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan task: %w", err)
	}
	if parentID.Valid {
		t.ParentID = &parentID.Int64
	}
	return &t, nil
}
