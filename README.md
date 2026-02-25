# hive

**Kanban board for AI agents.** You are the PM. Agents are your workers.

A CLI tool with an interactive TUI dashboard that lets you organize work across multiple AI agents (Claude, Gemini, Codex, Ollama — anything with a CLI or API). Create epics, let a PM agent break them into tasks, get cross-model code reviews, and run automated fix loops — all from your terminal. Every epic runs on a git safety branch so you can accept or reject all agent work with a single command.

## Why

You already use AI coding agents. Maybe several. The problem:
- You copy-paste context between them manually
- You juggle multiple terminal windows
- You lose track of what's done and what's blocked
- When a review fails, you re-run everything from scratch

hive gives you a **single board** where you see all epics and tasks, all agents, all blockers. You make decisions, agents do the work. And every change is reversible — agents work on safety branches, you review the diff and accept or reject.

## How it differs from CrewAI / AutoGen / Auto-Claude

| | Others | hive |
|---|---|---|
| Philosophy | Autonomous black box | You're in control |
| Agents | Creates agents internally | Manages your existing CLI tools |
| Models | Single provider lock-in | Any mix: Claude + Gemini + Codex + local |
| Connection | API only | CLI spawn or API — your choice |
| Failure mode | Re-run entire pipeline | Fix the specific blocked task |
| Safety | Hope for the best | Git safety branches — accept or reject all changes |
| Human input | Minimal / afterthought | Core feature (blockers + accept/reject) |

## Install

```bash
# Build from source (requires Go 1.21+)
git clone https://github.com/imkarma/hive.git
cd hive
go build -o hive ./cmd/hive/

# Optional: move to PATH
sudo mv hive /usr/local/bin/
```

## Interactive Dashboard

Run `hive ui` to open a lazygit-style interactive TUI:

```
 hive — 5 tasks

 BACKLOG (2)              IN PROGRESS (1)          BLOCKED (1)              REVIEW (0)     DONE (1)
╭──────────────────────╮ ╭──────────────────────╮ ╭──────────────────────╮                ╭──────────────────────╮
│ ● #3 Setup DB        │ │ ● #1 Add JWT auth    │ │ ● #2 Write tests     │                │ ○ #4 API docs        │
│ [claude-dev]         │ │ [claude-dev]         │ │ ⚠ vitest or jest?    │                │                      │
╰──────────────────────╯ ╰──────────────────────╯ ╰──────────────────────╯                ╰──────────────────────╯
╭──────────────────────╮
│ ● #5 Code review     │
╰──────────────────────╯

⚠ Blockers
  #2: Which test framework: vitest or jest?

↑↓←→/hjkl navigate  enter detail  n new task  a answer blocker  s start  d done  q quit
```

### TUI Hotkeys

| Key | Action |
|-----|--------|
| `↑↓←→` / `hjkl` | Navigate the board |
| `enter` | Open task detail (events, agent info) |
| `n` / `c` | Create new task |
| `a` | Answer a blocker |
| `s` | Start task (→ in_progress) |
| `d` | Mark as done |
| `r` | Move to review |
| `b` | Move to backlog |
| `ctrl+p` | Cycle priority (in create dialog) |
| `R` | Refresh board |
| `esc` | Back |
| `q` | Quit |

Everything you can do with CLI commands, you can do from the dashboard without leaving it.

## Quick Start

```bash
# 1. Initialize in your project
cd your-project
hive init

# 2. Configure your agents (.hive/config.yaml)
cat > .hive/config.yaml << 'EOF'
version: 1
agents:
  claude-dev:
    role: coder
    mode: cli
    cmd: "claude"
    args: ["--model", "sonnet"]
    auto_accept: true
    timeout_sec: 900
  gemini-pm:
    role: pm
    mode: cli
    cmd: "gemini"
    args: ["--model", "2.5-pro"]
    auto_accept: true
    timeout_sec: 300
  gpt-reviewer:
    role: reviewer
    mode: api
    provider: openai
    model: "gpt-4o"
    api_key_env: "OPENAI_API_KEY"
    timeout_sec: 600
EOF

# 3. Create an epic and run the full pipeline
hive epic create "Add JWT authentication" -p high -d "With refresh tokens"
hive auto 1

# PM plans → agents code → reviewer approves → you review the diff.
# All work happens on a safety branch (hive/epic-1).

# When done, review and accept:
hive epic diff 1      # see all changes
hive epic accept 1    # merge into main
# Or reject:
hive epic reject 1    # discard everything, back to clean main

# Or do it step by step:
hive epic create "Add JWT auth" -p high
hive plan 1
hive task assign 2 claude-dev -r coder
hive fix 2
hive epic accept 1
```

## Commands

### Epics (you create these)

| Command | Description |
|---------|-------------|
| `hive epic create "title"` | Create an epic (`-p high/medium/low`, `-d "desc"`). Creates a git safety branch. |
| `hive epic list [status]` | List all epics with task progress |
| `hive epic show <id>` | Show epic details, tasks, and change summary |
| `hive epic diff <id>` | Show full diff of all agent work on this epic |
| `hive epic accept <id>` | Merge safety branch into main — accept all changes |
| `hive epic reject <id>` | Delete safety branch — discard all agent work |

### Tasks (PM agent creates these, or you create manually)

| Command | Description |
|---------|-------------|
| `hive task create "title"` | Create a task (`-p`, `-d`, `--parent`) |
| `hive task list [status]` | List tasks, filter by status |
| `hive task show <id>` | Show task details and event log |
| `hive task assign <id> <agent>` | Assign an agent (`-r role`) |
| `hive task block <id> "reason"` | Mark task as blocked |
| `hive task done <id>` | Mark task as done |

### Pipeline

| Command | Description |
|---------|-------------|
| `hive auto <id>` | Full pipeline: plan → assign → code → review → done (`--parallel N`) |
| `hive plan <id>` | PM agent breaks epic/task into subtasks |
| `hive run <id>` | Run assigned agent on a task (`--dry` to preview) |
| `hive review <id>` | Cross-model code review with git diff |
| `hive fix <id>` | Code → review → fix loop (`--max-loops 3`) |
| `hive resume [run-id]` | Resume an interrupted pipeline (crash recovery) |

### General

| Command | Description |
|---------|-------------|
| `hive init` | Initialize hive in current directory |
| `hive answer <id> "text"` | Answer a blocker |
| `hive board` | Show kanban board |
| `hive status` | Quick status overview |
| `hive log <id>` | Show event log for a task |
| `hive ui` | Open interactive TUI dashboard |

## Agent Configuration

Two modes for connecting agents:

### CLI mode (spawn process)

Uses your existing CLI tools and subscriptions. No extra API costs.

```yaml
claude-dev:
  role: coder
  mode: cli
  cmd: "claude"
  args: ["--model", "sonnet"]
  auto_accept: true       # adds --print --dangerously-skip-permissions automatically
  timeout_sec: 900
```

### API mode (HTTP call)

Direct API calls. Supports OpenAI, Anthropic, and Google.

```yaml
gpt-reviewer:
  role: reviewer
  mode: api
  provider: openai        # openai | anthropic | google
  model: "gpt-4o"
  api_key_env: "OPENAI_API_KEY"
  timeout_sec: 600
```

### Auto-accept mode

For automated pipelines (`hive auto`, `hive fix`), agents need to run non-interactively. Set `auto_accept: true` and hive automatically injects the right flags for known tools:

| Tool | Flags added |
|------|-------------|
| `claude` | `--print` (always) + `--dangerously-skip-permissions` (with auto_accept) |
| `gemini` | `--yolo` |
| `codex` | `--full-auto` |

If you already have these flags in `args`, hive won't duplicate them. For unknown CLI tools, `auto_accept` is a no-op — add your own flags in `args`.

### Roles

You assign roles — hive doesn't decide for you.

| Role | What it does |
|------|-------------|
| `pm` | Breaks tasks into subtasks (used by `hive plan`) |
| `coder` | Implements tasks (used by `hive run` and `hive fix`) |
| `reviewer` | Reviews code (used by `hive review` and `hive fix`) |
| `tester` | Runs tests |
| `analyst` | Analyzes requirements |
| Any custom | Your own roles — hive doesn't restrict you |

## The Fix Loop

The core workflow. One command does code → review → fix until approved:

```
$ hive fix 2

═══ Fix Loop: Task #2 ═══
  Coder:    claude-dev
  Reviewer: codex-reviewer
  Max loops: 3

── Iteration 1/3 ──

[coder] claude-dev working...
  Done (45.2s)
[reviewer] codex-reviewer reviewing...
  ✗ REJECTED (12.1s)
    • auth.go:42: Missing input validation
    • No error handling for expired tokens

  Retrying... (iteration 2/3)

── Iteration 2/3 ──

[coder] claude-dev working...
  Done (38.7s)
[reviewer] codex-reviewer reviewing...
  ✓ APPROVED (10.3s)
    • Issues fixed, good error handling now

═══ Task #2 completed in 2 iteration(s) ═══
```

## Epics and Tasks

hive uses a two-level hierarchy:

- **Epic** — a high-level feature you describe as the PM. Created with `hive epic create`.
- **Task** — an actionable work item the PM agent generates from your epic. Created by `hive plan`.

You think in epics ("Add JWT auth with refresh tokens"). The PM agent thinks in tasks ("create middleware", "write migration", "add tests"). You review and accept/reject at the epic level.

## Git Safety Net

Every epic gets its own git branch. All agent work happens there. Nothing touches your main branch until you explicitly accept.

```
main
  └── hive/epic-1           ← agents work here
        task #2: auth middleware (committed)
        task #3: database migration (committed)
        task #4: tests (committed)

user reviews: hive epic diff 1
user accepts: hive epic accept 1   → merges into main
user rejects: hive epic reject 1   → deletes branch, main untouched
```

This means `auto_accept: true` stops being scary. Agents can run in full-auto mode because every change is reversible.

## Auto Pipeline

One command to rule them all. `hive auto` runs the full pipeline end-to-end:

```
$ hive epic create "Add JWT authentication" -p high -d "With refresh tokens"
Created epic #1: Add JWT authentication [high]
  Branch: hive/epic-1 (safety net — all agent work happens here)

$ hive auto 1

╔══════════════════════════════════════╗
║  hive auto — full pipeline           ║
╚══════════════════════════════════════╝

  Epic:     #1 Add JWT authentication
  Branch:   hive/epic-1
  PM:       gemini-pm
  Coder:    claude-dev
  Reviewer: codex-reviewer
  Max fix loops: 3

═══ 1: PLAN — Breaking epic into tasks

  Running gemini-pm...
  #2 Setup database schema [high]
  #3 Implement auth middleware [high]
  #4 Create login endpoint [medium]
  Created 3 tasks

═══ 2: ASSIGN — Assigning agents to tasks

  #2 → claude-dev (coder)
  #3 → claude-dev (coder)
  #4 → claude-dev (coder)

═══ 3: WORK 1/3 — #2: Setup database schema

  [1/3] claude-dev coding... 32.1s → codex-reviewer reviewing... ✗ REJECTED (8.2s)
    • Missing foreign key constraint
  [2/3] claude-dev coding... 28.4s → codex-reviewer reviewing... ✓ APPROVED (7.1s)
    committed

═══ 3: WORK 2/3 — #3: Implement auth middleware
  ...

╔══════════════════════════════════════╗
║  Pipeline complete                   ║
╚══════════════════════════════════════╝

  Total tasks: 3
  ✓ Completed: 3

  All tasks complete!
  Committed changes on hive/epic-1

  Changes:
    src/auth.go          | 142 +++++++++
    src/auth_test.go     |  87 ++++++
    src/middleware.go     |  45 +++
    3 files changed, 274 insertions(+)

  Review and accept: hive epic accept 1
  Or reject:         hive epic reject 1
  View full diff:    hive epic diff 1
```

Flags:
- `--max-loops 3` — max fix-review iterations per task (default: 3)
- `--skip-plan` — skip planning, run on existing tasks (useful after answering blockers)
- `--parallel N` — run N tasks in parallel using git worktrees

If an agent hits a blocker, the pipeline pauses. Answer it and resume:

```bash
hive answer 3 "Use PostgreSQL with UUID primary keys"
hive auto 1 --skip-plan   # continues where it left off
```

## Parallel Execution

By default, tasks run one at a time. With `--parallel`, multiple CLI agents work simultaneously — each in its own git worktree so they don't overwrite each other's files:

```bash
hive auto 1 --parallel 3   # 3 tasks at once
```

How it works:
1. hive creates a git worktree per task (an independent working directory sharing the same repo)
2. Each agent works in its isolated worktree
3. When a task is approved, changes are cherry-picked back to the epic branch
4. Worktrees are cleaned up automatically

This is especially useful when you have many independent tasks (e.g., "add tests to 5 modules") that don't conflict with each other.

API-mode agents (reviewers via HTTP) are naturally parallel-safe. CLI-mode agents (Claude, Gemini, Codex) get worktree isolation automatically.

## Crash Recovery

Pipelines can be interrupted by Ctrl+C, crashes, or system restarts. hive tracks every `auto` run in the database, so you can always pick up where you left off.

```bash
# See what was interrupted
hive resume

# Example output:
#   Interrupted pipeline runs
#
#   Run #3  E#1 Add JWT authentication
#     Started:  2026-02-25 14:30:22 (2h ago)
#     Settings: max-loops=3 parallel=2
#     Tasks:    1 done, 1 stuck in_progress, 1 backlog
#
#   Resume with: hive resume <run-id>

# Resume it — resets stuck tasks and re-runs with same settings
hive resume 3
```

What happens when you resume:
1. Tasks stuck in `in_progress` or `review` (from the crash) are reset to `backlog`
2. The old run is marked as `interrupted`
3. `hive auto` re-runs on the same epic with `--skip-plan` and the same `--max-loops` / `--parallel` settings
4. Already-done tasks are skipped automatically

If you run `hive auto` on an epic that has an interrupted pipeline, hive warns you and suggests `hive resume` instead.

## Blockers

When an agent is unsure, it says `BLOCKED: question`. hive catches this, stops the task, and waits for your answer:

```
$ hive board

 BACKLOG (2)     IN PROGRESS (1)  BLOCKED (1)       REVIEW (0)    DONE (1)
 ──────────────────────────────────────────────────────────────────────────
 #4 Write tests  #3 Auth API      #2 DB schema                   #1 Plan
    [claude]        [claude]          ⚠ REST or GraphQL?            [gemini]

⚠  Blockers (need your input)
  #2: REST or GraphQL?
       → hive answer 2 "your answer"

$ hive answer 2 "REST with OpenAPI spec"
```

The agent gets your answer as context on the next run.

## Context Passing

No magic prompt chains. Context = the task itself:

1. **Task description** and acceptance criteria
2. **Epic context** (the high-level feature this task belongs to)
3. **User answers** to blockers
4. **Previous review comments** (in fix loop)
5. **Git diff** (for code reviews)
6. **Role-specific instructions**

Like a developer reading a Jira ticket — everything they need is in the task.

## Project Structure

```
.hive/
  config.yaml       # Agent configuration
  hive.db           # SQLite database (tasks, events, artifacts)
  runs/             # Agent output artifacts
```

```
cmd/hive/           # CLI entry point
internal/
  cli/              # All commands (epic, task, plan, auto, fix, review, board, etc.)
  tui/              # Interactive dashboard (bubbletea)
  config/           # YAML config parser
  store/            # SQLite store (epics, tasks, events, artifacts, reviews)
  agent/            # Agent runners (CLI + API adapters)
  context/          # Prompt builder from task data
  git/              # Git safety net (branch per epic, commit per task, accept/reject)
  worker/           # Parallel task execution (worker pool, worktree isolation)
```

## Roadmap

- [x] CLI with task management and kanban board
- [x] Agent config (CLI spawn + API mode)
- [x] Context builder (task → prompt)
- [x] PM agent integration (`hive plan`)
- [x] Cross-model code review (`hive review`)
- [x] Automated fix loop (`hive fix`)
- [x] Interactive TUI dashboard (`hive ui`)
- [x] Full autonomous pipeline (`hive auto`)
- [x] Epic/Task hierarchy (user creates epics, PM creates tasks)
- [x] Git safety net (branch per epic, accept/reject workflow)
- [x] Parallel task execution (git worktrees, `--parallel N`)
- [x] Resume after crash (`hive resume`)
- [ ] **hive hub** — web dashboard to see all your hive projects in one place
- [ ] **hive PM** — premium AI agent that interviews you and creates perfect tasks

## License

MIT
