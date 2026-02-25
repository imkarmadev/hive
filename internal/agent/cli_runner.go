package agent

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/imkarma/hive/internal/config"
)

// CLIRunner spawns an external CLI process (claude, gemini, codex, ollama, etc.)
// and passes the task prompt via stdin or as an argument.
type CLIRunner struct {
	name string
	cfg  config.Agent
}

// NewCLIRunner creates a runner that spawns CLI processes.
func NewCLIRunner(name string, cfg config.Agent) *CLIRunner {
	return &CLIRunner{name: name, cfg: cfg}
}

func (r *CLIRunner) Name() string { return r.name }
func (r *CLIRunner) Mode() string { return "cli" }

// Run spawns the CLI agent process with the prompt.
//
// The prompt is passed as the last argument to the command.
// For example, if cmd="claude" and args=["--model", "sonnet"],
// the full command becomes: claude --model sonnet "the prompt text"
//
// The agent runs in the specified working directory (repo root)
// so it has access to the project files.
func (r *CLIRunner) Run(ctx context.Context, req Request) (*Response, error) {
	start := time.Now()

	// Build the command: cmd + args + prompt as last arg.
	args := make([]string, len(r.cfg.Args))
	copy(args, r.cfg.Args)

	// Append prompt as the final argument.
	// Most CLI agents accept the prompt as a positional argument.
	args = append(args, req.Prompt)

	// Apply timeout from config or request.
	timeout := time.Duration(r.cfg.DefaultTimeout()) * time.Second
	if req.TimeoutSec > 0 {
		timeout = time.Duration(req.TimeoutSec) * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.cfg.Cmd, args...)
	cmd.Dir = req.WorkDir

	// Capture stdout and stderr.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the process.
	err := cmd.Run()

	duration := time.Since(start).Seconds()

	resp := &Response{
		Output:   stdout.String(),
		Duration: duration,
	}

	if err != nil {
		// Check if it's a timeout.
		if ctx.Err() == context.DeadlineExceeded {
			resp.Error = fmt.Errorf("agent %s timed out after %ds", r.name, int(timeout.Seconds()))
			resp.ExitCode = -1
			return resp, resp.Error
		}

		// Get exit code if available.
		if exitErr, ok := err.(*exec.ExitError); ok {
			resp.ExitCode = exitErr.ExitCode()
		} else {
			resp.ExitCode = -1
		}

		// Include stderr in error context.
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			resp.Error = fmt.Errorf("agent %s exited with code %d: %s", r.name, resp.ExitCode, stderrStr)
		} else {
			resp.Error = fmt.Errorf("agent %s exited with code %d: %w", r.name, resp.ExitCode, err)
		}

		// Still return the response — partial output may be useful.
		return resp, nil
	}

	resp.ExitCode = 0
	return resp, nil
}

// CLIAvailable checks if the CLI command exists in PATH.
func CLIAvailable(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}
