package config

import (
	"os"
	"path/filepath"
	"testing"
)

// --- EffectiveArgs tests ---

func TestEffectiveArgs_APIMode_ReturnsArgsUnchanged(t *testing.T) {
	a := Agent{
		Mode:       "api",
		Cmd:        "",
		Args:       []string{"--some-flag"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if len(got) != 1 || got[0] != "--some-flag" {
		t.Fatalf("expected original args for api mode, got %v", got)
	}
}

func TestEffectiveArgs_Claude_AddsNonInteractive(t *testing.T) {
	a := Agent{
		Mode: "cli",
		Cmd:  "claude",
		Args: []string{"--model", "sonnet"},
	}
	got := a.EffectiveArgs()
	// Should prepend --print even without auto_accept.
	if !containsAny(got, "--print") {
		t.Fatalf("expected --print in args, got %v", got)
	}
	// Should NOT have --dangerously-skip-permissions without auto_accept.
	if containsAny(got, "--dangerously-skip-permissions") {
		t.Fatalf("should not have --dangerously-skip-permissions without auto_accept, got %v", got)
	}
}

func TestEffectiveArgs_Claude_AutoAccept(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "claude",
		Args:       []string{"--model", "sonnet"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if !containsAny(got, "--print") {
		t.Fatalf("expected --print, got %v", got)
	}
	if !containsAny(got, "--dangerously-skip-permissions") {
		t.Fatalf("expected --dangerously-skip-permissions with auto_accept, got %v", got)
	}
}

func TestEffectiveArgs_Claude_NoDuplicateFlags(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "claude",
		Args:       []string{"--print", "--dangerously-skip-permissions"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	// Count occurrences — should be exactly 1 each.
	printCount := 0
	skipCount := 0
	for _, arg := range got {
		if arg == "--print" {
			printCount++
		}
		if arg == "--dangerously-skip-permissions" {
			skipCount++
		}
	}
	if printCount != 1 {
		t.Fatalf("expected 1 --print, got %d in %v", printCount, got)
	}
	if skipCount != 1 {
		t.Fatalf("expected 1 --dangerously-skip-permissions, got %d in %v", skipCount, got)
	}
}

func TestEffectiveArgs_Claude_ShortPrintFlag(t *testing.T) {
	// If user specifies -p (short for --print), don't add --print.
	a := Agent{
		Mode: "cli",
		Cmd:  "claude",
		Args: []string{"-p"},
	}
	got := a.EffectiveArgs()
	printCount := 0
	for _, arg := range got {
		if arg == "--print" || arg == "-p" {
			printCount++
		}
	}
	if printCount != 1 {
		t.Fatalf("expected 1 print flag variant, got %d in %v", printCount, got)
	}
}

func TestEffectiveArgs_Claude_PermissionModeSkipsAutoAccept(t *testing.T) {
	// If --permission-mode is already set, don't add --dangerously-skip-permissions.
	a := Agent{
		Mode:       "cli",
		Cmd:        "claude",
		Args:       []string{"--permission-mode", "plan"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if containsAny(got, "--dangerously-skip-permissions") {
		t.Fatalf("should not add --dangerously-skip-permissions when --permission-mode present, got %v", got)
	}
}

func TestEffectiveArgs_Gemini_AutoAccept(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "gemini",
		Args:       []string{},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if !containsAny(got, "--yolo") {
		t.Fatalf("expected --yolo for gemini with auto_accept, got %v", got)
	}
}

func TestEffectiveArgs_Gemini_NoAutoAccept(t *testing.T) {
	a := Agent{
		Mode: "cli",
		Cmd:  "gemini",
		Args: []string{},
	}
	got := a.EffectiveArgs()
	if containsAny(got, "--yolo") {
		t.Fatalf("should not have --yolo without auto_accept, got %v", got)
	}
}

func TestEffectiveArgs_Gemini_NoDuplicateYolo(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "gemini",
		Args:       []string{"--yolo"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	count := 0
	for _, arg := range got {
		if arg == "--yolo" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected 1 --yolo, got %d in %v", count, got)
	}
}

func TestEffectiveArgs_Codex_AutoAccept(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "codex",
		Args:       []string{},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if !containsAny(got, "--full-auto") {
		t.Fatalf("expected --full-auto for codex with auto_accept, got %v", got)
	}
}

func TestEffectiveArgs_Codex_ApprovalModeSkips(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "codex",
		Args:       []string{"--approval-mode", "suggest"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if containsAny(got, "--full-auto") {
		t.Fatalf("should not add --full-auto when --approval-mode present, got %v", got)
	}
}

func TestEffectiveArgs_UnknownCLI_ReturnsArgsUnchanged(t *testing.T) {
	a := Agent{
		Mode:       "cli",
		Cmd:        "my-custom-agent",
		Args:       []string{"--verbose"},
		AutoAccept: true,
	}
	got := a.EffectiveArgs()
	if len(got) != 1 || got[0] != "--verbose" {
		t.Fatalf("expected unchanged args for unknown CLI, got %v", got)
	}
}

func TestEffectiveArgs_DoesNotMutateOriginal(t *testing.T) {
	original := []string{"--model", "sonnet"}
	a := Agent{
		Mode:       "cli",
		Cmd:        "claude",
		Args:       original,
		AutoAccept: true,
	}
	_ = a.EffectiveArgs()
	// Original should be untouched.
	if len(original) != 2 || original[0] != "--model" || original[1] != "sonnet" {
		t.Fatalf("EffectiveArgs mutated original args: %v", original)
	}
}

// --- DefaultTimeout tests ---

func TestDefaultTimeout_Custom(t *testing.T) {
	a := Agent{TimeoutSec: 600}
	if a.DefaultTimeout() != 600 {
		t.Fatalf("expected 600, got %d", a.DefaultTimeout())
	}
}

func TestDefaultTimeout_Default(t *testing.T) {
	a := Agent{}
	if a.DefaultTimeout() != 300 {
		t.Fatalf("expected default 300, got %d", a.DefaultTimeout())
	}
}

// --- containsAny / appendFront tests ---

func TestContainsAny_Found(t *testing.T) {
	if !containsAny([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected true")
	}
}

func TestContainsAny_NotFound(t *testing.T) {
	if containsAny([]string{"a", "b", "c"}, "d", "e") {
		t.Fatal("expected false")
	}
}

func TestContainsAny_Empty(t *testing.T) {
	if containsAny([]string{}, "a") {
		t.Fatal("expected false for empty slice")
	}
}

func TestAppendFront(t *testing.T) {
	got := appendFront([]string{"b", "c"}, "a")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("expected [a b c], got %v", got)
	}
}

// --- Load / Save / Validate tests ---

func TestLoad_Valid(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")
	data := `version: 1
agents:
  claude:
    role: coder
    mode: cli
    cmd: claude
    args: ["--print"]
    auto_accept: true
  gpt4:
    role: reviewer
    mode: api
    provider: openai
    model: gpt-4o
    api_key_env: OPENAI_API_KEY
`
	os.WriteFile(p, []byte(data), 0644)

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Version != 1 {
		t.Fatalf("expected version 1, got %d", cfg.Version)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}
	claude := cfg.Agents["claude"]
	if !claude.AutoAccept {
		t.Fatal("expected claude auto_accept to be true")
	}
	if claude.Role != "coder" {
		t.Fatalf("expected coder, got %s", claude.Role)
	}
}

func TestLoad_MissingMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")
	data := `version: 1
agents:
  bad:
    role: coder
`
	os.WriteFile(p, []byte(data), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected validation error for missing mode")
	}
}

func TestLoad_MissingCmd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")
	data := `version: 1
agents:
  bad:
    role: coder
    mode: cli
`
	os.WriteFile(p, []byte(data), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected validation error for missing cmd in cli mode")
	}
}

func TestLoad_MissingProvider(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")
	data := `version: 1
agents:
  bad:
    role: reviewer
    mode: api
`
	os.WriteFile(p, []byte(data), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected validation error for missing provider in api mode")
	}
}

func TestLoad_MissingRole(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")
	data := `version: 1
agents:
  bad:
    mode: cli
    cmd: claude
`
	os.WriteFile(p, []byte(data), 0644)

	_, err := Load(p)
	if err == nil {
		t.Fatal("expected validation error for missing role")
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/hive.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestSave_And_Reload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "hive.yaml")

	cfg := &Config{
		Version: 1,
		Agents: map[string]Agent{
			"test": {
				Role:       "coder",
				Mode:       "cli",
				Cmd:        "claude",
				Args:       []string{"--print"},
				AutoAccept: true,
				TimeoutSec: 600,
			},
		},
	}

	if err := Save(p, cfg); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := Load(p)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	agent := loaded.Agents["test"]
	if !agent.AutoAccept {
		t.Fatal("auto_accept lost after save/load round-trip")
	}
	if agent.TimeoutSec != 600 {
		t.Fatalf("timeout lost after round-trip: got %d", agent.TimeoutSec)
	}
}

// --- AgentsByRole tests ---

func TestAgentsByRole(t *testing.T) {
	cfg := &Config{
		Version: 1,
		Agents: map[string]Agent{
			"a": {Role: "coder", Mode: "cli", Cmd: "claude"},
			"b": {Role: "reviewer", Mode: "cli", Cmd: "gemini"},
			"c": {Role: "coder", Mode: "api", Provider: "openai"},
		},
	}
	coders := cfg.AgentsByRole("coder")
	if len(coders) != 2 {
		t.Fatalf("expected 2 coders, got %d", len(coders))
	}
	reviewers := cfg.AgentsByRole("reviewer")
	if len(reviewers) != 1 {
		t.Fatalf("expected 1 reviewer, got %d", len(reviewers))
	}
	none := cfg.AgentsByRole("pm")
	if len(none) != 0 {
		t.Fatalf("expected 0 pm agents, got %d", len(none))
	}
}
