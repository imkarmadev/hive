# hive

**Kanban board for AI agents.** You are the PM. Agents are your workers.

A CLI tool with an interactive TUI dashboard that lets you organize work across multiple AI agents (Claude, Gemini, Codex, Ollama — anything with a CLI or API). Create tasks, assign agents, get cross-model code reviews, and run automated fix loops — all from your terminal.

## Why

You already use AI coding agents. Maybe several. The problem:
- You copy-paste context between them manually
- You juggle multiple terminal windows
- You lose track of what's done and what's blocked
- When a review fails, you re-run everything from scratch

hive gives you a **single board** where you see all tasks, all agents, all blockers. You make decisions, agents do the work.

## How it differs from CrewAI / AutoGen / Auto-Claude

| | Others | hive |
|---|---|---|
| Philosophy | Autonomous black box | You're in control |
| Agents | Creates agents internally | Manages your existing CLI tools |
| Models | Single provider lock-in | Any mix: Claude + Gemini + Codex + local |
| Connection | API only | CLI spawn or API — your choice |
| Failure mode | Re-run entire pipeline | Fix the specific blocked task |
| Human input | Minimal / afterthought | Core feature (blockers) |

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
    timeout_sec: 900
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

# 3. Create a task
hive task create "Add JWT authentication" -p high -d "With refresh tokens"

# 4. Let PM break it down
hive plan 1

# 5. Assign agents to subtasks
hive task assign 2 claude-dev -r coder

# 6. Run the fix loop (code → review → fix → approve)
hive fix 2

# 7. Check the board
hive board

# Or open the interactive dashboard
hive ui
```

## Commands

| Command | Description |
|---------|-------------|
| `hive init` | Initialize hive in current directory |
| `hive task create "title"` | Create a task (`-p high/medium/low`, `-d "desc"`, `--parent 1`) |
| `hive task list [status]` | List tasks, filter by status |
| `hive task show <id>` | Show task details and event log |
| `hive task assign <id> <agent>` | Assign an agent (`-r role`) |
| `hive task block <id> "reason"` | Mark task as blocked |
| `hive task done <id>` | Mark task as done |
| `hive plan <id>` | PM agent breaks task into subtasks |
| `hive run <id>` | Run assigned agent on a task (`--dry` to preview prompt) |
| `hive review <id>` | Cross-model code review with git diff |
| `hive fix <id>` | Automated code → review → fix loop (`--max-loops 3`) |
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
2. **Parent task** context (if subtask)
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
  cli/              # All commands
  tui/              # Interactive dashboard (bubbletea)
  config/           # YAML config parser
  store/            # SQLite task store
  agent/            # Agent runners (CLI + API adapters)
  context/          # Prompt builder from task data
```

## Roadmap

- [x] CLI with task management and kanban board
- [x] Agent config (CLI spawn + API mode)
- [x] Context builder (task → prompt)
- [x] PM agent integration (`hive plan`)
- [x] Cross-model code review (`hive review`)
- [x] Automated fix loop (`hive fix`)
- [x] Interactive TUI dashboard (`hive ui`)
- [ ] `hive run --auto` — full pipeline without manual steps
- [ ] Parallel task execution
- [ ] Resume after crash
- [ ] Safe mode with command allowlist
- [ ] **hive PM** — premium AI agent that interviews you and creates perfect tasks

## License

MIT
