// Package context builds the prompt context for an agent from task data.
// This is the key piece — how we pass information between agents via tasks.
package context

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/imkarma/hive/internal/store"
)

// Builder constructs the full prompt for an agent based on the task
// and its history. Think of it as building a "Jira ticket" that the
// agent reads before starting work.
type Builder struct {
	store *store.Store
}

// New creates a context builder.
func New(s *store.Store) *Builder {
	return &Builder{store: s}
}

// BuildPrompt creates the full prompt for an agent working on a task.
// The prompt includes:
// 1. The task description and acceptance criteria
// 2. Parent task context (if subtask)
// 3. User answers to blockers (conversation history)
// 4. Related artifacts (diffs, plans, review comments)
// 5. Role-specific instructions
func (b *Builder) BuildPrompt(task *store.Task, role string) (string, error) {
	var parts []string

	// 1. Role context.
	parts = append(parts, b.roleHeader(role))

	// 2. Task description.
	parts = append(parts, b.taskSection(task))

	// 3. Parent task context.
	if task.ParentID != nil {
		parentCtx, err := b.parentContext(*task.ParentID)
		if err == nil && parentCtx != "" {
			parts = append(parts, parentCtx)
		}
	}

	// 4. Event history (user answers, previous agent outputs).
	eventCtx, err := b.eventHistory(task.ID)
	if err == nil && eventCtx != "" {
		parts = append(parts, eventCtx)
	}

	// 5. Role-specific instructions.
	parts = append(parts, b.roleInstructions(role))

	return strings.Join(parts, "\n\n"), nil
}

// BuildReviewPrompt creates a specialized prompt for code review.
// Includes the task context plus git diff to show what changed.
func (b *Builder) BuildReviewPrompt(task *store.Task) (string, error) {
	var parts []string

	parts = append(parts, b.roleHeader("reviewer"))
	parts = append(parts, b.taskSection(task))

	// Parent context.
	if task.ParentID != nil {
		parentCtx, err := b.parentContext(*task.ParentID)
		if err == nil && parentCtx != "" {
			parts = append(parts, parentCtx)
		}
	}

	// Git diff — the core of the review.
	diff := b.gitDiff()
	if diff != "" {
		parts = append(parts, "## Changes (git diff)\n```diff\n"+diff+"\n```")
	}

	// Event history (previous reviews, user answers).
	eventCtx, err := b.eventHistory(task.ID)
	if err == nil && eventCtx != "" {
		parts = append(parts, eventCtx)
	}

	parts = append(parts, b.roleInstructions("reviewer"))

	return strings.Join(parts, "\n\n"), nil
}

// gitDiff returns the current uncommitted changes, or the last commit diff.
func (b *Builder) gitDiff() string {
	// First try uncommitted changes.
	out, err := exec.Command("git", "diff").Output()
	if err == nil && len(out) > 0 {
		return truncateDiff(string(out))
	}

	// Try staged changes.
	out, err = exec.Command("git", "diff", "--cached").Output()
	if err == nil && len(out) > 0 {
		return truncateDiff(string(out))
	}

	// Fall back to last commit.
	out, err = exec.Command("git", "diff", "HEAD~1").Output()
	if err == nil && len(out) > 0 {
		return truncateDiff(string(out))
	}

	return ""
}

// truncateDiff limits diff size to avoid blowing up the prompt.
func truncateDiff(diff string) string {
	const maxLen = 8000
	if len(diff) <= maxLen {
		return diff
	}
	return diff[:maxLen] + "\n\n... (diff truncated, " + fmt.Sprintf("%d", len(diff)) + " bytes total)"
}

func (b *Builder) roleHeader(role string) string {
	switch role {
	case "pm":
		return "# You are a Project Manager\nYour job is to break down the task into actionable subtasks with clear acceptance criteria."
	case "coder":
		return "# You are a Software Developer\nYour job is to implement the task. Write clean, tested code. If something is unclear, say so explicitly."
	case "reviewer":
		return "# You are a Code Reviewer\nYour job is to review the changes made for this task. Focus on bugs, security issues, and logic errors. Ignore style nitpicks."
	case "tester":
		return "# You are a QA Engineer\nYour job is to verify the implementation works correctly. Run tests and validate the acceptance criteria."
	case "analyst":
		return "# You are a Technical Analyst\nYour job is to analyze the requirements and provide technical recommendations."
	default:
		return fmt.Sprintf("# You are working as: %s", role)
	}
}

func (b *Builder) taskSection(task *store.Task) string {
	var sb strings.Builder

	sb.WriteString("## Task\n")
	sb.WriteString(fmt.Sprintf("**#%d: %s**\n", task.ID, task.Title))
	sb.WriteString(fmt.Sprintf("Priority: %s\n", task.Priority))

	if task.Description != "" {
		sb.WriteString(fmt.Sprintf("\n### Description\n%s\n", task.Description))
	}

	return sb.String()
}

func (b *Builder) parentContext(parentID int64) (string, error) {
	parent, err := b.store.GetTask(parentID)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString("## Parent Task (for context)\n")
	sb.WriteString(fmt.Sprintf("**#%d: %s**\n", parent.ID, parent.Title))
	if parent.Description != "" {
		sb.WriteString(fmt.Sprintf("%s\n", parent.Description))
	}

	return sb.String(), nil
}

func (b *Builder) eventHistory(taskID int64) (string, error) {
	events, err := b.store.GetEvents(taskID)
	if err != nil {
		return "", err
	}

	// Filter to relevant events (user answers, agent outputs, reviews).
	var relevant []store.Event
	for _, e := range events {
		switch e.Type {
		case "unblocked", "comment", "reviewed", "completed":
			relevant = append(relevant, e)
		}
	}

	if len(relevant) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("## History\n")
	sb.WriteString("Previous interactions on this task:\n\n")

	for _, e := range relevant {
		agent := "system"
		if e.Agent != "" {
			agent = e.Agent
		}
		sb.WriteString(fmt.Sprintf("- **[%s]** %s: %s\n", agent, e.Type, e.Content))
	}

	return sb.String(), nil
}

func (b *Builder) roleInstructions(role string) string {
	switch role {
	case "reviewer":
		return `## Response Format
Respond in this exact format:

VERDICT: APPROVE or REJECT

COMMENTS:
- file:line: description of issue

If approving, briefly explain why the changes look good.
If rejecting, list specific issues that must be fixed.`

	case "pm":
		return `## Response Format
Break the task into subtasks. For each subtask provide:

SUBTASKS:
1. [title] - [description] (priority: high/medium/low)
2. [title] - [description] (priority: high/medium/low)
...

If you need clarification from the user, say:
BLOCKED: [your question]`

	case "coder":
		return `## Instructions
- Make the changes needed to complete this task
- If you're unsure about something, state it clearly rather than guessing
- If you need information from the user, say: BLOCKED: [your question]
- Focus on the specific task, don't refactor unrelated code`

	default:
		return ""
	}
}
