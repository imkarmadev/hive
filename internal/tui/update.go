package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/store"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tasksRefreshedMsg:
		m.tasks = msg.tasks
		m.rebuildColumns()
		return m, nil

	case taskEventsMsg:
		m.selectedTask = msg.task
		m.taskEvents = msg.events
		m.currentView = viewDetail
		return m, nil

	case statusMsgMsg:
		m.statusMsg = string(msg)
		return m, nil
	}

	// Pass to text inputs if in dialog mode.
	if m.currentView == viewCreate {
		return m.updateCreateDialog(msg)
	}
	if m.currentView == viewAnswer {
		return m.updateAnswerDialog(msg)
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Global keys.
	switch msg.String() {
	case "q", "ctrl+c":
		if m.currentView == viewBoard {
			m.quitting = true
			return m, tea.Quit
		}
		// From any sub-view, go back to board.
		m.currentView = viewBoard
		m.statusMsg = ""
		return m, m.refreshTasks()
	case "esc":
		if m.currentView != viewBoard {
			m.currentView = viewBoard
			m.statusMsg = ""
			return m, m.refreshTasks()
		}
	}

	switch m.currentView {
	case viewBoard:
		return m.handleBoardKey(msg)
	case viewDetail:
		return m.handleDetailKey(msg)
	case viewCreate:
		return m.handleCreateKey(msg)
	case viewAnswer:
		return m.handleAnswerKey(msg)
	}

	return m, nil
}

func (m Model) handleBoardKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// Navigation.
	case "h", "left":
		m.cursorCol--
		m.clampCursor()
	case "l", "right":
		m.cursorCol++
		m.clampCursor()
	case "j", "down":
		m.cursorRow++
		m.clampCursor()
	case "k", "up":
		m.cursorRow--
		m.clampCursor()

	// Actions.
	case "enter":
		if t := m.selectedTaskFromBoard(); t != nil {
			return m, m.loadTaskDetail(t.ID)
		}

	case "n", "c":
		// New task.
		m.currentView = viewCreate
		m.titleInput.Reset()
		m.descInput.Reset()
		m.titleInput.Focus()
		m.inputFocused = 0
		m.createPriority = "medium"
		m.statusMsg = ""
		return m, textinput.Blink

	case "a":
		// Answer blocker.
		if t := m.selectedTaskFromBoard(); t != nil && t.Status == store.StatusBlocked {
			m.selectedTask = t
			m.currentView = viewAnswer
			m.answerInput.Reset()
			m.answerInput.Focus()
			m.statusMsg = ""
			return m, textinput.Blink
		}

	case "d":
		// Mark as done.
		if t := m.selectedTaskFromBoard(); t != nil {
			m.store.UpdateTaskStatus(t.ID, store.StatusDone)
			m.statusMsg = "Task marked as done"
			return m, m.refreshTasks()
		}

	case "s":
		// Start (move to in_progress).
		if t := m.selectedTaskFromBoard(); t != nil && t.Status == store.StatusBacklog {
			m.store.UpdateTaskStatus(t.ID, store.StatusInProgress)
			m.statusMsg = "Task started"
			return m, m.refreshTasks()
		}

	case "r":
		// Move to review.
		if t := m.selectedTaskFromBoard(); t != nil {
			m.store.UpdateTaskStatus(t.ID, store.StatusReview)
			m.statusMsg = "Task moved to review"
			return m, m.refreshTasks()
		}

	case "b":
		// Move back to backlog.
		if t := m.selectedTaskFromBoard(); t != nil {
			m.store.UpdateTaskStatus(t.ID, store.StatusBacklog)
			m.statusMsg = "Task moved to backlog"
			return m, m.refreshTasks()
		}

	case "R":
		// Refresh.
		return m, m.refreshTasks()
	}

	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "backspace":
		m.currentView = viewBoard
		return m, m.refreshTasks()
	}
	return m, nil
}

func (m Model) handleCreateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.currentView = viewBoard
		return m, nil

	case "tab":
		if m.inputFocused == 0 {
			m.titleInput.Blur()
			m.descInput.Focus()
			m.inputFocused = 1
		} else {
			m.descInput.Blur()
			m.titleInput.Focus()
			m.inputFocused = 0
		}
		return m, textinput.Blink

	case "ctrl+p":
		// Cycle priority.
		switch m.createPriority {
		case "low":
			m.createPriority = "medium"
		case "medium":
			m.createPriority = "high"
		case "high":
			m.createPriority = "low"
		}
		return m, nil

	case "enter":
		title := m.titleInput.Value()
		if title == "" {
			m.statusMsg = "Title cannot be empty"
			return m, nil
		}
		desc := m.descInput.Value()
		task, err := m.store.CreateTask(title, desc, m.createPriority, nil)
		if err != nil {
			m.statusMsg = "Error: " + err.Error()
			return m, nil
		}
		m.statusMsg = "Created task #" + itoa(int(task.ID))
		m.currentView = viewBoard
		return m, m.refreshTasks()
	}

	return m.updateCreateDialog(msg)
}

func (m Model) handleAnswerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.currentView = viewBoard
		return m, nil

	case "enter":
		answer := m.answerInput.Value()
		if answer == "" {
			m.statusMsg = "Answer cannot be empty"
			return m, nil
		}
		if m.selectedTask != nil {
			m.store.UnblockTask(m.selectedTask.ID, answer)
			m.statusMsg = "Unblocked task #" + itoa(int(m.selectedTask.ID))
		}
		m.currentView = viewBoard
		return m, m.refreshTasks()
	}

	return m.updateAnswerDialog(msg)
}

func (m Model) updateCreateDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.inputFocused == 0 {
		m.titleInput, cmd = m.titleInput.Update(msg)
	} else {
		m.descInput, cmd = m.descInput.Update(msg)
	}
	return m, cmd
}

func (m Model) updateAnswerDialog(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.answerInput, cmd = m.answerInput.Update(msg)
	return m, cmd
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
