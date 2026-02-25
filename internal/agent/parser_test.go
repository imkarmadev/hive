package agent

import (
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

func TestParseReview_NoVerdict(t *testing.T) {
	output := "The code looks fine but I'm not sure about the architecture."
	review := ParseReview(output)
	if review.Verdict != "" {
		t.Errorf("expected empty verdict, got %q", review.Verdict)
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
	}

	for _, tc := range tests {
		got := ParseBlocked(tc.input)
		if got != tc.expected {
			t.Errorf("ParseBlocked(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}
