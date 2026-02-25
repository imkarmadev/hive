package agent

import (
	"fmt"
	"testing"
)

func TestParseSubtasks_Standard(t *testing.T) {
	output := `Here's my analysis of the task.

SUBTASKS:
1. Setup auth middleware - Configure JWT verification on protected routes (priority: high)
2. Create login endpoint - POST /auth/login with email/password (priority: high)
3. Add refresh token logic - Token rotation and storage (priority: medium)
4. Write integration tests - Test full auth flow (priority: low)
`

	subtasks := ParseSubtasks(output)
	if len(subtasks) != 4 {
		t.Fatalf("expected 4 subtasks, got %d", len(subtasks))
	}

	if subtasks[0].Title != "Setup auth middleware" {
		t.Errorf("subtask 0 title: got %q", subtasks[0].Title)
	}
	if subtasks[0].Priority != "high" {
		t.Errorf("subtask 0 priority: got %q", subtasks[0].Priority)
	}
	if subtasks[0].Description != "Configure JWT verification on protected routes" {
		t.Errorf("subtask 0 desc: got %q", subtasks[0].Description)
	}
	if subtasks[3].Priority != "low" {
		t.Errorf("subtask 3 priority: got %q", subtasks[3].Priority)
	}
}

func TestParseSubtasks_NoPriorityDefaults(t *testing.T) {
	output := `SUBTASKS:
1. Do thing A - First thing
2. Do thing B - Second thing
`
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(subtasks))
	}
	if subtasks[0].Priority != "medium" {
		t.Errorf("expected default medium, got %q", subtasks[0].Priority)
	}
}

func TestParseSubtasks_BulletPoints(t *testing.T) {
	output := `SUBTASKS:
- Setup database - Create tables (priority: high)
- Add migrations - Schema versioning (priority: medium)
`
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(subtasks))
	}
	if subtasks[0].Title != "Setup database" {
		t.Errorf("got title %q", subtasks[0].Title)
	}
}

func TestParseSubtasks_WithoutHeader(t *testing.T) {
	output := `I think we should do:
1. First task - Do this
2. Second task - Do that
`
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(subtasks))
	}
}

func TestParseSubtasks_Empty(t *testing.T) {
	output := "I don't think this needs subtasks."
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 0 {
		t.Fatalf("expected 0 subtasks, got %d", len(subtasks))
	}
}

func TestParseReview_Approve(t *testing.T) {
	output := `Looking at the changes...

VERDICT: APPROVE

COMMENTS:
- Clean implementation, good error handling
- Tests cover edge cases well
`
	review := ParseReview(output)
	if review.Verdict != "APPROVE" {
		t.Errorf("expected APPROVE, got %q", review.Verdict)
	}
	if len(review.Comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(review.Comments))
	}
}

func TestParseReview_Reject(t *testing.T) {
	output := `VERDICT: REJECT

COMMENTS:
- auth.go:42: SQL injection vulnerability in query builder
- auth.go:88: Missing error handling for token expiration
- No tests for refresh token flow
`
	review := ParseReview(output)
	if review.Verdict != "REJECT" {
		t.Errorf("expected REJECT, got %q", review.Verdict)
	}
	if len(review.Comments) != 3 {
		t.Fatalf("expected 3 comments, got %d", len(review.Comments))
	}
	if review.Comments[0] != "auth.go:42: SQL injection vulnerability in query builder" {
		t.Errorf("comment 0: got %q", review.Comments[0])
	}
}

func TestParseReview_MarkdownFormatted(t *testing.T) {
	output := `## Code Review

**Verdict:** Approve

The changes look solid. Good error handling throughout.

**Comments:**
- Clean implementation with proper error boundaries
- Tests cover the main scenarios
`
	review := ParseReview(output)
	if review.Verdict != "APPROVE" {
		t.Errorf("expected APPROVE from markdown format, got %q", review.Verdict)
	}
}

func TestParseReview_NaturalLanguageApprove(t *testing.T) {
	output := `I've reviewed the changes and they look good to me.
The implementation follows best practices and LGTM.

- Nice use of middleware pattern
- Error handling is comprehensive
`
	review := ParseReview(output)
	if review.Verdict != "APPROVE" {
		t.Errorf("expected APPROVE from natural language, got %q", review.Verdict)
	}
}

func TestParseReview_NaturalLanguageReject(t *testing.T) {
	output := `The vulnerability has NOT been fixed. The dependency is still outdated.

This is a critical issue that must be fixed before merging.

- golang.org/x/text is still at v0.3.8
- CVE-2022-32149 is still present
`
	review := ParseReview(output)
	if review.Verdict != "REJECT" {
		t.Errorf("expected REJECT from natural language, got %q", review.Verdict)
	}
}

func TestParseReview_NoVerdict(t *testing.T) {
	output := "The code has some interesting patterns. I need more context to evaluate."
	review := ParseReview(output)
	if review.Verdict != "" {
		t.Errorf("expected empty verdict, got %q", review.Verdict)
	}
}

func TestParseReview_BulletComments(t *testing.T) {
	output := `VERDICT: REJECT

Some issues I found:

• auth.go:42: Missing validation
• No tests for edge cases
• Consider adding rate limiting
`
	review := ParseReview(output)
	if review.Verdict != "REJECT" {
		t.Errorf("expected REJECT, got %q", review.Verdict)
	}
	if len(review.Comments) < 3 {
		t.Errorf("expected at least 3 comments, got %d", len(review.Comments))
	}
}

func TestParseSubtasks_GarbageSectionHeaders(t *testing.T) {
	// Simulates a PM that writes analysis with section headers and markdown bold.
	// Only real subtasks after SUBTASKS: should be parsed; section headers should be filtered.
	output := `I analyzed the project and here are my findings:

1. Comprehensive SECURITY.md document** that already identifies vulnerabilities
2. Existing mitigations**:
3. Known limitations** (documented as architectural):

SUBTASKS:
1. Sanitize shell metacharacters in vars/resolver.go Substitute() - Escape backticks, $(), and pipe chars in variable substitution (priority: high)
2. Add input validation to runner/runner.go ExecCmd() - Validate default_cmd does not contain shell injection patterns (priority: high)
3. Existing mitigations**: some description here (priority: medium)
4. Path traversal prevention in config/loader.go - Ensure config file paths are restricted to project root (priority: medium)
`

	subtasks := ParseSubtasks(output)

	// Should get 3 real subtasks: items 1, 2, 4 from SUBTASKS section.
	// Item 3 "Existing mitigations**" should be filtered as garbage.
	// Items before SUBTASKS: should be ignored because explicit header exists.
	if len(subtasks) != 3 {
		t.Fatalf("expected 3 subtasks, got %d: %+v", len(subtasks), subtasks)
	}

	if subtasks[0].Title != "Sanitize shell metacharacters in vars/resolver.go Substitute()" {
		t.Errorf("subtask 0 title: got %q", subtasks[0].Title)
	}
	if subtasks[1].Title != "Add input validation to runner/runner.go ExecCmd()" {
		t.Errorf("subtask 1 title: got %q", subtasks[1].Title)
	}
	if subtasks[2].Title != "Path traversal prevention in config/loader.go" {
		t.Errorf("subtask 2 title: got %q", subtasks[2].Title)
	}
}

func TestParseSubtasks_MarkdownBoldCleaning(t *testing.T) {
	output := `SUBTASKS:
1. **Fix SQL injection in db/query.go** - Parameterize raw queries (priority: high)
2. [Update dependencies] - Bump vulnerable packages (priority: medium)
`
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(subtasks))
	}
	if subtasks[0].Title != "Fix SQL injection in db/query.go" {
		t.Errorf("expected cleaned title, got %q", subtasks[0].Title)
	}
	if subtasks[1].Title != "Update dependencies" {
		t.Errorf("expected cleaned title, got %q", subtasks[1].Title)
	}
}

func TestParseSubtasks_CapAt10(t *testing.T) {
	output := "SUBTASKS:\n"
	for i := 1; i <= 15; i++ {
		output += fmt.Sprintf("%d. Task number %d - Description of task %d (priority: medium)\n", i, i, i)
	}
	subtasks := ParseSubtasks(output)
	if len(subtasks) != 10 {
		t.Fatalf("expected 10 subtasks (capped), got %d", len(subtasks))
	}
}

func TestParseBlocked(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"BLOCKED: Which database should I use?", "Which database should I use?"},
		{"Some text\nBLOCKED: Need clarification on API format\nMore text", "Need clarification on API format"},
		{"No blockers here", ""},
		{"blocked: lowercase works too", "lowercase works too"},
		// Markdown bold — the most common LLM variant.
		{"**BLOCKED: This task requires architectural clarification**", "This task requires architectural clarification"},
		{"**BLOCKED:** This task cannot be completed as specified.", "This task cannot be completed as specified."},
		// Blockquote.
		{"> BLOCKED: Needs user input on API version", "Needs user input on API version"},
		// Markdown heading prefix.
		{"## BLOCKED: Design decision needed", "Design decision needed"},
		// Bullet prefix.
		{"- BLOCKED: Which ORM should we use?", "Which ORM should we use?"},
	}

	for _, tc := range tests {
		got := ParseBlocked(tc.input)
		if got != tc.expected {
			t.Errorf("ParseBlocked(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
