package tui

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
)

// screen represents which view the TUI is showing.
type screen int

const (
	screenGrid    screen = iota // Epic card grid (main)
	screenEpic                  // Drill-down into a single epic
	screenDiff                  // Diff viewer for an epic
	screenHistory               // Epic history / timeline
)

// popup represents an active overlay dialog.
type popup int

const (
	popupNone          popup = iota
	popupResolve             // Answer a blocker
	popupReject              // Reject with optional reason
	popupRequestFix          // Request changes (creates new task)
	popupCreateEpic          // Create new epic
	popupConfirmAccept       // Confirm accept/merge
)

// epicPhase describes the high-level stage of an epic pipeline.
type epicPhase int

const (
	phasePlan      epicPhase = 0
	phaseArchitect epicPhase = 1
	phaseCode      epicPhase = 2
	phaseReview    epicPhase = 3
	phaseAccept    epicPhase = 4
	numPhases                = 5
)

var phaseLabels = [numPhases]string{"plan", "arch", "code", "review", "accept"}

// epicCard holds pre-computed display data for one epic on the grid.
type epicCard struct {
	Epic       store.Task
	Tasks      []store.Task
	Phase      epicPhase       // Current overall phase
	PhasesDone [numPhases]bool // Which phases are complete
	HasBlocker bool
	BlockerMsg string
	LogLine    string // Most recent log line
	Events     []store.Event
}

// Model is the top-level bubbletea model for the hive TUI.
type Model struct {
	store   *store.Store
	workDir string
	width   int
	height  int

	// Current screen + popup overlay.
	screen screen
	popup  popup

	// Grid state (main screen).
	epics    []epicCard
	cursor   int // Selected epic index
	gridCols int // Number of columns in the grid

	// Epic drill-down state.
	epicDetail *epicCard
	taskCursor int // Selected task index within the epic

	// Diff viewer.
	diffViewport viewport.Model
	diffContent  string
	diffEpicID   int64

	// History viewer.
	historyViewport viewport.Model
	historyContent  string

	// Text inputs for popups.
	textInput    textinput.Model
	textInput2   textinput.Model // For description fields
	inputFocused int             // 0=first, 1=second

	// Popup context.
	popupTaskID    int64 // Which task the popup is about
	popupEpicID    int64 // Which epic the popup is about
	createPriority string

	// Status bar message.
	statusMsg  string
	statusTime time.Time

	// Auto-refresh ticker.
	refreshing bool

	quitting bool
}

// New creates a new TUI model.
func New(s *store.Store, workDir string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type here..."
	ti.CharLimit = 500
	ti.Width = 50

	ti2 := textinput.New()
	ti2.Placeholder = "Description (optional)..."
	ti2.CharLimit = 500
	ti2.Width = 50

	vp := viewport.New(80, 20)
	hp := viewport.New(80, 20)

	return Model{
		store:           s,
		workDir:         workDir,
		screen:          screenGrid,
		popup:           popupNone,
		gridCols:        2,
		textInput:       ti,
		textInput2:      ti2,
		diffViewport:    vp,
		historyViewport: hp,
		createPriority:  "high",
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.loadEpics(), tickCmd())
}

// --- Messages ---

type epicsLoadedMsg struct {
	epics []epicCard
	err   error
}

type statusClearMsg struct{}

type tickMsg time.Time

type diffLoadedMsg struct {
	epicID  int64
	content string
}

type historyLoadedMsg struct {
	epicID  int64
	content string
}

type autoStartedMsg struct {
	epicID int64
	err    error
}

type acceptDoneMsg struct {
	epicID int64
	err    error
}

type rejectDoneMsg struct {
	epicID int64
	reason string
	err    error
}

type createFixDoneMsg struct {
	epicID int64
	err    error
}

// --- Commands ---

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadEpics() tea.Cmd {
	return func() tea.Msg {
		epics, err := m.store.ListEpics("")
		if err != nil {
			return epicsLoadedMsg{err: err}
		}

		var cards []epicCard
		for _, e := range epics {
			card := epicCard{Epic: e}

			// Load tasks under this epic.
			tasks, _ := m.store.ListTasksByEpic(e.ID)
			card.Tasks = tasks

			// Check if architect has run on any task.
			hasArch := false
			for _, t := range tasks {
				tevts, _ := m.store.GetEvents(t.ID)
				for _, ev := range tevts {
					if ev.Type == "architect_spec" {
						hasArch = true
						break
					}
				}
				if hasArch {
					break
				}
			}

			// Compute phase.
			card.Phase, card.PhasesDone = computePhase(e, tasks, hasArch)

			// Check for blockers.
			for _, t := range tasks {
				if t.Status == store.StatusBlocked {
					card.HasBlocker = true
					card.BlockerMsg = fmt.Sprintf("#%d: %s", t.ID, t.BlockedReason)
					break
				}
			}
			if e.Status == store.StatusBlocked {
				card.HasBlocker = true
				card.BlockerMsg = e.BlockedReason
			}

			// Load and sort events from epic + tasks for better log/history.
			card.Events = m.eventsForEpic(e.ID, tasks)

			// Pick the most recent event for the log line.
			if len(card.Events) > 0 {
				latest := card.Events[len(card.Events)-1]
				card.LogLine = formatLogLine(latest)
			}

			cards = append(cards, card)
		}

		return epicsLoadedMsg{epics: cards}
	}
}

func (m Model) loadDiff(epicID int64) tea.Cmd {
	return func() tea.Msg {
		epic, err := m.store.GetTask(epicID)
		if err != nil || epic.GitBranch == "" {
			return diffLoadedMsg{epicID: epicID, content: "No git branch for this epic.\n\nRun pipeline again on this epic to create safety branch:\n  hive auto " + fmt.Sprintf("%d", epicID)}
		}

		safety := git.New(m.workDir)
		if !safety.IsGitRepo() {
			return diffLoadedMsg{epicID: epicID, content: "Not a git repository."}
		}

		baseBranch, err := safety.BaseBranch()
		if err != nil {
			return diffLoadedMsg{epicID: epicID, content: "Cannot determine base branch."}
		}

		// Get stat summary + full diff.
		stat, _ := safety.DiffStat(baseBranch, epic.GitBranch)
		diff, err := safety.Diff(baseBranch, epic.GitBranch)
		if err != nil {
			return diffLoadedMsg{epicID: epicID, content: "Error getting diff: " + err.Error()}
		}

		if diff == "" {
			return diffLoadedMsg{epicID: epicID, content: "No changes on branch " + epic.GitBranch}
		}

		content := "Branch: " + epic.GitBranch + "\n\n"
		if stat != "" {
			content += stat + "\n"
		}
		content += diff

		return diffLoadedMsg{epicID: epicID, content: content}
	}
}

func (m Model) loadHistory(epicID int64) tea.Cmd {
	return func() tea.Msg {
		epic, err := m.store.GetTask(epicID)
		if err != nil {
			return historyLoadedMsg{epicID: epicID, content: "Epic not found."}
		}

		var content string
		content += fmt.Sprintf("Epic #%d: %s\n", epic.ID, epic.Title)
		content += fmt.Sprintf("Created: %s\n", epic.CreatedAt.Local().Format("2006-01-02 15:04"))
		content += fmt.Sprintf("Status:  %s\n", epic.Status)
		if epic.GitBranch != "" {
			content += fmt.Sprintf("Branch:  %s\n", epic.GitBranch)
		}
		content += "\n"

		// Git commits if available.
		if epic.GitBranch != "" {
			safety := git.New(m.workDir)
			if safety.IsGitRepo() {
				baseBranch, _ := safety.BaseBranch()
				commits, err := safety.LogCommits(baseBranch, epic.GitBranch)
				if err == nil && commits != "" {
					content += "Commits:\n"
					content += commits + "\n\n"
				}
			}
		}

		tasks, _ := m.store.ListTasksByEpic(epic.ID)
		events := m.eventsForEpic(epic.ID, tasks)

		content += "Timeline:\n"
		for _, e := range events {
			agent := ""
			if e.Agent != "" {
				agent = "[" + e.Agent + "] "
			}
			content += fmt.Sprintf("  %s %s%s: %s\n",
				e.Timestamp.Local().Format("15:04:05"),
				agent, e.Type, truncate(e.Content, 80))
		}

		return historyLoadedMsg{epicID: epicID, content: content}
	}
}

func (m Model) eventsForEpic(epicID int64, tasks []store.Task) []store.Event {
	events, _ := m.store.GetEvents(epicID)
	for _, t := range tasks {
		tevents, _ := m.store.GetEvents(t.ID)
		events = append(events, tevents...)
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	return events
}

func (m Model) doAccept(epicID int64) tea.Cmd {
	return func() tea.Msg {
		epic, err := m.store.GetTask(epicID)
		if err != nil {
			return acceptDoneMsg{epicID: epicID, err: err}
		}

		// Guard: all tasks must be done or cancelled.
		tasks, _ := m.store.ListTasksByEpic(epicID)
		for _, t := range tasks {
			if t.Status != store.StatusDone && t.Status != store.StatusCancelled {
				return acceptDoneMsg{epicID: epicID, err: fmt.Errorf("task #%d is still %s — finish all tasks first", t.ID, t.Status)}
			}
		}

		safety := git.New(m.workDir)
		if !safety.IsGitRepo() || epic.GitBranch == "" {
			// No git — just mark done.
			m.store.UpdateTaskStatus(epicID, store.StatusDone)
			return acceptDoneMsg{epicID: epicID}
		}

		baseBranch, err := safety.BaseBranch()
		if err != nil {
			return acceptDoneMsg{epicID: epicID, err: err}
		}

		if err := safety.MergeBranch(baseBranch, epic.GitBranch); err != nil {
			return acceptDoneMsg{epicID: epicID, err: err}
		}

		// Cleanup branch.
		safety.DeleteBranch(epic.GitBranch, false)
		m.store.UpdateTaskStatus(epicID, store.StatusDone)
		m.store.AddEvent(epicID, "user", "accepted", "Epic accepted and merged")

		return acceptDoneMsg{epicID: epicID}
	}
}

func (m Model) doReject(epicID int64, reason string) tea.Cmd {
	return func() tea.Msg {
		epic, err := m.store.GetTask(epicID)
		if err != nil {
			return rejectDoneMsg{epicID: epicID, err: err}
		}

		safety := git.New(m.workDir)
		if safety.IsGitRepo() && epic.GitBranch != "" {
			baseBranch, _ := safety.BaseBranch()
			if err := safety.RejectBranch(baseBranch, epic.GitBranch); err != nil {
				return rejectDoneMsg{epicID: epicID, err: err}
			}
		}

		m.store.UpdateTaskStatus(epicID, store.StatusFailed)
		eventContent := "Epic rejected"
		if reason != "" {
			eventContent += ": " + reason
		}
		m.store.AddEvent(epicID, "user", "rejected", eventContent)

		return rejectDoneMsg{epicID: epicID, reason: reason}
	}
}

func (m Model) doCreateFixTask(epicID int64, description string) tea.Cmd {
	return func() tea.Msg {
		_, err := m.store.CreateTask(description, "", "high", &epicID)
		if err != nil {
			return createFixDoneMsg{epicID: epicID, err: err}
		}
		m.store.AddEvent(epicID, "user", "requested_changes", description)
		return createFixDoneMsg{epicID: epicID}
	}
}

// --- Helpers ---

func computePhase(epic store.Task, tasks []store.Task, hasArchitectSpec bool) (epicPhase, [numPhases]bool) {
	var done [numPhases]bool

	if len(tasks) == 0 {
		// No tasks yet — still in plan phase.
		if epic.Status == store.StatusDone {
			done = [numPhases]bool{true, true, true, true, true}
			return phaseAccept, done
		}
		return phasePlan, done
	}

	// Plan is done if we have tasks.
	done[phasePlan] = true

	if hasArchitectSpec {
		done[phaseArchitect] = true
	}

	totalTasks := len(tasks)
	doneCount := 0
	reviewCount := 0
	inProgressCount := 0

	for _, t := range tasks {
		switch t.Status {
		case store.StatusDone:
			doneCount++
		case store.StatusReview:
			reviewCount++
		case store.StatusInProgress:
			inProgressCount++
		}
	}

	// Determine current phase.
	if doneCount == totalTasks {
		done[phaseArchitect] = true
		done[phaseCode] = true
		done[phaseReview] = true
		if epic.Status == store.StatusDone {
			done[phaseAccept] = true
			return phaseAccept, done
		}
		return phaseAccept, done
	}

	if reviewCount > 0 || doneCount > 0 {
		done[phaseArchitect] = true
		done[phaseCode] = true
		return phaseReview, done
	}

	if inProgressCount > 0 {
		done[phaseArchitect] = true
		return phaseCode, done
	}

	// Tasks exist but none started yet.
	if !hasArchitectSpec {
		return phaseArchitect, done
	}

	done[phaseArchitect] = true
	return phaseCode, done
}

func formatLogLine(e store.Event) string {
	agent := ""
	if e.Agent != "" {
		agent = e.Agent + ": "
	}
	content := truncate(e.Content, 50)
	return agent + content
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (m *Model) setStatus(msg string) {
	m.statusMsg = msg
	m.statusTime = time.Now()
}

func (m *Model) selectedEpic() *epicCard {
	if m.cursor >= 0 && m.cursor < len(m.epics) {
		return &m.epics[m.cursor]
	}
	return nil
}

func (m *Model) selectedTask() *store.Task {
	if m.epicDetail == nil {
		return nil
	}
	if m.taskCursor >= 0 && m.taskCursor < len(m.epicDetail.Tasks) {
		return &m.epicDetail.Tasks[m.taskCursor]
	}
	return nil
}

func (m *Model) clampGridCursor() {
	if len(m.epics) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.epics) {
		m.cursor = len(m.epics) - 1
	}
}

func (m *Model) clampTaskCursor() {
	if m.epicDetail == nil || len(m.epicDetail.Tasks) == 0 {
		m.taskCursor = 0
		return
	}
	if m.taskCursor < 0 {
		m.taskCursor = 0
	}
	if m.taskCursor >= len(m.epicDetail.Tasks) {
		m.taskCursor = len(m.epicDetail.Tasks) - 1
	}
}

func getWorkDir() string {
	wd, _ := os.Getwd()
	return wd
}
