package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration for a hive project.
type Config struct {
	Version int              `yaml:"version"`
	Agents  map[string]Agent `yaml:"agents"`
}

// Agent describes a single AI agent and how to connect to it.
type Agent struct {
	Role       string   `yaml:"role"`                  // pm, coder, reviewer, tester, etc.
	Mode       string   `yaml:"mode"`                  // "cli" or "api"
	Cmd        string   `yaml:"cmd,omitempty"`         // CLI command to spawn
	Args       []string `yaml:"args,omitempty"`        // CLI arguments
	Provider   string   `yaml:"provider,omitempty"`    // API provider: openai, anthropic, google
	Model      string   `yaml:"model,omitempty"`       // Model name for API mode
	APIKeyEnv  string   `yaml:"api_key_env,omitempty"` // Env var name containing API key
	TimeoutSec int      `yaml:"timeout_sec,omitempty"` // Timeout in seconds (0 = default 300)
	AutoAccept bool     `yaml:"auto_accept,omitempty"` // Auto-accept all agent actions (skip permissions)
}

// EffectiveArgs returns the final args for a CLI agent, injecting
// non-interactive and auto-accept flags for known CLI tools.
//
// Known tools and their flags:
//   - claude: --print --dangerously-skip-permissions
//   - gemini: --yolo
//   - codex:  --full-auto (if supported)
//
// This only applies when auto_accept: true in the config.
// Users can always add these flags manually in args if they prefer.
func (a Agent) EffectiveArgs() []string {
	if a.Mode != "cli" {
		return a.Args
	}

	args := make([]string, len(a.Args))
	copy(args, a.Args)

	cmd := a.Cmd

	// Always ensure non-interactive mode for pipeline usage.
	switch cmd {
	case "claude":
		if !containsAny(args, "-p", "--print") {
			args = appendFront(args, "--print")
		}
		if a.AutoAccept && !containsAny(args, "--dangerously-skip-permissions", "--permission-mode") {
			args = appendFront(args, "--dangerously-skip-permissions")
		}
	case "gemini":
		if !containsAny(args, "-p", "--prompt") {
			// For gemini, -p means non-interactive with prompt.
			// The prompt itself is appended later by the runner.
			// We need to add --prompt flag — but it expects value, so
			// we handle this in the runner. Just add yolo here.
		}
		if a.AutoAccept && !containsAny(args, "-y", "--yolo") {
			args = appendFront(args, "--yolo")
		}
	case "codex":
		if a.AutoAccept && !containsAny(args, "--full-auto", "--approval-mode") {
			args = appendFront(args, "--full-auto")
		}
	}

	return args
}

// DefaultTimeout returns the effective timeout for the agent.
func (a Agent) DefaultTimeout() int {
	if a.TimeoutSec > 0 {
		return a.TimeoutSec
	}
	return 300
}

// Load reads and parses the config file at the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Save writes the config to the given path.
func Save(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// DefaultConfig returns a starter config with example agents.
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Agents:  map[string]Agent{},
	}
}

func (c *Config) validate() error {
	for name, agent := range c.Agents {
		if agent.Mode == "" {
			return fmt.Errorf("agent %q: mode is required (cli or api)", name)
		}
		if agent.Mode != "cli" && agent.Mode != "api" {
			return fmt.Errorf("agent %q: mode must be 'cli' or 'api', got %q", name, agent.Mode)
		}
		if agent.Mode == "cli" && agent.Cmd == "" {
			return fmt.Errorf("agent %q: cmd is required for cli mode", name)
		}
		if agent.Mode == "api" && agent.Provider == "" {
			return fmt.Errorf("agent %q: provider is required for api mode", name)
		}
		if agent.Role == "" {
			return fmt.Errorf("agent %q: role is required", name)
		}
	}
	return nil
}

// containsAny checks if any of the targets exist in the slice.
func containsAny(slice []string, targets ...string) bool {
	for _, s := range slice {
		for _, t := range targets {
			if s == t {
				return true
			}
		}
	}
	return false
}

// appendFront inserts a value at the beginning of a slice.
func appendFront(slice []string, val string) []string {
	return append([]string{val}, slice...)
}

// AgentsByRole returns all agents that have the given role.
func (c *Config) AgentsByRole(role string) map[string]Agent {
	result := make(map[string]Agent)
	for name, agent := range c.Agents {
		if agent.Role == role {
			result[name] = agent
		}
	}
	return result
}
