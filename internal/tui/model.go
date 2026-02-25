package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/store"
)

// view represents which screen/mode the TUI is in.
type view int

const (
	viewBoard  view = iota // Kanban board (main)
	viewDetail             // Task detail panel
	viewCreate             // Create new task
	viewAnswer             // Answer a blocker
)

// column indices for navigation
const (
	colBacklog    = 0
	colInProgress = 1
	colBlocked    = 2
	colReview     = 3
	colDone       = 4
	numColumns    = 5
)

var columnStatuses = [numColumns]store.TaskStatus{
	store.StatusBacklog,
	store.StatusInProgress,
	store.StatusBlocked,
	store.StatusReview,
	store.StatusDone,
}

var columnLabels = [numColumns]string{
	"BACKLOG",
	"IN PROGRESS",
	"BLOCKED",
	"REVIEW",
	"DONE",
}

// Model is the top-level bubbletea model.
type Model struct {
	store  *store.Store
	width  int
	height int

	// Current view.
	currentView view

	// Board state.
	columns   [numColumns][]store.Task
	cursorCol int
	cursorRow int

	// All tasks cache.
	tasks []store.Task

	// Text inputs for create/answer dialogs.
	titleInput     textinput.Model
	descInput      textinput.Model
	answerInput    textinput.Model
	inputFocused   int // which input is focused in create mode (0=title, 1=desc)
	createPriority string

	// Selected task for detail view.
	selectedTask *store.Task
	taskEvents   []store.Event

	// Status message at the bottom.
	statusMsg string

	// Quitting flag.
	quitting bool
}

// New creates a new TUI model.
func New(s *store.Store) Model {
	ti := textinput.New()
	ti.Placeholder = "Task title..."
	ti.CharLimit = 120
	ti.Width = 50

	di := textinput.New()
	di.Placeholder = "Description (optional)..."
	di.CharLimit = 500
	di.Width = 50

	ai := textinput.New()
	ai.Placeholder = "Your answer..."
	ai.CharLimit = 500
	ai.Width = 50

	return Model{
		store:          s,
		currentView:    viewBoard,
		cursorCol:      0,
		cursorRow:      0,
		titleInput:     ti,
		descInput:      di,
		answerInput:    ai,
		createPriority: "medium",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return m.refreshTasks()
}

type tasksRefreshedMsg struct {
	tasks []store.Task
}

type taskEventsMsg struct {
	task   *store.Task
	events []store.Event
}

type statusMsgMsg string

func (m Model) refreshTasks() tea.Cmd {
	return func() tea.Msg {
		tasks, _ := m.store.ListTasks("")
		return tasksRefreshedMsg{tasks: tasks}
	}
}

func (m Model) loadTaskDetail(id int64) tea.Cmd {
	return func() tea.Msg {
		task, err := m.store.GetTask(id)
		if err != nil {
			return statusMsgMsg("Error loading task")
		}
		events, _ := m.store.GetEvents(id)
		return taskEventsMsg{task: task, events: events}
	}
}

func (m *Model) rebuildColumns() {
	for i := range m.columns {
		m.columns[i] = nil
	}
	for _, t := range m.tasks {
		for i, status := range columnStatuses {
			if t.Status == status {
				m.columns[i] = append(m.columns[i], t)
				break
			}
		}
	}
	// Clamp cursor.
	m.clampCursor()
}

func (m *Model) clampCursor() {
	if m.cursorCol < 0 {
		m.cursorCol = 0
	}
	if m.cursorCol >= numColumns {
		m.cursorCol = numColumns - 1
	}
	col := m.columns[m.cursorCol]
	if m.cursorRow >= len(col) {
		m.cursorRow = len(col) - 1
	}
	if m.cursorRow < 0 {
		m.cursorRow = 0
	}
}

func (m *Model) selectedTaskFromBoard() *store.Task {
	col := m.columns[m.cursorCol]
	if m.cursorRow < len(col) {
		t := col[m.cursorRow]
		return &t
	}
	return nil
}
