# hive — Kanban for AI Agents

## Vision

A CLI tool that gives developers a **kanban board for AI agents**.
The user is the **Product Owner** — they know the project, they assign roles,
they make decisions. Agents are **workers** who take tasks from the board.

**Not** an autonomous black box. **Not** a framework that creates agents.
A management tool for agents that already exist (Claude CLI, Gemini CLI, Codex CLI, or any CLI/API).

## Core Principles

1. **User is the PM.** User assigns roles, prioritizes, unblocks. Agents work.
2. **Two connection modes:** spawn CLI process OR API key. User decides per agent.
3. **Roles assigned by user**, not system. The system provides tools, not opinions.
4. **Human-in-the-loop at every step.** Agents ask when unsure (blockers), user answers.
5. **Transparency.** Board shows everything: status, progress, blockers, artifacts.
6. **CLI-first.** TUI dashboard later. Core value works from plain terminal.

## How It Works

```
User creates a task
  -> PM agent (or user manually) breaks it into subtasks
  -> Subtasks go on the board with priorities and dependencies
  -> Assigned agents pick up tasks and work
  -> If agent is unsure -> BLOCKED status + question to user
  -> User sees blockers on board, answers them
  -> Agent continues
  -> Completed work goes to review (cross-model)
  -> If rejected -> fix loop (with iteration limit)
  -> Done -> summary + artifacts
```

## Key Differentiator

| Others (CrewAI, AutoGen) | hive |
|---------------------------|------|
| User presses "go" and hopes | User sees everything, decides everything |
| Black box pipeline | Transparent kanban board |
| Creates agents internally | Manages existing CLI agents |
| API-only, expensive | CLI-first, use your subscriptions |
| Bad result = rerun everything | Bad result = fix the specific blocked task |
| No human-in-the-loop | Human-in-the-loop is the core feature |

## Agent Configuration

```yaml
# .hive/config.yaml
version: 1

agents:
  gemini-pm:
    role: pm
    mode: cli                      # spawn process
    cmd: "gemini"
    args: ["--model", "2.5-pro"]
    timeout_sec: 300

  claude-dev:
    role: coder
    mode: cli
    cmd: "claude"
    args: ["--model", "sonnet"]
    timeout_sec: 900

  gpt-reviewer:
    role: reviewer
    mode: api                      # via API key
    provider: openai
    model: "gpt-4o"
    api_key_env: "OPENAI_API_KEY"
    timeout_sec: 600

  local-llm:
    role: tester
    mode: cli                      # even local models
    cmd: "ollama"
    args: ["run", "codellama"]
    timeout_sec: 600
```

## CLI Commands

```bash
hive init                           # create .hive/ in repo
hive config                         # configure agents and roles
hive task "Add JWT authentication"  # create a task
hive plan 1                         # PM agent breaks task #1 into subtasks
hive board                          # show kanban board
hive run                            # start workers
hive run --auto                     # full auto with notifications on blockers
hive answer 3 "Use jose library"    # answer a blocker on task #3
hive status                         # quick status overview
hive log 2                          # show event log for task #2
hive artifacts 2                    # show artifacts for task #2
```

## Board View

```
hive board

  BACKLOG       IN PROGRESS    REVIEW        DONE
 ─────────────────────────────────────────────────
  #4 Write      #2 Implement   #5 Review     #1 Analyze
     tests         JWT auth       auth API      requirements
     [tester]      [claude]       [codex]       [gemini] 3m
     pri: med       pri: high     pri: high     
                                                
  #3 Add API                                   
     endpoints                                 
     BLOCKED                                   
     "REST or GraphQL?"                        
     waiting for user                          
```

## Architecture

```
cmd/hive/          CLI entry point (cobra)
internal/
  config/          YAML config parser
  store/           SQLite task store (tasks, events, artifacts)
  agent/           Agent runner interface + adapters (cli, api)
  board/           Board rendering (terminal, later TUI)
  orchestrator/    Task assignment, blockers, fix-loop logic
  context/         Context builder for agent prompts
.hive/
  config.yaml      Agent configuration
  hive.db          SQLite database
  runs/            Artifacts per task
```

## Data Model (SQLite)

```sql
tasks:      id, parent_id, title, description, status, assigned_agent,
            role, priority, blocked_reason, created_at, updated_at
events:     id, task_id, agent, event_type, content, timestamp
artifacts:  id, task_id, type, file_path, timestamp
reviews:    id, task_id, reviewer_agent, verdict, comments, timestamp
```

Task statuses: `backlog`, `in_progress`, `blocked`, `review`, `done`, `failed`

## Context Passing (The Key Problem)

Context for each agent is built from the **task itself**:

1. **Original task description** + acceptance criteria
2. **Parent task** context (if subtask)
3. **Comments/answers** from user (blocker resolutions)
4. **Artifacts** from dependent tasks (git diffs, plans, review comments)
5. **Repository state** (agent has access to filesystem)

No magic. No prompt injection chains. Agent reads its task — like a developer reads a Jira ticket.

## Phases

### Phase 0 — Working Skeleton
- [ ] CLI scaffold (hive init, task, board, answer)
- [ ] SQLite task store with statuses and blockers
- [ ] Agent config (YAML, cli + api modes)
- [ ] Agent runner (spawn CLI process)
- [ ] Simple board output in terminal
- [ ] Basic flow: task -> code -> review (manual assignment)

### Phase 1 — Automation
- [ ] PM agent integration (plan command)
- [ ] Auto-assignment based on roles
- [ ] Fix-loop on review reject
- [ ] Cross-model review
- [ ] Resume after crash
- [ ] Event log and audit trail

### Phase 2 — Polish
- [ ] TUI dashboard (bubbletea)
- [ ] Notifications on blockers
- [ ] Parallel task execution
- [ ] Safe mode with command allowlist
- [ ] Multiple repos support

### Phase 3 — Monetization
- [ ] hive PM — premium AI agent that:
  - Interviews user about the project intelligently
  - Asks the right questions BEFORE starting work
  - Creates quality tasks with acceptance criteria
  - Breaks down tasks with dependencies
  - Knows when to ask user vs when the decision is obvious

## Acceptance Criteria (Phase 0)

1. `hive init` creates `.hive/` directory with config template
2. `hive task "description"` creates a task in SQLite
3. `hive board` shows tasks grouped by status
4. Agent can be spawned via CLI and receives task context
5. `hive answer <id> "text"` resolves a blocker
6. Basic code -> review flow works with two different agents

## Tech Stack

- **Language:** Go 1.25+
- **CLI framework:** cobra
- **Database:** SQLite (via modernc.org/sqlite — pure Go, no CGO)
- **Config:** YAML (gopkg.in/yaml.v3)
- **TUI (later):** bubbletea
