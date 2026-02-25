// Package agent defines the interface for running AI agents and provides
// concrete adapters for CLI-based and API-based agents.
package agent

import (
	"context"
	"fmt"

	"github.com/imkarma/hive/internal/config"
)

// Request contains everything an agent needs to work on a task.
type Request struct {
	TaskID     int64  // Task ID for tracking
	Prompt     string // The full prompt with context
	WorkDir    string // Working directory (repo root)
	TimeoutSec int    // Max execution time
}

// Response is what we get back from an agent.
type Response struct {
	Output   string  // Agent's text output
	ExitCode int     // 0 = success, non-zero = failure
	Duration float64 // Execution time in seconds
	Error    error   // Any execution error
}

// Runner is the interface that all agent adapters must implement.
type Runner interface {
	// Run executes the agent with the given request and returns the response.
	Run(ctx context.Context, req Request) (*Response, error)

	// Name returns the agent's configured name.
	Name() string

	// Mode returns "cli" or "api".
	Mode() string
}

// NewRunner creates the appropriate runner based on agent config.
func NewRunner(name string, agentCfg config.Agent) (Runner, error) {
	switch agentCfg.Mode {
	case "cli":
		return NewCLIRunner(name, agentCfg), nil
	case "api":
		return NewAPIRunner(name, agentCfg)
	default:
		return nil, fmt.Errorf("unknown agent mode: %s", agentCfg.Mode)
	}
}
