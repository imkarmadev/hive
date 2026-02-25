package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imkarma/hive/internal/store"
)

// --- Color palette ---
var (
	clrSubtle    = lipgloss.AdaptiveColor{Light: "#555555", Dark: "#666666"}
	clrHighlight = lipgloss.AdaptiveColor{Light: "#0F766E", Dark: "#2DD4BF"}
	clrGreen     = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	clrYellow    = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F59E0B"}
	clrRed       = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	clrBlue      = lipgloss.AdaptiveColor{Light: "#1D4ED8", Dark: "#60A5FA"}
	clrCyan      = lipgloss.AdaptiveColor{Light: "#0E7490", Dark: "#22D3EE"}
	clrWhite     = lipgloss.AdaptiveColor{Light: "#333333", Dark: "#DDDDDD"}
	clrDim       = lipgloss.AdaptiveColor{Light: "#999999", Dark: "#555555"}
)

// --- Styles ---
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(clrHighlight)
	dimStyle    = lipgloss.NewStyle().Foreground(clrDim)
	subtleStyle = lipgloss.NewStyle().Foreground(clrSubtle)

	epicCardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrSubtle).
			Padding(0, 1).
			Width(42).
			Height(11)

	epicCardSelectedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrHighlight).
				Padding(0, 1).
				Width(42).
				Height(11).
				Bold(true)

	epicCardBlockedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrRed).
				Padding(0, 1).
				Width(42).
				Height(11)

	epicCardDoneStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(clrGreen).
				Padding(0, 1).
				Width(42).
				Height(11)

	popupStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(clrHighlight).
			Padding(1, 2).
			Width(60)

	statusStyle = lipgloss.NewStyle().Foreground(clrGreen).Bold(true)
	errorStyle  = lipgloss.NewStyle().Foreground(clrRed).Bold(true)

	footerKeyStyle  = lipgloss.NewStyle().Bold(true).Foreground(clrHighlight)
	footerDescStyle = lipgloss.NewStyle().Foreground(clrSubtle)
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var content string

	switch m.screen {
	case screenGrid:
		content = m.viewGrid()
	case screenEpic:
		content = m.viewEpicDetail()
	case screenDiff:
		content = m.viewDiff()
	case screenHistory:
		content = m.viewHistory()
	}

	// Overlay popup if active.
	if m.popup != popupNone {
		content = m.overlayPopup(content)
	}

	return content
}

// ════════════════════════════════════════════════
// GRID VIEW — main screen with epic cards
// ════════════════════════════════════════════════

func (m Model) viewGrid() string {
	var b strings.Builder

	// Header.
	count := len(m.epics)
	header := titleStyle.Render("hive board")
	header += dimStyle.Render(fmt.Sprintf(" — %d epics", count))

	rightHelp := footerKeyStyle.Render("c") + footerDescStyle.Render(" new  ") +
		footerKeyStyle.Render("q") + footerDescStyle.Render(" quit")

	headerLine := header
	if m.width > 0 {
		pad := m.width - lipgloss.Width(header) - lipgloss.Width(rightHelp)
		if pad > 0 {
			headerLine = header + strings.Repeat(" ", pad) + rightHelp
		}
	}
	b.WriteString(headerLine + "\n\n")

	if count == 0 {
		b.WriteString(dimStyle.Render("  No epics yet. Press ") +
			footerKeyStyle.Render("c") +
			dimStyle.Render(" to create one.\n"))
		return b.String()
	}

	// Render epic cards in a grid.
	cols := m.gridCols
	if cols < 1 {
		cols = 2
	}

	cardWidth := 42
	if m.width > 0 {
		cardWidth = (m.width - (cols + 1)) / cols
		if cardWidth < 30 {
			cardWidth = 30
		}
		if cardWidth > 50 {
			cardWidth = 50
		}
	}

	for i := 0; i < len(m.epics); i += cols {
		var rowCards []string
		for j := 0; j < cols && i+j < len(m.epics); j++ {
			idx := i + j
			card := m.renderEpicCard(&m.epics[idx], idx == m.cursor, cardWidth)
			rowCards = append(rowCards, card)
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, rowCards...))
		b.WriteString("\n")
	}

	// Status bar.
	if m.statusMsg != "" {
		b.WriteString("\n")
		if strings.HasPrefix(strings.ToLower(m.statusMsg), "failed") || strings.HasPrefix(strings.ToLower(m.statusMsg), "error") {
			b.WriteString(errorStyle.Render("  " + m.statusMsg))
		} else {
			b.WriteString(statusStyle.Render("  " + m.statusMsg))
		}
	}

	// Footer.
	b.WriteString("\n")
	b.WriteString(m.gridFooter())

	return b.String()
}

func (m Model) renderEpicCard(card *epicCard, selected bool, width int) string {
	var content strings.Builder

	// Title line: E#id + status
	idStr := lipgloss.NewStyle().Foreground(clrCyan).Render(fmt.Sprintf("E#%d", card.Epic.ID))
	status := dimStyle.Render(string(card.Epic.Status))
	content.WriteString(idStr + "  " + status + "\n")

	title := lipgloss.NewStyle().Bold(true).Render(truncate(card.Epic.Title, width-6))
	content.WriteString(title + "\n")

	// Short description (always reserve one line).
	if card.Epic.Description != "" {
		desc := truncate(card.Epic.Description, width-6)
		content.WriteString(dimStyle.Render(desc) + "\n")
	} else {
		content.WriteString(dimStyle.Render("No description") + "\n")
	}

	// Pipeline tracker: ● ──── ● ──── ○ ──── ○
	content.WriteString(m.renderPipeline(card) + "\n")

	// Phase labels.
	content.WriteString(m.renderPhaseLabels(card) + "\n")

	done := 0
	for _, t := range card.Tasks {
		if t.Status == store.StatusDone {
			done++
		}
	}
	meta := fmt.Sprintf("Tasks: %d/%d done", done, len(card.Tasks))
	if len(card.Tasks) == 0 {
		meta = "Tasks: not planned yet"
	}
	content.WriteString(dimStyle.Render(meta) + "\n")

	// Log line or status.
	if card.HasBlocker {
		blockerMsg := truncate(card.BlockerMsg, width-6)
		content.WriteString(lipgloss.NewStyle().Foreground(clrRed).Render("⚠ BLOCKED ") + blockerMsg)
	} else if card.Phase == phaseAccept && allTasksDone(card) && card.Epic.Status != store.StatusDone {
		content.WriteString(lipgloss.NewStyle().Foreground(clrGreen).Render("✓ Ready — review & accept"))
	} else if card.Epic.Status == store.StatusDone {
		content.WriteString(lipgloss.NewStyle().Foreground(clrGreen).Render("✓ Accepted"))
	} else if card.LogLine != "" {
		content.WriteString(dimStyle.Render(truncate(card.LogLine, width-6)))
	}

	// Pick card style.
	style := epicCardStyle.Width(width)
	if selected {
		style = epicCardSelectedStyle.Width(width)
	} else if card.HasBlocker {
		style = epicCardBlockedStyle.Width(width)
	} else if card.Epic.Status == store.StatusDone {
		style = epicCardDoneStyle.Width(width)
	}

	return style.Render(content.String())
}

func (m Model) renderPipeline(card *epicCard) string {
	var parts []string

	for i := 0; i < numPhases; i++ {
		var dot string
		if card.PhasesDone[i] {
			// Completed phase — green dot.
			dot = lipgloss.NewStyle().Foreground(clrGreen).Render("●")
		} else if int(card.Phase) == i {
			// Current active phase.
			if card.HasBlocker {
				dot = lipgloss.NewStyle().Foreground(clrRed).Render("●")
			} else {
				dot = lipgloss.NewStyle().Foreground(clrBlue).Render("◉")
			}
		} else {
			// Future phase — dim dot.
			dot = dimStyle.Render("○")
		}

		parts = append(parts, dot)
		if i < numPhases-1 {
			connector := dimStyle.Render(" ── ")
			if card.PhasesDone[i] {
				connector = lipgloss.NewStyle().Foreground(clrGreen).Render(" ── ")
			}
			parts = append(parts, connector)
		}
	}

	return strings.Join(parts, "")
}

func (m Model) renderPhaseLabels(card *epicCard) string {
	var parts []string
	for i, label := range phaseLabels {
		style := dimStyle
		if card.PhasesDone[i] {
			style = lipgloss.NewStyle().Foreground(clrGreen)
		} else if int(card.Phase) == i {
			if card.HasBlocker {
				style = lipgloss.NewStyle().Foreground(clrRed)
			} else {
				style = lipgloss.NewStyle().Foreground(clrBlue)
			}
		}
		// Pad label to align with dots + connectors.
		padded := fmt.Sprintf("%-6s", label)
		parts = append(parts, style.Render(padded))
	}
	return strings.Join(parts, "")
}

func (m Model) gridFooter() string {
	keys := []struct{ key, desc string }{
		{"↑↓←→", "navigate"},
		{"enter", "open epic"},
		{"a", "auto cmd"},
		{"r", "resolve"},
		{"d", "diff"},
		{"y", "accept"},
		{"n", "reject"},
		{"H", "history"},
		{"c", "new epic"},
		{"R", "refresh"},
	}
	return renderFooter(keys)
}

// ════════════════════════════════════════════════
// EPIC DETAIL VIEW — drill-down into tasks
// ════════════════════════════════════════════════

func (m Model) viewEpicDetail() string {
	if m.epicDetail == nil {
		return "No epic selected"
	}

	var b strings.Builder
	e := m.epicDetail

	// Header.
	b.WriteString(titleStyle.Render(fmt.Sprintf("E#%d %s", e.Epic.ID, e.Epic.Title)))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("esc back"))
	b.WriteString("\n\n")

	if e.Epic.GitBranch != "" {
		b.WriteString("  " + dimStyle.Render("branch: "+e.Epic.GitBranch) + "\n")
	}

	// Pipeline tracker (same as card but full width).
	b.WriteString("  " + m.renderPipeline(e) + "\n")
	b.WriteString("  " + m.renderPhaseLabels(e) + "\n\n")

	// Task list.
	if len(e.Tasks) == 0 {
		b.WriteString(dimStyle.Render("  No tasks yet. Run auto to plan.\n"))
	} else {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("  Tasks:") + "\n")

		for i, t := range e.Tasks {
			selected := i == m.taskCursor
			line := m.renderTaskLine(t, selected)
			b.WriteString(line + "\n")
		}
	}

	b.WriteString("\n")

	// Recent log (last 8 events).
	if len(e.Events) > 0 {
		b.WriteString(lipgloss.NewStyle().Bold(true).Render("  Log:") + "\n")
		start := 0
		if len(e.Events) > 8 {
			start = len(e.Events) - 8
		}
		for _, ev := range e.Events[start:] {
			ts := dimStyle.Render(ev.Timestamp.Local().Format("15:04"))
			agent := ""
			if ev.Agent != "" {
				agent = lipgloss.NewStyle().Foreground(clrCyan).Render(ev.Agent) + " "
			}
			content := truncate(ev.Content, 60)
			b.WriteString(fmt.Sprintf("    %s %s%s\n", ts, agent, content))
		}
	}

	// Footer.
	b.WriteString("\n")
	keys := []struct{ key, desc string }{
		{"↑↓", "select task"},
		{"r", "resolve"},
		{"d", "diff"},
		{"y", "accept"},
		{"n", "reject"},
		{"H", "history"},
		{"a", "auto cmd"},
		{"esc", "back"},
	}
	b.WriteString(renderFooter(keys))

	return b.String()
}

func (m Model) renderTaskLine(t store.Task, selected bool) string {
	// Status dot.
	var dot string
	switch t.Status {
	case store.StatusDone:
		dot = lipgloss.NewStyle().Foreground(clrGreen).Render("●")
	case store.StatusInProgress:
		dot = lipgloss.NewStyle().Foreground(clrBlue).Render("◉")
	case store.StatusBlocked:
		dot = lipgloss.NewStyle().Foreground(clrRed).Render("●")
	case store.StatusReview:
		dot = lipgloss.NewStyle().Foreground(clrYellow).Render("◉")
	case store.StatusFailed:
		dot = lipgloss.NewStyle().Foreground(clrRed).Render("✗")
	case store.StatusCancelled:
		dot = dimStyle.Render("—")
	default:
		dot = dimStyle.Render("○")
	}

	// ID + title.
	id := lipgloss.NewStyle().Foreground(clrCyan).Render(fmt.Sprintf("#%d", t.ID))
	title := truncate(t.Title, 35)

	// Status label.
	statusStr := dimStyle.Render(string(t.Status))

	// Agent.
	agent := ""
	if t.AssignedAgent != "" {
		agent = dimStyle.Render(t.AssignedAgent)
	}

	// Cursor indicator.
	cursor := "  "
	if selected {
		cursor = lipgloss.NewStyle().Foreground(clrHighlight).Render("▸ ")
	}

	line := fmt.Sprintf("  %s %s %s %-40s %-12s %s", cursor, dot, id, title, statusStr, agent)

	// Add blocker reason if blocked.
	if t.Status == store.StatusBlocked && t.BlockedReason != "" {
		reason := truncate(t.BlockedReason, 50)
		line += "\n      " + lipgloss.NewStyle().Foreground(clrRed).Render("⚠ "+reason)
	}

	return line
}

// ════════════════════════════════════════════════
// DIFF VIEW
// ════════════════════════════════════════════════

func (m Model) viewDiff() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("Diff"))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render(fmt.Sprintf("E#%d", m.diffEpicID)))
	b.WriteString("\n\n")

	b.WriteString(m.diffViewport.View())
	b.WriteString("\n\n")

	keys := []struct{ key, desc string }{
		{"↑↓", "scroll"},
		{"y", "accept"},
		{"n", "reject"},
		{"e", "request fix"},
		{"esc", "back"},
	}
	b.WriteString(renderFooter(keys))

	return b.String()
}

// ════════════════════════════════════════════════
// HISTORY VIEW
// ════════════════════════════════════════════════

func (m Model) viewHistory() string {
	var b strings.Builder

	b.WriteString(titleStyle.Render("History"))
	b.WriteString("\n\n")

	b.WriteString(m.historyViewport.View())
	b.WriteString("\n\n")

	keys := []struct{ key, desc string }{
		{"↑↓", "scroll"},
		{"esc", "back"},
	}
	b.WriteString(renderFooter(keys))

	return b.String()
}

// ════════════════════════════════════════════════
// POPUPS
// ════════════════════════════════════════════════

func (m Model) overlayPopup(bg string) string {
	var popup string

	switch m.popup {
	case popupResolve:
		popup = m.viewResolvePopup()
	case popupReject:
		popup = m.viewRejectPopup()
	case popupRequestFix:
		popup = m.viewRequestFixPopup()
	case popupCreateEpic:
		popup = m.viewCreateEpicPopup()
	case popupConfirmAccept:
		popup = m.viewConfirmAcceptPopup()
	default:
		return bg
	}

	// Place popup in center of screen.
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			popup,
			lipgloss.WithWhitespaceChars(" "),
		)
	}

	return popup
}

func (m Model) viewResolvePopup() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(clrYellow).Render("Resolve Blocker")
	b.WriteString(title + "\n\n")

	// Find the blocked task to show the question.
	task, _ := m.store.GetTask(m.popupTaskID)
	if task != nil {
		q := lipgloss.NewStyle().Foreground(clrRed).Render(task.BlockedReason)
		b.WriteString(fmt.Sprintf("#%d asks:\n%s\n\n", task.ID, q))
	}

	b.WriteString("Your answer:\n")
	b.WriteString(m.textInput.View() + "\n\n")
	b.WriteString(footerDescStyle.Render("enter submit • esc cancel"))

	return m.popupBoxStyle().Render(b.String())
}

func (m Model) viewRejectPopup() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(clrRed).Render("Reject Epic")
	b.WriteString(title + "\n\n")

	b.WriteString("This will delete the safety branch and discard all changes.\n\n")
	b.WriteString("Reason (optional):\n")
	b.WriteString(m.textInput.View() + "\n\n")
	b.WriteString(footerDescStyle.Render("enter confirm • esc cancel"))

	return m.popupBoxStyle().Render(b.String())
}

func (m Model) viewRequestFixPopup() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(clrYellow).Render("Request Changes")
	b.WriteString(title + "\n\n")

	b.WriteString("Describe what needs fixing.\nThis creates a new task and re-runs the pipeline.\n\n")
	b.WriteString("What needs fixing:\n")
	b.WriteString(m.textInput.View() + "\n\n")
	b.WriteString(footerDescStyle.Render("enter create task • esc cancel"))

	return m.popupBoxStyle().Render(b.String())
}

func (m Model) viewCreateEpicPopup() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(clrHighlight).Render("Create Epic")
	b.WriteString(title + "\n\n")

	b.WriteString("Title:\n")
	b.WriteString(m.textInput.View() + "\n\n")

	b.WriteString("Description:\n")
	b.WriteString(m.textInput2.View() + "\n\n")

	priStyle := lipgloss.NewStyle().Bold(true)
	switch m.createPriority {
	case "high":
		priStyle = priStyle.Foreground(clrRed)
	case "medium":
		priStyle = priStyle.Foreground(clrYellow)
	case "low":
		priStyle = priStyle.Foreground(clrSubtle)
	}
	b.WriteString(fmt.Sprintf("Priority: %s\n\n", priStyle.Render(m.createPriority)))

	b.WriteString(footerDescStyle.Render("enter create • tab switch • ctrl+p priority • esc cancel"))

	return m.popupBoxStyle().Render(b.String())
}

func (m Model) viewConfirmAcceptPopup() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(clrGreen).Render("Accept Epic")
	b.WriteString(title + "\n\n")

	b.WriteString("Merge safety branch into main?\n")
	b.WriteString("This is permanent.\n\n")

	b.WriteString(footerKeyStyle.Render("y") + footerDescStyle.Render(" confirm  ") +
		footerKeyStyle.Render("n") + footerDescStyle.Render(" cancel"))

	return m.popupBoxStyle().Render(b.String())
}

func (m Model) popupBoxStyle() lipgloss.Style {
	w := 60
	if m.width > 0 {
		w = m.width - 12
		if w < 42 {
			w = 42
		}
		if w > 84 {
			w = 84
		}
	}
	return popupStyle.Width(w)
}

// ════════════════════════════════════════════════
// SHARED HELPERS
// ════════════════════════════════════════════════

func renderFooter(keys []struct{ key, desc string }) string {
	var parts []string
	for _, k := range keys {
		key := footerKeyStyle.Render(k.key)
		desc := footerDescStyle.Render(k.desc)
		parts = append(parts, key+" "+desc)
	}
	return "  " + strings.Join(parts, "  ")
}

func allTasksDone(card *epicCard) bool {
	if len(card.Tasks) == 0 {
		return false
	}
	for _, t := range card.Tasks {
		if t.Status != store.StatusDone && t.Status != store.StatusCancelled {
			return false
		}
	}
	return true
}
