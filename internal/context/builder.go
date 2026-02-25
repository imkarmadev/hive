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
		return `# You are a Project Manager / Tech Lead
Your job is to INVESTIGATE the actual project codebase first, then break the task into concrete, actionable subtasks.

CRITICAL RULES:
- You MUST explore the codebase before creating subtasks. Read the README, look at the project structure, understand what the project does.
- Each subtask must be specific and actionable — a developer should know exactly what to do from the title and description alone.
- BAD subtask: "Security vulnerabilities" (too vague, not actionable)
- GOOD subtask: "Sanitize shell metacharacters in vars/resolver.go Substitute()" (specific file, specific action)
- If the epic title is vague or misspelled, interpret the intent and create meaningful tasks based on what you find in the actual code.
- Do NOT create subtasks for things you don't find evidence of in the code.`

	case "architect":
		return `# You are a Technical Architect / Tech Lead
Your job is to investigate the codebase for THIS SPECIFIC TASK and produce a detailed technical plan.

CRITICAL RULES:
- You produce a spec that tells the developer EXACTLY what to change, where, and how.
- You do NOT write implementation code.
- If the task is vague, unclear, or doesn't make sense for this codebase — say BLOCKED with a specific question rather than making up work.
- Only include changes that are directly relevant to this task. Don't expand scope.`

	case "coder":
		return `# You are a Software Developer
Your job is to implement the changes specified in this task. You must actually modify files in the project.

CRITICAL RULES:
- You MUST make real code changes — editing, creating, or deleting files as needed.
- If a technical spec (architect_spec) is provided in the history, follow it precisely.
- After making changes, run the project's tests to verify nothing is broken.
- Do NOT just describe what needs to change — actually change it.
- Do NOT ask for permission — you have full access to modify files.`

	case "reviewer":
		return `# You are a Code Reviewer
Your job is to review the changes made for this task. You use a severity-based approach.

CRITICAL RULES FOR VERDICT:
- REJECT only for CRITICAL or HIGH severity issues (security vulnerabilities, data loss, crashes, broken core functionality)
- APPROVE for everything else, even if there are medium/low issues — list them as comments
- You are pragmatic, not perfectionist. Ship working code, note improvements for later.
- If the core task is accomplished and there are no critical bugs, APPROVE.`

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
	label := "Parent Task"
	if parent.Kind == store.KindEpic {
		label = "Epic"
	}
	sb.WriteString(fmt.Sprintf("## %s (for context)\n", label))
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

	// Filter to relevant events (user answers, agent outputs, reviews, architect specs).
	var relevant []store.Event
	for _, e := range events {
		switch e.Type {
		case "unblocked", "comment", "reviewed", "completed", "architect_spec":
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
	case "pm":
		return `## Your Process
1. FIRST: Explore the project structure. Read the README, list directories, understand the tech stack.
2. THEN: Read relevant source files to understand what the project actually does and how it's structured.
3. ONLY THEN: Create subtasks based on what you actually found in the code.

## Rules for Good Subtasks
- Each subtask title must reference a specific file, module, or component (e.g., "Sanitize shell metacharacters in vars/resolver.go Substitute()")
- Each subtask must be completable by a single developer in a focused session
- Do NOT create subtasks for problems you didn't find evidence of in the code
- Do NOT create "research" or "investigate" subtasks — that's YOUR job, you just did it
- If the epic title is vague or misspelled, interpret the user's intent based on what you find in the code
- Create between 3 and 7 subtasks. No more. If you think you need more, combine related work.

## CRITICAL OUTPUT RULES
Your ENTIRE response must be ONLY the SUBTASKS block below. Nothing else.
Do NOT write analysis, findings, summaries, explanations, or commentary.
Do NOT use markdown headers, bold text, or section labels in your output.
Do NOT write anything before "SUBTASKS:" or after the last subtask line.

## Response Format
Your complete response must look EXACTLY like this and nothing else:

SUBTASKS:
1. Title of first subtask - Description of what to do (priority: high)
2. Title of second subtask - Description of what to do (priority: medium)
3. Title of third subtask - Description of what to do (priority: low)

If the task is unclear and you cannot determine what the user wants even after reading the code:
BLOCKED: [your specific question about what the user wants]`

	case "architect":
		return `## Your Process
1. Read the task description carefully. Understand what is being asked.
2. Explore the relevant parts of the codebase — find the files, functions, and types involved.
3. If the task is vague, irrelevant to this project, or doesn't make sense — respond with BLOCKED and a specific question. Do NOT invent work.
4. Produce a spec that tells a developer exactly what to change.

## Rules
- Reference actual file paths and function names from the codebase (not guesses)
- Be specific: "Add a timeout parameter to fetchData() in api/client.go:45" not "consider adding timeouts"
- Note dependencies between changes (what order they should be done in)
- Mention edge cases and things that could go wrong
- Do NOT write implementation code — describe WHAT to change, not the code itself
- Keep scope tight: only include changes directly needed for this task

## Response Format
Provide your technical specification:

SPEC:
For each change:
- **File**: path/to/file.go (function or type name)
  **Change**: What to modify and how (mention function names, approaches, constraints)
  **Reason**: Why this change is needed

SUMMARY:
One paragraph overview of the approach and key architectural decisions.

If the task is unclear or doesn't apply to this codebase:
BLOCKED: [your specific question or concern]`

	case "coder":
		return `## Your Process
1. Read the task description and any architect_spec in the history section carefully.
2. If an architect_spec is provided, follow it as your implementation plan — it tells you exactly what files to change and how.
3. Make the actual code changes — edit files, create new files if needed, delete dead code.
4. After making changes, run the project's test suite to catch regressions.
5. If tests fail, fix the issues before finishing.

## Critical Rules
- You MUST actually modify files. Do NOT just describe what should change — CHANGE IT.
- Do NOT ask for permission to edit files — you have full access.
- Do NOT refactor unrelated code. Stay focused on this specific task.
- Do NOT add features or improvements beyond what the task asks for.
- If you encounter something genuinely unclear that blocks your work, say: BLOCKED: [your specific question]
- If tests exist, run them. If they fail because of your changes, fix them.
- Commit messages are not your job — just make the changes.`

	case "reviewer":
		return `## Your Process
1. Read the task description to understand WHAT was supposed to be done.
2. Check the git diff to see WHAT was actually changed.
3. If the diff is empty or shows no changes related to this task, that means the coder made no changes — evaluate accordingly.
4. Check if the changes accomplish the task's goal.
5. Classify each finding by severity.

## Severity Levels
- CRITICAL: Security vulnerabilities, data loss, crashes, broken core functionality → REJECT
- HIGH: Logic errors that will cause bugs in production, missing error handling for critical paths → REJECT
- MEDIUM: Code quality issues, missing edge cases, suboptimal approaches → APPROVE with comments
- LOW: Style issues, naming, minor improvements → APPROVE with comments

## Verdict Rules
- REJECT only if there are CRITICAL or HIGH severity issues in the ACTUAL CODE CHANGES
- APPROVE if the task is accomplished and there are no critical/high issues, even if medium/low issues exist
- When approving with issues, list them as comments for future improvement
- Be pragmatic: working code that solves the problem is better than perfect code that doesn't ship
- If the diff is empty and the coder didn't make changes, REJECT with a simple note that no changes were made. Do NOT write lengthy analysis about what should have been done.

## IMPORTANT CONSTRAINTS
- You are reviewing CODE CHANGES only. You review what the diff shows.
- Do NOT suggest tools, commands, or features that you are not certain exist in this project.
- Do NOT reference "AskUserQuestion", "interactive mode", or other features unless you see them in the actual code.
- Keep your review focused and concise. A review should be 5-15 lines, not a multi-page essay.

## Response Format
You MUST include a verdict line in this exact format:

VERDICT: APPROVE
or
VERDICT: REJECT

COMMENTS:
- [severity] file:line: description of finding

Example:
VERDICT: APPROVE

COMMENTS:
- [MEDIUM] api/handler.go:42: Missing input length validation, could accept very large payloads
- [LOW] api/handler.go:15: Consider renaming "data" to something more descriptive`

	default:
		return ""
	}
}
