// Package worker provides parallel task execution for hive.
// It manages a pool of goroutines, each running a task's fix loop
// in its own git worktree, then merging results back.
package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/imkarma/hive/internal/agent"
	"github.com/imkarma/hive/internal/config"
	agentctx "github.com/imkarma/hive/internal/context"
	"github.com/imkarma/hive/internal/git"
	"github.com/imkarma/hive/internal/store"
)

// TaskResult holds the outcome of a single task execution.
type TaskResult struct {
	TaskID   int64
	Title    string
	Status   string // "done", "blocked", "failed"
	Duration time.Duration
	Error    error
	Log      []string // Collected log messages.
}

// Pool manages parallel task execution.
type Pool struct {
	store       *store.Store
	cfg         *config.Config
	workDir     string
	epicBranch  string
	maxWorkers  int
	maxLoops    int
	coderName   string
	coderCfg    config.Agent
	reviewName  string
	reviewCfg   config.Agent
	useWorktree bool // Whether to use git worktrees for isolation.

	mu      sync.Mutex
	results []TaskResult
}

// PoolConfig holds configuration for creating a worker pool.
type PoolConfig struct {
	Store      *store.Store
	Config     *config.Config
	WorkDir    string
	EpicBranch string
	MaxWorkers int
	MaxLoops   int
	CoderName  string
	CoderCfg   config.Agent
	ReviewName string
	ReviewCfg  config.Agent
}

// NewPool creates a new worker pool.
func NewPool(pc PoolConfig) *Pool {
	// Determine if we can use worktrees (need git repo and a branch).
	useWorktree := false
	if pc.EpicBranch != "" {
		safety := git.New(pc.WorkDir)
		useWorktree = safety.IsGitRepo()
	}

	return &Pool{
		store:       pc.Store,
		cfg:         pc.Config,
		workDir:     pc.WorkDir,
		epicBranch:  pc.EpicBranch,
		maxWorkers:  pc.MaxWorkers,
		maxLoops:    pc.MaxLoops,
		coderName:   pc.CoderName,
		coderCfg:    pc.CoderCfg,
		reviewName:  pc.ReviewName,
		reviewCfg:   pc.ReviewCfg,
		useWorktree: useWorktree,
	}
}

// Run executes all tasks in parallel (up to maxWorkers at a time)
// and returns results.
func (p *Pool) Run(tasks []store.Task) []TaskResult {
	if p.maxWorkers <= 1 || len(tasks) <= 1 {
		// Sequential execution — no worktrees needed.
		return p.runSequential(tasks)
	}
	return p.runParallel(tasks)
}

// runSequential runs tasks one by one (existing behavior).
func (p *Pool) runSequential(tasks []store.Task) []TaskResult {
	var results []TaskResult
	for _, task := range tasks {
		r := p.executeTask(task, p.workDir, false)
		results = append(results, r)
	}
	return results
}

// runParallel runs tasks concurrently using goroutines + worktrees.
func (p *Pool) runParallel(tasks []store.Task) []TaskResult {
	sem := make(chan struct{}, p.maxWorkers)
	var wg sync.WaitGroup

	results := make([]TaskResult, len(tasks))

	for i, task := range tasks {
		// Skip tasks that are already done or blocked.
		if task.Status == store.StatusDone {
			results[i] = TaskResult{
				TaskID: task.ID,
				Title:  task.Title,
				Status: "done",
				Log:    []string{"Already done"},
			}
			continue
		}
		if task.Status == store.StatusBlocked {
			results[i] = TaskResult{
				TaskID: task.ID,
				Title:  task.Title,
				Status: "blocked",
				Log:    []string{fmt.Sprintf("Blocked: %s", task.BlockedReason)},
			}
			continue
		}
		if task.AssignedAgent == "" {
			results[i] = TaskResult{
				TaskID: task.ID,
				Title:  task.Title,
				Status: "failed",
				Log:    []string{"No agent assigned"},
			}
			continue
		}

		wg.Add(1)
		sem <- struct{}{} // Acquire worker slot.

		go func(idx int, t store.Task) {
			defer wg.Done()
			defer func() { <-sem }() // Release worker slot.

			var taskWorkDir string
			var usingWorktree bool

			if p.useWorktree && p.coderCfg.Mode == "cli" {
				// Create a worktree for this task.
				wtPath := git.WorktreePath(p.workDir, t.ID)
				safety := git.New(p.workDir)

				if err := safety.AddWorktree(wtPath, p.epicBranch); err == nil {
					taskWorkDir = wtPath
					usingWorktree = true
					defer func() {
						safety.RemoveWorktree(wtPath)
						os.RemoveAll(wtPath)
						safety.PruneWorktrees()
					}()
				} else {
					// Fall back to main workdir.
					taskWorkDir = p.workDir
				}
			} else {
				taskWorkDir = p.workDir
			}

			r := p.executeTask(t, taskWorkDir, usingWorktree)

			// If using worktree, merge changes back.
			if usingWorktree && r.Status == "done" {
				safety := git.New(p.workDir)
				p.mu.Lock()
				err := safety.MergeWorktreeChanges(taskWorkDir, t.ID, t.Title)
				p.mu.Unlock()
				if err != nil {
					r.Log = append(r.Log, fmt.Sprintf("merge failed: %v", err))
					// Don't change status — code was written, merge just failed.
				} else {
					r.Log = append(r.Log, "merged into epic branch")
				}
			}

			results[idx] = r
		}(i, task)
	}

	wg.Wait()
	return results
}

// executeTask runs the fix loop for a single task.
func (p *Pool) executeTask(task store.Task, workDir string, isolated bool) TaskResult {
	start := time.Now()
	var log []string

	logf := func(format string, args ...any) {
		log = append(log, fmt.Sprintf(format, args...))
	}

	ctxBuilder := agentctx.New(p.store)

	// No reviewer — just run coder once.
	if p.reviewName == "" {
		result := p.runCoder(ctxBuilder, &task, workDir, logf)
		return TaskResult{
			TaskID:   task.ID,
			Title:    task.Title,
			Status:   result,
			Duration: time.Since(start),
			Log:      log,
		}
	}

	coderRunner, err := agent.NewRunner(p.coderName, p.coderCfg)
	if err != nil {
		logf("failed to create coder: %v", err)
		return TaskResult{TaskID: task.ID, Title: task.Title, Status: "failed", Duration: time.Since(start), Log: log, Error: err}
	}

	reviewerRunner, err := agent.NewRunner(p.reviewName, p.reviewCfg)
	if err != nil {
		logf("failed to create reviewer: %v", err)
		return TaskResult{TaskID: task.ID, Title: task.Title, Status: "failed", Duration: time.Since(start), Log: log, Error: err}
	}

	for iteration := 1; iteration <= p.maxLoops; iteration++ {
		// Re-fetch task for latest context.
		task2, _ := p.store.GetTask(task.ID)
		if task2 != nil {
			task = *task2
		}

		// === CODER ===
		p.store.UpdateTaskStatus(task.ID, store.StatusInProgress)
		logf("[%d/%d] %s coding...", iteration, p.maxLoops, p.coderName)

		coderPrompt, _ := ctxBuilder.BuildPrompt(&task, "coder")
		coderResp, err := coderRunner.Run(context.Background(), agent.Request{
			TaskID: task.ID, Prompt: coderPrompt, WorkDir: workDir, TimeoutSec: p.coderCfg.DefaultTimeout(),
		})
		if err != nil {
			p.store.UpdateTaskStatus(task.ID, store.StatusFailed)
			logf("coder error: %v", err)
			return TaskResult{TaskID: task.ID, Title: task.Title, Status: "failed", Duration: time.Since(start), Log: log, Error: err}
		}

		// Save artifact.
		artifactPath := fmt.Sprintf(".hive/runs/task-%d-parallel-code-iter%d.md", task.ID, iteration)
		os.MkdirAll(".hive/runs", 0755)
		os.WriteFile(artifactPath, []byte(coderResp.Output), 0644)
		p.store.AddArtifact(task.ID, "code", artifactPath)

		preview := coderResp.Output
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		p.store.AddEvent(task.ID, p.coderName, "agent_output", preview)

		logf("  %.1fs", coderResp.Duration)

		// Check blocked.
		if b := agent.ParseBlocked(coderResp.Output); b != "" {
			p.store.BlockTask(task.ID, b)
			logf("  BLOCKED: %s", b)
			return TaskResult{TaskID: task.ID, Title: task.Title, Status: "blocked", Duration: time.Since(start), Log: log}
		}

		if coderResp.ExitCode != 0 {
			p.store.UpdateTaskStatus(task.ID, store.StatusFailed)
			logf("  exit code %d", coderResp.ExitCode)
			return TaskResult{TaskID: task.ID, Title: task.Title, Status: "failed", Duration: time.Since(start), Log: log}
		}

		// === REVIEWER ===
		p.store.UpdateTaskStatus(task.ID, store.StatusReview)
		logf("  %s reviewing...", p.reviewName)

		reviewPrompt, _ := ctxBuilder.BuildReviewPrompt(&task)
		reviewResp, err := reviewerRunner.Run(context.Background(), agent.Request{
			TaskID: task.ID, Prompt: reviewPrompt, WorkDir: workDir, TimeoutSec: p.reviewCfg.DefaultTimeout(),
		})
		if err != nil {
			logf("  reviewer error: %v", err)
			continue
		}

		// Save artifact.
		reviewPath := fmt.Sprintf(".hive/runs/task-%d-parallel-review-iter%d.md", task.ID, iteration)
		os.WriteFile(reviewPath, []byte(reviewResp.Output), 0644)
		p.store.AddArtifact(task.ID, "review", reviewPath)

		review := agent.ParseReview(reviewResp.Output)

		switch review.Verdict {
		case "APPROVE":
			p.store.AddReview(task.ID, p.reviewName, "approve", reviewResp.Output)
			p.store.UpdateTaskStatus(task.ID, store.StatusDone)
			logf("  APPROVED (%.1fs)", reviewResp.Duration)

			// If not isolated, commit in-place.
			if !isolated {
				safety := git.New(workDir)
				if safety.IsGitRepo() {
					msg := fmt.Sprintf("hive: task #%d — %s", task.ID, task.Title)
					safety.CommitAll(msg)
				}
			}

			return TaskResult{TaskID: task.ID, Title: task.Title, Status: "done", Duration: time.Since(start), Log: log}

		case "REJECT":
			p.store.AddReview(task.ID, p.reviewName, "reject", reviewResp.Output)
			p.store.UpdateTaskStatus(task.ID, store.StatusBacklog)
			logf("  REJECTED (%.1fs)", reviewResp.Duration)
			for _, c := range review.Comments {
				logf("    • %s", c)
			}
			var comments strings.Builder
			for _, c := range review.Comments {
				comments.WriteString("- " + c + "\n")
			}
			p.store.AddEvent(task.ID, p.reviewName, "reviewed",
				fmt.Sprintf("REJECTED (iter %d):\n%s", iteration, comments.String()))

		default:
			logf("  no verdict (%.1fs)", reviewResp.Duration)
			p.store.AddEvent(task.ID, p.reviewName, "reviewed", "No clear verdict")
		}
	}

	p.store.UpdateTaskStatus(task.ID, store.StatusFailed)
	logf("max iterations reached")
	return TaskResult{TaskID: task.ID, Title: task.Title, Status: "failed", Duration: time.Since(start), Log: log}
}

// runCoder runs coder agent once without review.
func (p *Pool) runCoder(ctxBuilder *agentctx.Builder, task *store.Task, workDir string, logf func(string, ...any)) string {
	runner, err := agent.NewRunner(p.coderName, p.coderCfg)
	if err != nil {
		logf("failed to create coder: %v", err)
		return "failed"
	}

	p.store.UpdateTaskStatus(task.ID, store.StatusInProgress)
	logf("%s coding...", p.coderName)

	prompt, _ := ctxBuilder.BuildPrompt(task, "coder")
	resp, err := runner.Run(context.Background(), agent.Request{
		TaskID: task.ID, Prompt: prompt, WorkDir: workDir, TimeoutSec: p.coderCfg.DefaultTimeout(),
	})
	if err != nil {
		p.store.UpdateTaskStatus(task.ID, store.StatusFailed)
		logf("error: %v", err)
		return "failed"
	}

	if b := agent.ParseBlocked(resp.Output); b != "" {
		p.store.BlockTask(task.ID, b)
		logf("BLOCKED: %s", b)
		return "blocked"
	}

	if resp.ExitCode != 0 {
		p.store.UpdateTaskStatus(task.ID, store.StatusFailed)
		logf("exit code %d", resp.ExitCode)
		return "failed"
	}

	p.store.UpdateTaskStatus(task.ID, store.StatusDone)
	logf("done (%.1fs)", resp.Duration)
	return "done"
}
