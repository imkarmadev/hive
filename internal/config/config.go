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
