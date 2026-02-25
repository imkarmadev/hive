package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/imkarma/hive/internal/store"
)

// Styles.
var (
	// Colors.
	subtle    = lipgloss.AdaptiveColor{Light: "#555555", Dark: "#888888"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	warning   = lipgloss.AdaptiveColor{Light: "#FF6600", Dark: "#FF8800"}
	danger    = lipgloss.AdaptiveColor{Light: "#FF0000", Dark: "#FF4444"}
	info      = lipgloss.AdaptiveColor{Light: "#0066FF", Dark: "#4499FF"}

	// Board styles.
	columnHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1).
			Width(26)

	selectedCardStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(highlight).
				Padding(0, 1).
				Width(26).
				Bold(true)

	blockedCardStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(danger).
				Padding(0, 1).
				Width(26)

	// Detail view styles.
	detailTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(highlight).
				MarginBottom(1)

	detailLabelStyle = lipgloss.NewStyle().
				Foreground(subtle).
				Width(12)

	// Dialog styles.
	dialogStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(highlight).
			Padding(1, 2).
			Width(60)

	// Footer.
	footerStyle = lipgloss.NewStyle().
			Foreground(subtle)

	statusBarStyle = lipgloss.NewStyle().
			Foreground(special)
)

// View implements tea.Model.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	switch m.currentView {
	case viewBoard:
		return m.viewBoard()
	case viewDetail:
		return m.viewDetail()
	case viewCreate:
		return m.viewCreate()
	case viewAnswer:
		return m.viewAnswer()
	}
	return ""
}

func (m Model) viewBoard() string {
	var b strings.Builder

	// Header.
	totalTasks := len(m.tasks)
	header := lipgloss.NewStyle().Bold(true).Foreground(highlight).Render("hive")
	header += lipgloss.NewStyle().Foreground(subtle).Render(fmt.Sprintf(" — %d tasks", totalTasks))
	b.WriteString(header + "\n\n")

	// Columns.
	colWidth := 28
	if m.width > 0 {
		colWidth = (m.width - 2) / numColumns
		if colWidth < 20 {
			colWidth = 20
		}
		if colWidth > 35 {
			colWidth = 35
		}
	}

	// Update card widths.
	cStyle := cardStyle.Width(colWidth - 2)
	sStyle := selectedCardStyle.Width(colWidth - 2)
	bStyle := blockedCardStyle.Width(colWidth - 2)

	// Column headers.
	headers := make([]string, numColumns)
	for i, label := range columnLabels {
		count := len(m.columns[i])
		color := columnColor(i)
		style := columnHeaderStyle.Foreground(color).Width(colWidth)
		if i == m.cursorCol {
			style = style.Underline(true)
		}
		headers[i] = style.Render(fmt.Sprintf("%s (%d)", label, count))
	}
	b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, headers...))
	b.WriteString("\n")

	// Find max rows.
	maxRows := 0
	for _, col := range m.columns {
		if len(col) > maxRows {
			maxRows = len(col)
		}
	}

	// Limit visible rows based on height.
	visibleRows := maxRows
	if m.height > 0 {
		maxVisible := (m.height - 6) / 4 // each card ~3-4 lines
		if maxVisible < 1 {
			maxVisible = 1
		}
		if visibleRows > maxVisible {
			visibleRows = maxVisible
		}
	}

	// Render rows.
	for row := 0; row < visibleRows; row++ {
		cards := make([]string, numColumns)
		for col := 0; col < numColumns; col++ {
			if row < len(m.columns[col]) {
				t := m.columns[col][row]
				isSelected := col == m.cursorCol && row == m.cursorRow

				// Build card content.
				id := fmt.Sprintf("#%d", t.ID)
				title := truncateStr(t.Title, colWidth-8)
				pri := priorityIndicator(t.Priority)

				content := fmt.Sprintf("%s %s %s", pri, id, title)

				// Add agent info.
				if t.AssignedAgent != "" {
					agentLine := lipgloss.NewStyle().Foreground(info).Render("[" + t.AssignedAgent + "]")
					content += "\n" + agentLine
				}

				// Add blocker.
				if t.Status == store.StatusBlocked && t.BlockedReason != "" {
					reason := truncateStr(t.BlockedReason, colWidth-8)
					blockerLine := lipgloss.NewStyle().Foreground(danger).Render("⚠ " + reason)
					content += "\n" + blockerLine
				}

				// Pick style.
				style := cStyle
				if isSelected {
					style = sStyle
				} else if t.Status == store.StatusBlocked {
					style = bStyle
				}

				cards[col] = style.Render(content)
			} else {
				cards[col] = strings.Repeat(" ", colWidth)
			}
		}
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, cards...))
		b.WriteString("\n")
	}

	// Blockers summary.
	blockedTasks := m.columns[colBlocked]
	if len(blockedTasks) > 0 {
		b.WriteString("\n")
		blockerHeader := lipgloss.NewStyle().Bold(true).Foreground(danger).Render("⚠ Blockers")
		b.WriteString(blockerHeader + "\n")
		for _, t := range blockedTasks {
			line := fmt.Sprintf("  #%d: %s", t.ID, t.BlockedReason)
			b.WriteString(lipgloss.NewStyle().Foreground(warning).Render(line) + "\n")
		}
	}

	// Status message.
	if m.statusMsg != "" {
		b.WriteString("\n")
		b.WriteString(statusBarStyle.Render(m.statusMsg))
	}

	// Footer with hotkeys.
	b.WriteString("\n\n")
	b.WriteString(m.boardFooter())

	return b.String()
}

func (m Model) viewDetail() string {
	if m.selectedTask == nil {
		return "No task selected"
	}

	t := m.selectedTask
	var b strings.Builder

	b.WriteString(detailTitleStyle.Render(fmt.Sprintf("Task #%d: %s", t.ID, t.Title)))
	b.WriteString("\n")

	// Fields.
	fields := []struct{ label, value string }{
		{"Status", string(t.Status)},
		{"Priority", t.Priority},
	}
	if t.Description != "" {
		fields = append(fields, struct{ label, value string }{"Description", t.Description})
	}
	if t.AssignedAgent != "" {
		fields = append(fields, struct{ label, value string }{"Agent", t.AssignedAgent})
	}
	if t.Role != "" {
		fields = append(fields, struct{ label, value string }{"Role", t.Role})
	}
	if t.BlockedReason != "" {
		fields = append(fields, struct{ label, value string }{"Blocked", t.BlockedReason})
	}
	if t.ParentID != nil {
		fields = append(fields, struct{ label, value string }{"Parent", fmt.Sprintf("#%d", *t.ParentID)})
	}
	fields = append(fields, struct{ label, value string }{"Created", t.CreatedAt.Format("2006-01-02 15:04")})
	fields = append(fields, struct{ label, value string }{"Updated", t.UpdatedAt.Format("2006-01-02 15:04")})

	for _, f := range fields {
		label := detailLabelStyle.Render(f.label)
		b.WriteString(fmt.Sprintf("%s %s\n", label, f.value))
	}

	// Events.
	if len(m.taskEvents) > 0 {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(highlight).Render("Events"))
		b.WriteString("\n")
		for _, e := range m.taskEvents {
			agent := ""
			if e.Agent != "" {
				agent = lipgloss.NewStyle().Foreground(info).Render("["+e.Agent+"]") + " "
			}
			ts := lipgloss.NewStyle().Foreground(subtle).Render(e.Timestamp.Format("15:04:05"))
			etype := lipgloss.NewStyle().Bold(true).Render(e.Type)
			b.WriteString(fmt.Sprintf("  %s %s%s: %s\n", ts, agent, etype, e.Content))
		}
	}

	b.WriteString("\n")
	b.WriteString(footerStyle.Render("esc/q back"))

	return b.String()
}

func (m Model) viewCreate() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(highlight).Render("Create New Task")
	b.WriteString(title + "\n\n")

	b.WriteString("Title:\n")
	b.WriteString(m.titleInput.View() + "\n\n")

	b.WriteString("Description:\n")
	b.WriteString(m.descInput.View() + "\n\n")

	priStyle := lipgloss.NewStyle().Bold(true)
	switch m.createPriority {
	case "high":
		priStyle = priStyle.Foreground(danger)
	case "medium":
		priStyle = priStyle.Foreground(warning)
	case "low":
		priStyle = priStyle.Foreground(subtle)
	}
	b.WriteString(fmt.Sprintf("Priority: %s\n\n", priStyle.Render(m.createPriority)))

	b.WriteString(footerStyle.Render("enter create • tab switch field • ctrl+p cycle priority • esc cancel"))

	return dialogStyle.Render(b.String())
}

func (m Model) viewAnswer() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(warning).Render("Answer Blocker")
	b.WriteString(title + "\n\n")

	if m.selectedTask != nil {
		q := lipgloss.NewStyle().Foreground(danger).Render(m.selectedTask.BlockedReason)
		b.WriteString(fmt.Sprintf("Task #%d asks:\n%s\n\n", m.selectedTask.ID, q))
	}

	b.WriteString("Your answer:\n")
	b.WriteString(m.answerInput.View() + "\n\n")

	b.WriteString(footerStyle.Render("enter submit • esc cancel"))

	return dialogStyle.Render(b.String())
}

func (m Model) boardFooter() string {
	keys := []struct{ key, desc string }{
		{"↑↓←→/hjkl", "navigate"},
		{"enter", "detail"},
		{"n", "new task"},
		{"a", "answer blocker"},
		{"s", "start"},
		{"d", "done"},
		{"r", "→review"},
		{"b", "→backlog"},
		{"R", "refresh"},
		{"q", "quit"},
	}

	var parts []string
	for _, k := range keys {
		key := lipgloss.NewStyle().Bold(true).Foreground(highlight).Render(k.key)
		desc := lipgloss.NewStyle().Foreground(subtle).Render(k.desc)
		parts = append(parts, key+" "+desc)
	}

	return strings.Join(parts, "  ")
}

func columnColor(col int) lipgloss.AdaptiveColor {
	switch col {
	case colBacklog:
		return subtle
	case colInProgress:
		return info
	case colBlocked:
		return danger
	case colReview:
		return lipgloss.AdaptiveColor{Light: "#8B5CF6", Dark: "#A78BFA"}
	case colDone:
		return special
	default:
		return subtle
	}
}

func priorityIndicator(priority string) string {
	switch priority {
	case "high":
		return lipgloss.NewStyle().Foreground(danger).Render("●")
	case "medium":
		return lipgloss.NewStyle().Foreground(warning).Render("●")
	case "low":
		return lipgloss.NewStyle().Foreground(subtle).Render("○")
	default:
		return " "
	}
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
