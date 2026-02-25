package agent

import (
	"regexp"
	"strings"
)

// ParsedSubtask represents a subtask extracted from PM agent output.
type ParsedSubtask struct {
	Title       string
	Description string
	Priority    string // high, medium, low
}

// ParsedReview represents a review verdict extracted from reviewer agent output.
type ParsedReview struct {
	Verdict  string // APPROVE, REJECT
	Comments []string
}

// ParseSubtasks extracts subtasks from PM agent output.
// Expected format:
//
//	SUBTASKS:
//	1. [Title] - [Description] (priority: high)
//	2. [Title] - [Description] (priority: medium)
//
// Also supports:
//
//  1. Title - Description
//     - Title - Description
func ParseSubtasks(output string) []ParsedSubtask {
	var subtasks []ParsedSubtask

	// Find SUBTASKS: section or just numbered/bulleted lines.
	lines := strings.Split(output, "\n")
	inSection := false
	hasExplicitHeader := false

	// Pattern: "1. Title - Description (priority: high)" or "- Title - Description"
	numberedRe := regexp.MustCompile(`^(?:\d+[\.\)]\s*|[-*]\s+)(.+)`)
	priorityRe := regexp.MustCompile(`\(priority:\s*(high|medium|low)\)`)

	// Check if there's an explicit SUBTASKS: header — if so, only parse that section.
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "SUBTASKS:") {
			hasExplicitHeader = true
			break
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of subtasks section.
		if strings.HasPrefix(strings.ToUpper(trimmed), "SUBTASKS:") {
			inSection = true
			continue
		}

		// Stop at next section header or empty block after subtasks.
		if inSection && trimmed == "" {
			// Allow empty lines within the section.
			continue
		}
		if inSection && !numberedRe.MatchString(trimmed) && trimmed != "" {
			// Non-list line after subtasks started — could be end of section.
			if strings.HasSuffix(trimmed, ":") {
				break
			}
			continue
		}

		if !inSection {
			// If there's an explicit SUBTASKS: header, skip everything before it.
			if hasExplicitHeader {
				continue
			}
			// Fallback: try to parse numbered lists even without SUBTASKS: header.
			if !numberedRe.MatchString(trimmed) {
				continue
			}
			// Start parsing if we see a numbered list anywhere.
			inSection = true
		}

		match := numberedRe.FindStringSubmatch(trimmed)
		if match == nil {
			continue
		}

		content := match[1]

		// Extract priority.
		priority := "medium"
		if priMatch := priorityRe.FindStringSubmatch(content); priMatch != nil {
			priority = priMatch[1]
			content = strings.TrimSpace(priorityRe.ReplaceAllString(content, ""))
		}

		// Split title - description.
		title := content
		description := ""
		if idx := strings.Index(content, " - "); idx > 0 {
			title = strings.TrimSpace(content[:idx])
			description = strings.TrimSpace(content[idx+3:])
		}

		// Clean up markdown formatting: strip []**` and trailing colons/punctuation.
		title = strings.Trim(title, "[]`")
		// Remove leading/trailing ** (markdown bold).
		title = strings.TrimPrefix(title, "**")
		title = strings.TrimSuffix(title, "**")
		title = strings.TrimRight(title, ":")
		title = strings.TrimSpace(title)

		// Skip lines that look like section headers, not real subtasks.
		// These are artifacts from LLMs writing markdown analysis instead of clean lists.
		if isGarbageSubtask(title) {
			continue
		}

		if title != "" {
			subtasks = append(subtasks, ParsedSubtask{
				Title:       title,
				Description: description,
				Priority:    priority,
			})
		}
	}

	// Cap at 10 subtasks to prevent runaway parsing.
	if len(subtasks) > 10 {
		subtasks = subtasks[:10]
	}

	return subtasks
}

// isGarbageSubtask returns true if a title looks like a section header
// or analysis fragment rather than a real actionable subtask.
func isGarbageSubtask(title string) bool {
	lower := strings.ToLower(title)

	// Titles that are just section labels, not tasks.
	garbagePatterns := []string{
		"existing mitigations",
		"known limitations",
		"low-risk issues",
		"high-risk issues",
		"medium-risk issues",
		"summary",
		"overview",
		"background",
		"findings",
		"analysis",
		"recommendations",
		"conclusion",
		"references",
		"notes",
		"already mitigated",
		"documented as",
		"currently",
	}
	for _, p := range garbagePatterns {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}

	// Very short titles (< 5 chars) are likely not real subtasks.
	if len(title) < 5 {
		return true
	}

	return false
}

// ParseReview extracts the verdict and comments from reviewer output.
// Supports multiple formats since LLMs don't always follow templates exactly:
//
//	VERDICT: APPROVE           — explicit verdict line
//	**Verdict:** APPROVE       — markdown formatted
//	I approve these changes    — natural language (fallback heuristic)
//	LGTM                       — common shorthand
func ParseReview(output string) ParsedReview {
	result := ParsedReview{}

	lines := strings.Split(output, "\n")
	upper := strings.ToUpper(output)

	// Pass 1: Look for explicit VERDICT: line.
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lineUpper := strings.ToUpper(trimmed)

		// "VERDICT: APPROVE" or "**Verdict:** Approve" etc.
		if strings.Contains(lineUpper, "VERDICT") && strings.Contains(lineUpper, ":") {
			afterColon := ""
			if idx := strings.Index(lineUpper, ":"); idx >= 0 {
				afterColon = strings.ToUpper(strings.TrimSpace(trimmed[idx+1:]))
			}
			// Strip markdown formatting.
			afterColon = strings.NewReplacer("*", "", "`", "", "#", "").Replace(afterColon)
			afterColon = strings.TrimSpace(afterColon)

			if strings.Contains(afterColon, "APPROVE") || strings.Contains(afterColon, "ACCEPT") {
				result.Verdict = "APPROVE"
			} else if strings.Contains(afterColon, "REJECT") || strings.Contains(afterColon, "FAIL") {
				result.Verdict = "REJECT"
			}
		}

		// Extract comments section (COMMENTS:, Issues:, Problems:, etc.)
		if strings.HasPrefix(lineUpper, "COMMENTS:") ||
			strings.HasPrefix(lineUpper, "ISSUES:") ||
			strings.HasPrefix(lineUpper, "PROBLEMS:") ||
			strings.HasPrefix(lineUpper, "FINDINGS:") {
			for j := i + 1; j < len(lines); j++ {
				cl := strings.TrimSpace(lines[j])
				if cl == "" {
					continue
				}
				if strings.HasPrefix(cl, "-") || strings.HasPrefix(cl, "*") || strings.HasPrefix(cl, "•") {
					comment := strings.TrimSpace(strings.TrimLeft(cl, "-*•"))
					// Strip leading markdown bold.
					comment = strings.TrimPrefix(comment, "**")
					if comment != "" {
						result.Comments = append(result.Comments, comment)
					}
				} else if strings.HasSuffix(cl, ":") && !strings.HasPrefix(cl, " ") {
					break // New section header.
				}
			}
		}
	}

	// Pass 2: If no explicit verdict found, try heuristics.
	if result.Verdict == "" {
		result.Verdict = inferVerdict(upper)
	}

	// Pass 3: If still no comments, try to extract bullet points from anywhere.
	if len(result.Comments) == 0 {
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if (strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "• ")) &&
				len(trimmed) > 10 {
				comment := strings.TrimSpace(strings.TrimLeft(trimmed, "-•"))
				comment = strings.TrimPrefix(comment, "**")
				if comment != "" {
					result.Comments = append(result.Comments, comment)
				}
			}
		}
	}

	return result
}

// inferVerdict uses heuristics to guess the verdict from natural language.
func inferVerdict(upperOutput string) string {
	// Strong approve signals.
	approveSignals := []string{
		"LGTM", "LOOKS GOOD", "I APPROVE", "APPROVED",
		"CHANGES ARE GOOD", "CHANGES LOOK GOOD",
		"NO ISSUES FOUND", "NO PROBLEMS FOUND",
		"SHIP IT", "READY TO MERGE",
	}
	// Strong reject signals.
	rejectSignals := []string{
		"I REJECT", "REJECTED", "CHANGES REJECTED",
		"MUST BE FIXED", "NEEDS FIXING", "CRITICAL ISSUE",
		"NOT APPROVED", "DO NOT MERGE", "CANNOT APPROVE",
		"VULNERABILITY", "SECURITY ISSUE", "BUG FOUND",
		"HAS NOT BEEN FIXED", "NOT BEEN FIXED", "STILL VULNERABLE",
	}

	approveScore := 0
	rejectScore := 0

	for _, signal := range approveSignals {
		if strings.Contains(upperOutput, signal) {
			approveScore++
		}
	}
	for _, signal := range rejectSignals {
		if strings.Contains(upperOutput, signal) {
			rejectScore++
		}
	}

	// Need a clear winner with at least 1 signal.
	if rejectScore > 0 && rejectScore >= approveScore {
		return "REJECT"
	}
	if approveScore > 0 && approveScore > rejectScore {
		return "APPROVE"
	}

	return "" // genuinely ambiguous
}

// ParseBlocked extracts a BLOCKED reason from agent output.
// Handles various formats LLMs produce:
//
//	BLOCKED: question                — clean format
//	**BLOCKED: question**            — markdown bold
//	**BLOCKED:** question            — markdown bold on label
//	> BLOCKED: question              — blockquote
func ParseBlocked(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		// Strip common markdown prefixes: >, *, #, -
		cleaned := strings.TrimLeft(trimmed, ">*#- ")
		cleaned = strings.TrimSpace(cleaned)
		if strings.HasPrefix(strings.ToUpper(cleaned), "BLOCKED:") {
			reason := strings.TrimSpace(cleaned[8:])
			// Strip surrounding markdown (e.g., leading/trailing **)
			reason = strings.Trim(reason, "*")
			reason = strings.TrimSpace(reason)
			return reason
		}
	}
	return ""
}
