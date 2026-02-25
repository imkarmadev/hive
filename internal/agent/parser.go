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

	// Pattern: "1. Title - Description (priority: high)" or "- Title - Description"
	numberedRe := regexp.MustCompile(`^(?:\d+[\.\)]\s*|[-*]\s+)(.+)`)
	priorityRe := regexp.MustCompile(`\(priority:\s*(high|medium|low)\)`)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect start of subtasks section.
		if strings.HasPrefix(strings.ToUpper(trimmed), "SUBTASKS:") {
			inSection = true
			continue
		}

		// Stop at next section header or empty block after subtasks.
		if inSection && trimmed == "" {
			// Allow one empty line, but two in a row means end of section.
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
			// Also try to parse numbered lists even without SUBTASKS: header.
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

		// Clean up markdown formatting.
		title = strings.Trim(title, "[]**`")
		title = strings.TrimSpace(title)

		if title != "" {
			subtasks = append(subtasks, ParsedSubtask{
				Title:       title,
				Description: description,
				Priority:    priority,
			})
		}
	}

	return subtasks
}

// ParseReview extracts the verdict and comments from reviewer output.
// Expected format:
//
//	VERDICT: APPROVE
//	COMMENTS:
//	- file:line: description
func ParseReview(output string) ParsedReview {
	result := ParsedReview{}

	lines := strings.Split(output, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		upper := strings.ToUpper(trimmed)

		// Extract verdict.
		if strings.HasPrefix(upper, "VERDICT:") {
			rest := strings.TrimSpace(trimmed[8:])
			rest = strings.ToUpper(rest)
			if strings.Contains(rest, "APPROVE") {
				result.Verdict = "APPROVE"
			} else if strings.Contains(rest, "REJECT") {
				result.Verdict = "REJECT"
			}
			continue
		}

		// Extract comments section.
		if strings.HasPrefix(upper, "COMMENTS:") {
			// Collect all following lines that start with - or *
			for j := i + 1; j < len(lines); j++ {
				cl := strings.TrimSpace(lines[j])
				if cl == "" {
					continue
				}
				if strings.HasPrefix(cl, "-") || strings.HasPrefix(cl, "*") {
					comment := strings.TrimSpace(cl[1:])
					if comment != "" {
						result.Comments = append(result.Comments, comment)
					}
				} else if strings.HasSuffix(cl, ":") {
					// New section header, stop.
					break
				}
			}
		}
	}

	return result
}

// ParseBlocked extracts a BLOCKED reason from agent output.
func ParseBlocked(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(trimmed), "BLOCKED:") {
			return strings.TrimSpace(trimmed[8:])
		}
	}
	return ""
}
