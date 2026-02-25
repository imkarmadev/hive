# hive

**Kanban board for AI agents.** You are the PM. Agents are your workers.

A CLI tool that lets you organize work across multiple AI agents (Claude, Gemini, Codex, Ollama — anything with a CLI or API). Create epics, let a PM agent break them into tasks, an architect agent research the codebase and write technical specs, then coders implement and reviewers approve — all from your terminal. Every epic runs on a git safety branch so you can accept or reject all agent work with a single command.

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
    timeout_sec: 900
  claude-arch:
    role: architect
    mode: cli
    cmd: "claude"
    args: ["--model", "sonnet"]
    timeout_sec: 600
  gemini-pm:
    role: pm
    mode: cli
    cmd: "gemini"
    args: ["--model", "2.5-pro"]
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

# 4. When done, review and accept or reject the whole epic:
hive epic diff 1      # see all changes
hive epic accept 1    # merge into main
hive epic reject 1    # discard everything, back to clean main
```

Note: `auto_accept` is no longer needed in config — `hive auto`, `hive fix`, `hive run`, `hive plan`, and `hive review` all force auto-accept automatically for CLI agents.

## Auto Pipeline

One command to rule them all:

```
$ hive auto 1

╔══════════════════════════════════════╗
║  hive auto — full pipeline           ║
╚══════════════════════════════════════╝

  Epic:     #1 Add JWT authentication
  Branch:   hive/epic-1

═══ 1: PLAN — Breaking epic into tasks
  #2 Setup database schema [high]
  #3 Implement auth middleware [high]
  #4 Create login endpoint [medium]
  Created 3 tasks

═══ 2.5: ARCHITECT — Technical research & spec
  #2 — ✓ spec written
  #3 — ✓ spec written
  #4 — ⚠ BLOCKED

═══ 3: WORK 1/3 — #2: Setup database schema
  [1/3] claude-dev coding... 32.1s → reviewer... ✓ APPROVED
    committed

═══ 3: WORK 2/3 — #3: Implement auth middleware
  [1/3] claude-dev coding... 45.2s → reviewer... ✗ REJECTED
  [2/3] claude-dev coding... 38.7s → reviewer... ✓ APPROVED
    committed

═══ 3: WORK 3/3 — #4: Create login endpoint
  ⚠ Blocked: Which auth strategy: session or JWT?
  → hive answer 4 "..."
```

### Smart resume

Running `hive auto 1` again on the same epic **does not re-plan**. It detects existing tasks and picks up where it left off — completed tasks are skipped, blocked tasks stay blocked, remaining tasks continue through the pipeline. No `--skip-plan` needed.

### Flags

- `--max-loops 3` — max fix-review iterations per task (default: 3)
- `--skip-architect` — skip architect research
- `--parallel N` — run N tasks in parallel using git worktrees

## Blocker Flow

When an agent is unsure, it says `BLOCKED: question`. hive catches this and pauses that task. The rest of the epic continues.

```bash
# Answer the blocker — this automatically continues the pipeline for that task:
# architect re-runs → coder implements → reviewer approves
hive answer 4 "Use JWT with refresh tokens"

# Don't know the answer? Cancel the task — epic continues without it:
hive answer 4 skip
# Or explicitly:
hive task cancel 4
```

`hive answer` is not just an unblock — it's a full auto-continue:
1. Unblocks the task with your answer
2. Re-runs architect (if it was the architect who blocked) — architect may block again with follow-up questions
3. If architect is satisfied → coder → reviewer loop
4. Commits approved work on the epic's safety branch

## Epic Accept/Reject

You can only accept an epic when **all tasks are done or cancelled**. No accidental merges of half-finished work.

```bash
hive epic accept 1
# ✗ Cannot accept — 2 task(s) not finished:
#   #4   blocked      Create login endpoint
#   #5   backlog      Write integration tests
#
# Finish, cancel, or answer blocked tasks first.

hive task cancel 4
hive answer 5 "Use vitest"
# ... pipeline runs ...

hive epic accept 1
# ✓ Merged into main
# ✓ Epic #1 done
```

## Interactive Dashboard

Run `hive ui` for a TUI dashboard with epic cards, pipeline progress, and blocker resolution:

```
 hive board — 3 epics

╭──────────────────────────╮ ╭──────────────────────────╮ ╭──────────────────────────╮
│ E#1  backlog             │ │ E#2  backlog             │ │ E#3  done                │
│ Add JWT auth             │ │ Fix security vulns       │ │ Setup CI/CD              │
│ No description           │ │ Explore and fix issues   │ │ GitHub Actions pipeline  │
│ ● ── ● ── ◉ ── ○ ── ○   │ │ ● ── ◉ ── ○ ── ○ ── ○   │ │ ● ── ● ── ● ── ● ── ●   │
│ plan arch code review acc│ │ plan arch code review acc│ │ plan arch code review acc│
│ Tasks: 2/3 done          │ │ Tasks: 0/5 done          │ │ Tasks: 4/4 done          │
│ ⚠ BLOCKED #4: JWT?       │ │ claude-dev: working...   │ │ ✓ Accepted               │
╰──────────────────────────╯ ╰──────────────────────────╯ ╰──────────────────────────╯
```

### TUI Hotkeys

| Key | Action |
|-----|--------|
| `↑↓←→` / `hjkl` | Navigate the grid |
| `enter` / `space` | Open epic detail (task list, log) |
| `c` | Create new epic |
| `d` | View diff |
| `r` | Resolve blocker |
| `y` | Accept epic (merge) |
| `n` | Reject epic (discard) |
| `e` | Request changes (from diff view) |
| `H` | View history / timeline |
| `R` | Refresh |
| `esc` | Back |
| `q` | Quit |

## Commands

### Epics

| Command | Description |
|---------|-------------|
| `hive epic create "title"` | Create an epic (`-p high/medium/low`, `-d "desc"`). Creates a git safety branch. |
| `hive epic list [status]` | List all epics with task progress |
| `hive epic show <id>` | Show epic details, tasks, and change summary |
| `hive epic diff <id>` | Show full diff of all agent work on this epic |
| `hive epic accept <id>` | Merge safety branch into main (requires all tasks done/cancelled) |
| `hive epic reject <id>` | Delete safety branch — discard all agent work |

### Tasks

| Command | Description |
|---------|-------------|
| `hive task create "title"` | Create a task (`-p`, `-d`, `--parent`) |
| `hive task list [status]` | List tasks, filter by status |
| `hive task show <id>` | Show task details and event log |
| `hive task assign <id> <agent>` | Assign an agent (`-r role`) |
| `hive task block <id> "reason"` | Mark task as blocked |
| `hive task done <id>` | Mark task as done |
| `hive task cancel <id>` | Cancel task — pipeline skips it, epic can be accepted without it |

### Pipeline

| Command | Description |
|---------|-------------|
| `hive auto <id>` | Full pipeline: plan → architect → code → review. Smart resume if tasks exist. (`--parallel N`, `--skip-architect`) |
| `hive plan <id>` | PM agent breaks epic/task into subtasks |
| `hive run <id>` | Run assigned agent on a task (`--dry` to preview prompt) |
| `hive review <id>` | Cross-model code review with git diff |
| `hive fix <id>` | Code → review → fix loop (`--max-loops 3`) |
| `hive answer <id> "text"` | Answer a blocker and auto-continue the pipeline. Use `skip` to cancel the task. |
| `hive resume [run-id]` | Resume an interrupted pipeline (crash recovery) |

### General

| Command | Description |
|---------|-------------|
| `hive init` | Initialize hive in current directory |
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
  timeout_sec: 900
```

hive auto-injects the right non-interactive flags for known tools:

| Tool | Flags added automatically |
|------|--------------------------|
| `claude` | `--print --dangerously-skip-permissions` |
| `gemini` | `--yolo` |
| `codex` | `--full-auto` |

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

### Roles

You assign roles — hive doesn't decide for you.

| Role | What it does | Used by |
|------|-------------|---------|
| `pm` | Breaks epics into actionable tasks | `hive plan`, `hive auto` |
| `architect` | Researches codebase, writes technical specs | `hive auto` |
| `coder` | Implements tasks following the spec | `hive run`, `hive fix`, `hive auto` |
| `reviewer` | Reviews code changes | `hive review`, `hive fix`, `hive auto` |

## Git Safety Net

Every epic gets its own git branch. All agent work happens there. Nothing touches your main branch until you explicitly accept.

```
main
  └── hive/epic-1           ← agents work here
        task #2: committed
        task #3: committed
        task #4: cancelled (skipped)

hive epic diff 1     → see all changes
hive epic accept 1   → merge into main
hive epic reject 1   → delete branch, main untouched
```

## Context Passing

No magic prompt chains. Context = the task itself:

1. **Task description** and acceptance criteria
2. **Epic context** (the high-level feature this task belongs to)
3. **Architect spec** (technical plan from the architect phase)
4. **User answers** to blockers
5. **Previous review comments** (in fix loop)
6. **Git diff** (for code reviews)
7. **Role-specific instructions**

Like a developer reading a Jira ticket — everything they need is in the task.

## Parallel Execution

With `--parallel`, multiple CLI agents work simultaneously — each in its own git worktree:

```bash
hive auto 1 --parallel 3   # 3 tasks at once
```

Each agent works in an isolated worktree. When a task is approved, changes are cherry-picked back to the epic branch. Worktrees are cleaned up automatically.

## Crash Recovery

```bash
hive resume          # list interrupted pipelines
hive resume 3        # resume a specific run
```

Resets stuck tasks, marks the old run as interrupted, and re-runs `hive auto` with the same settings.

## Task Statuses

| Status | Meaning |
|--------|---------|
| `backlog` | Not started |
| `in_progress` | Agent is working on it |
| `blocked` | Agent needs your input |
| `review` | Under code review |
| `done` | Completed and approved |
| `failed` | Failed after max retries |
| `cancelled` | Skipped by user decision |

## Project Structure

```
.hive/
  config.yaml       # Agent configuration
  hive.db           # SQLite database (tasks, events, artifacts)
  runs/             # Agent output artifacts

cmd/hive/           # CLI entry point
internal/
  cli/              # All commands
  tui/              # Interactive dashboard (bubbletea)
  config/           # YAML config parser
  store/            # SQLite store
  agent/            # Agent runners (CLI + API)
  context/          # Prompt builder
  git/              # Git safety net
  worker/           # Parallel execution
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
- [x] Epic/Task hierarchy
- [x] Git safety net (branch per epic, accept/reject)
- [x] Parallel execution (git worktrees)
- [x] Crash recovery (`hive resume`)
- [x] Smart resume (auto-detect existing tasks)
- [x] Auto-continue on blocker answer (`hive answer`)
- [x] Task cancel (`hive task cancel`)
- [x] Epic accept guard (all tasks must be done/cancelled)
- [ ] **hive hub** — web dashboard for multi-project view
- [ ] **hive PM** — premium AI agent that interviews you and creates perfect tasks

## License

MIT
