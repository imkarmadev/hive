package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If popup is active, handle popup keys first.
		if m.popup != popupNone {
			return m.handlePopupKey(msg)
		}
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Update grid columns based on width.
		m.gridCols = m.width / 46
		if m.gridCols < 1 {
			m.gridCols = 1
		}
		if m.gridCols > 4 {
			m.gridCols = 4
		}
		// Update viewport sizes.
		vw := m.width - 4
		vh := m.height - 6
		if vw < 20 {
			vw = 20
		}
		if vh < 6 {
			vh = 6
		}
		m.diffViewport.Width = vw
		m.diffViewport.Height = vh
		m.historyViewport.Width = vw
		m.historyViewport.Height = vh
		return m, nil

	case epicsLoadedMsg:
		if msg.err != nil {
			m.setStatus("Failed to load epics: " + msg.err.Error())
			m.refreshing = false
			return m, nil
		}
		m.epics = msg.epics
		m.clampGridCursor()
		// If we're in epic detail, refresh it too.
		if m.screen == screenEpic && m.epicDetail != nil {
			for i := range m.epics {
				if m.epics[i].Epic.ID == m.epicDetail.Epic.ID {
					m.epicDetail = &m.epics[i]
					m.clampTaskCursor()
					break
				}
			}
		}
		m.refreshing = false
		return m, nil

	case createFixDoneMsg:
		if msg.err != nil {
			m.setStatus("Failed to create fix task: " + msg.err.Error())
			return m, nil
		}
		m.setStatus("Created fix task for E#" + itoa(int(msg.epicID)))
		return m, m.loadEpics()

	case diffLoadedMsg:
		m.diffContent = msg.content
		m.diffEpicID = msg.epicID
		m.diffViewport.SetContent(msg.content)
		m.diffViewport.GotoTop()
		m.screen = screenDiff
		return m, nil

	case historyLoadedMsg:
		m.historyContent = msg.content
		m.historyViewport.SetContent(msg.content)
		m.historyViewport.GotoTop()
		m.screen = screenHistory
		return m, nil

	case acceptDoneMsg:
		if msg.err != nil {
			m.setStatus("Accept failed: " + msg.err.Error())
		} else {
			m.setStatus("Epic accepted and merged!")
		}
		m.popup = popupNone
		m.screen = screenGrid
		return m, m.loadEpics()

	case rejectDoneMsg:
		if msg.err != nil {
			m.setStatus("Reject failed: " + msg.err.Error())
		} else {
			m.setStatus("Epic rejected.")
		}
		m.popup = popupNone
		m.screen = screenGrid
		return m, m.loadEpics()

	case statusClearMsg:
		m.statusMsg = ""
		return m, nil

	case tickMsg:
		// Auto-refresh every tick.
		var cmds []tea.Cmd
		cmds = append(cmds, tickCmd())
		// Clear old status messages.
		if m.statusMsg != "" && time.Since(m.statusTime) > 5*time.Second {
			m.statusMsg = ""
		}
		// Refresh data if not already loading.
		if !m.refreshing {
			m.refreshing = true
			cmds = append(cmds, m.loadEpics())
		}
		return m, tea.Batch(cmds...)
	}

	// Forward to viewport if in diff/history view.
	if m.screen == screenDiff {
		var cmd tea.Cmd
		m.diffViewport, cmd = m.diffViewport.Update(msg)
		return m, cmd
	}
	if m.screen == screenHistory {
		var cmd tea.Cmd
		m.historyViewport, cmd = m.historyViewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		if m.screen == screenGrid {
			m.quitting = true
			return m, tea.Quit
		}
		// From sub-screens, go back.
		return m.goBack()

	case "esc":
		return m.goBack()
	}

	switch m.screen {
	case screenGrid:
		return m.handleGridKey(msg)
	case screenEpic:
		return m.handleEpicKey(msg)
	case screenDiff:
		return m.handleDiffKey(msg)
	case screenHistory:
		return m.handleHistoryKey(msg)
	}

	return m, nil
}

func (m Model) goBack() (tea.Model, tea.Cmd) {
	switch m.screen {
	case screenEpic:
		m.screen = screenGrid
		m.epicDetail = nil
		return m, m.loadEpics()
	case screenDiff, screenHistory:
		// Go back to epic detail if we drilled down, or grid.
		if m.epicDetail != nil {
			m.screen = screenEpic
		} else {
			m.screen = screenGrid
		}
		return m, nil
	default:
		return m, nil
	}
}

// --- Grid screen keys ---

func (m Model) handleGridKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	// Navigation.
	case "j", "down":
		m.cursor += m.gridCols
		m.clampGridCursor()
	case "k", "up":
		m.cursor -= m.gridCols
		m.clampGridCursor()
	case "h", "left":
		m.cursor--
		m.clampGridCursor()
	case "l", "right":
		m.cursor++
		m.clampGridCursor()

	// Drill-down into epic.
	case "enter", " ":
		if e := m.selectedEpic(); e != nil {
			m.epicDetail = e
			m.taskCursor = 0
			m.screen = screenEpic
			return m, nil
		}

	// Run auto on selected epic.
	case "a":
		if e := m.selectedEpic(); e != nil {
			m.setStatus("Run in terminal: hive auto " + itoa(int(e.Epic.ID)) + " --skip-plan")
		}

	// Resolve blocker.
	case "r":
		if e := m.selectedEpic(); e != nil && e.HasBlocker {
			// Find the blocked task.
			for _, t := range e.Tasks {
				if t.Status == store.StatusBlocked {
					m.popupTaskID = t.ID
					m.popup = popupResolve
					m.textInput.Reset()
					m.textInput.Placeholder = "Your answer..."
					m.textInput.Focus()
					return m, textinput.Blink
				}
			}
			// Epic itself might be blocked.
			if e.Epic.Status == store.StatusBlocked {
				m.popupTaskID = e.Epic.ID
				m.popup = popupResolve
				m.textInput.Reset()
				m.textInput.Placeholder = "Your answer..."
				m.textInput.Focus()
				return m, textinput.Blink
			}
		}

	// Diff view.
	case "d":
		if e := m.selectedEpic(); e != nil {
			m.epicDetail = e
			return m, m.loadDiff(e.Epic.ID)
		}

	// Accept epic.
	case "y":
		if e := m.selectedEpic(); e != nil {
			m.popupEpicID = e.Epic.ID
			m.popup = popupConfirmAccept
			return m, nil
		}

	// Reject epic.
	case "n":
		if e := m.selectedEpic(); e != nil {
			m.popupEpicID = e.Epic.ID
			m.popup = popupReject
			m.textInput.Reset()
			m.textInput.Placeholder = "Reason (optional, press enter to skip)..."
			m.textInput.Focus()
			return m, textinput.Blink
		}

	// History.
	case "H":
		if e := m.selectedEpic(); e != nil {
			m.epicDetail = e
			return m, m.loadHistory(e.Epic.ID)
		}

	// Create new epic.
	case "c", "ctrl+n":
		m.popup = popupCreateEpic
		m.textInput.Reset()
		m.textInput.Placeholder = "Epic title..."
		m.textInput.Focus()
		m.textInput2.Reset()
		m.textInput2.Placeholder = "Description (optional)..."
		m.inputFocused = 0
		m.createPriority = "high"
		return m, textinput.Blink

	// Refresh.
	case "R":
		return m, m.loadEpics()
	}

	return m, nil
}

// --- Epic drill-down keys ---

func (m Model) handleEpicKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.epicDetail == nil {
		m.screen = screenGrid
		return m, nil
	}

	switch msg.String() {
	case "j", "down":
		m.taskCursor++
		m.clampTaskCursor()
	case "k", "up":
		m.taskCursor--
		m.clampTaskCursor()

	// Resolve blocker on selected task.
	case "r":
		if t := m.selectedTask(); t != nil && t.Status == store.StatusBlocked {
			m.popupTaskID = t.ID
			m.popup = popupResolve
			m.textInput.Reset()
			m.textInput.Placeholder = "Your answer..."
			m.textInput.Focus()
			return m, textinput.Blink
		}

	// Diff for the whole epic.
	case "d":
		return m, m.loadDiff(m.epicDetail.Epic.ID)

	// History.
	case "H":
		return m, m.loadHistory(m.epicDetail.Epic.ID)

	// Accept the epic.
	case "y":
		m.popupEpicID = m.epicDetail.Epic.ID
		m.popup = popupConfirmAccept
		return m, nil

	// Reject the epic.
	case "n":
		m.popupEpicID = m.epicDetail.Epic.ID
		m.popup = popupReject
		m.textInput.Reset()
		m.textInput.Placeholder = "Reason (optional)..."
		m.textInput.Focus()
		return m, textinput.Blink

	// Run auto on this epic.
	case "a":
		m.setStatus("Run in terminal: hive auto " + itoa(int(m.epicDetail.Epic.ID)) + " --skip-plan")

	case "esc", "backspace":
		m.screen = screenGrid
		m.epicDetail = nil
		return m, m.loadEpics()
	}

	return m, nil
}

// --- Diff view keys ---

func (m Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		// Accept from diff view.
		m.popupEpicID = m.diffEpicID
		m.popup = popupConfirmAccept
		return m, nil

	case "n":
		// Reject from diff view.
		m.popupEpicID = m.diffEpicID
		m.popup = popupReject
		m.textInput.Reset()
		m.textInput.Placeholder = "Reason (optional)..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "e":
		// Request changes.
		m.popupEpicID = m.diffEpicID
		m.popup = popupRequestFix
		m.textInput.Reset()
		m.textInput.Placeholder = "What needs fixing..."
		m.textInput.Focus()
		return m, textinput.Blink

	case "esc", "q", "backspace":
		return m.goBack()
	}

	// Forward to viewport for scrolling.
	var cmd tea.Cmd
	m.diffViewport, cmd = m.diffViewport.Update(msg)
	return m, cmd
}

// --- History view keys ---

func (m Model) handleHistoryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "backspace":
		return m.goBack()
	}

	var cmd tea.Cmd
	m.historyViewport, cmd = m.historyViewport.Update(msg)
	return m, cmd
}

// --- Popup keys ---

func (m Model) handlePopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.popup {
	case popupResolve:
		return m.handleResolvePopup(msg)
	case popupReject:
		return m.handleRejectPopup(msg)
	case popupRequestFix:
		return m.handleRequestFixPopup(msg)
	case popupCreateEpic:
		return m.handleCreateEpicPopup(msg)
	case popupConfirmAccept:
		return m.handleConfirmAcceptPopup(msg)
	}
	return m, nil
}

func (m Model) handleResolvePopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.popup = popupNone
		return m, nil
	case "enter":
		answer := m.textInput.Value()
		if answer == "" {
			m.setStatus("Answer cannot be empty")
			return m, nil
		}
		m.store.UnblockTask(m.popupTaskID, answer)
		m.popup = popupNone
		m.setStatus("Resolved blocker on #" + itoa(int(m.popupTaskID)))
		return m, m.loadEpics()
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleRejectPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.popup = popupNone
		return m, nil
	case "enter":
		reason := m.textInput.Value()
		m.popup = popupNone
		return m, m.doReject(m.popupEpicID, reason)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleRequestFixPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.popup = popupNone
		return m, nil
	case "enter":
		desc := m.textInput.Value()
		if desc == "" {
			m.setStatus("Description cannot be empty")
			return m, nil
		}
		m.popup = popupNone
		return m, m.doCreateFixTask(m.popupEpicID, desc)
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m Model) handleCreateEpicPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.popup = popupNone
		return m, nil
	case "tab":
		if m.inputFocused == 0 {
			m.textInput.Blur()
			m.textInput2.Focus()
			m.inputFocused = 1
		} else {
			m.textInput2.Blur()
			m.textInput.Focus()
			m.inputFocused = 0
		}
		return m, textinput.Blink
	case "ctrl+p":
		switch m.createPriority {
		case "high":
			m.createPriority = "medium"
		case "medium":
			m.createPriority = "low"
		case "low":
			m.createPriority = "high"
		}
		return m, nil
	case "enter":
		title := m.textInput.Value()
		if title == "" {
			m.setStatus("Title cannot be empty")
			return m, nil
		}
		desc := m.textInput2.Value()
		epic, err := m.store.CreateEpic(title, desc, m.createPriority)
		if err != nil {
			m.setStatus("Error: " + err.Error())
			return m, nil
		}

		// Create git safety branch.
		safety := git.New(m.workDir)
		if safety.IsGitRepo() {
			branch := git.BranchName(epic.ID)
			if err := safety.CreateBranch(branch); err == nil {
				m.store.SetGitBranch(epic.ID, branch)
				// Switch back to the original branch so we don't stay on the epic branch.
				baseBranch, _ := safety.BaseBranch()
				safety.Checkout(baseBranch)
			}
		}

		m.popup = popupNone
		m.setStatus("Created epic E#" + itoa(int(epic.ID)) + ": " + title)
		return m, m.loadEpics()
	}

	// Forward to the active text input.
	var cmd tea.Cmd
	if m.inputFocused == 0 {
		m.textInput, cmd = m.textInput.Update(msg)
	} else {
		m.textInput2, cmd = m.textInput2.Update(msg)
	}
	return m, cmd
}

func (m Model) handleConfirmAcceptPopup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		m.popup = popupNone
		return m, m.doAccept(m.popupEpicID)
	case "n", "esc":
		m.popup = popupNone
		return m, nil
	}
	return m, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
