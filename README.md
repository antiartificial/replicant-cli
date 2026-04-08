# replicant

A multi-provider AI agent harness in Go with an interactive terminal UI. Define agent personas as markdown files, point them at a codebase, and let them work.

Inspired by the agentic coding tool pattern, where an LLM loops over tool calls until an objective is complete, but built as a native CLI with full control over the provider, the prompt, the tools, and the permission model.

## What it does

```
you type a message
    -> replicant sends it to the LLM with your tools
    -> LLM responds (streamed to your terminal in real time)
    -> if the LLM calls a tool: execute it, feed the result back, loop
    -> when the LLM is done: you see the result, type the next message
```

That's the whole loop. The rest is configuration.

## Quick start

```bash
# Build
make build

# Run with default replicant (deckard)
ANTHROPIC_API_KEY=sk-... ./replicant

# Pick a different persona
./replicant -r roy        # architect/planner (Opus)
./replicant -r rachael    # autonomous coder (Sonnet, 100 turns)
./replicant -r zhora      # debugger (Sonnet)
./replicant -r pris       # fast validator (Haiku)

# Use a different provider
./replicant -m openai/gpt-4o
./replicant -m xai/grok-3

# Resume a previous session
./replicant --list-sessions
./replicant --resume 20260408-deckard
```

## Replicants

Agent personas are markdown files with YAML frontmatter. Drop them in `./replicants/` or `~/.replicant/replicants/`.

```markdown
---
name: deckard
description: General-purpose coding replicant
model: anthropic/claude-sonnet-4-20250514
tools: [read_file, edit_file, execute, glob, grep, remember, recall]
max_turns: 50
---

You are Deckard, a coding replicant. You read, search, edit,
and execute with precision.

When given a task:
1. Understand the objective
2. Plan your approach
3. Execute incrementally
4. Validate
```

The frontmatter configures the model, tools, and loop limits. The markdown body is the system prompt. No DSL, no config files, no YAML hell.

### Built-in roster

| Name | Model | Role |
|------|-------|------|
| **deckard** | Sonnet | General-purpose coding agent |
| **roy** | Opus | Architecture, planning, trade-off analysis |
| **rachael** | Sonnet | Autonomous objective completion (100 turns) |
| **zhora** | Sonnet | Debugging and root cause investigation |
| **pris** | Haiku | Fast validation, code review, yes/no checks |

## Example usage patterns

### Fix a bug

```
> there's a nil pointer panic in internal/agent/agent.go when the provider
  returns an empty content array. find it and fix it.
```

Deckard will `read_file` the source, identify the missing nil check, `edit_file` to add it, then `execute` the tests to verify.

### Plan then implement

Start with Roy (the planner), then hand off:

```bash
./replicant -r roy
```
```
> design an HTTP middleware that rate-limits by API key using a
  token bucket algorithm. consider the interface, storage backend,
  and configuration surface.
```

Roy reads the codebase, produces a structured plan. Then switch to Rachael:

```bash
./replicant -r rachael
```
```
> implement the rate limiter from this plan: [paste roy's output]
```

Rachael writes the code, runs tests, iterates until green.

Or let Roy delegate directly. Since Roy has the `delegate` tool, he can spawn Deckard as a subagent:

```
> design and implement a rate limiter middleware
```

Roy plans it, then calls `delegate(replicant: "deckard", task: "implement the rate limiter...")` and Deckard does the coding in a child session.

### Debug a test failure

```bash
./replicant -r zhora
```
```
> TestReconstructHistory is failing with "unexpected message count: got 4, want 3".
  the test is in internal/agent/session_test.go. find the root cause.
```

Zhora reads the test, reads the implementation, traces the logic, and reports the root cause with file:line references. She won't fix it unless you ask.

### Quick code review

```bash
./replicant -r pris
```
```
> review the changes in internal/tools/delegate.go for security issues.
  is there any way a malicious tool input could escape the sandbox?
```

Pris reads the file, answers yes/no first, then explains briefly. Fast and cheap on Haiku.

### Point at a GitHub issue

```bash
./replicant -r rachael -m anthropic/claude-sonnet-4-20250514
```
```
> complete this issue: https://github.com/myorg/myrepo/issues/42
  the repo is cloned at ~/projects/myrepo
```

Rachael will read the issue (via `execute` + `curl` or tools you add), explore the codebase, implement the fix, run tests, and iterate. With `/auto full` she runs unattended.

### Cross-session memory

Agents remember things across sessions using contextdb:

```
> remember that the auth module uses RS256 for JWT signing, not HS256.
  this matters for key rotation.
```

In a later session:

```
> what do we know about the auth module's JWT configuration?
```

The `recall` tool retrieves relevant memories ranked by similarity, recency, and confidence. Episodic memories (task outcomes, errors) decay faster than semantic ones (decisions, learnings).

### Dynamic autonomy

During a session, toggle how much the agent can do without asking:

```
/auto full      # let it run unattended
/auto normal    # confirm edits and shell commands (default)
/auto off       # confirm everything
/auto           # show current level
```

Useful when you're watching closely at first (`/auto off`), gain confidence, and step away (`/auto full`).

### Resume a session

```bash
# List recent sessions
./replicant --list-sessions

# Resume by ID prefix
./replicant --resume 20260408-153045-deckard

# The conversation history replays in the TUI and the agent
# picks up where it left off
```

## Providers

Replicant talks to LLMs through a `Provider` interface. Three implementations ship:

- **Anthropic** -- streaming via SSE, tool use, prompt caching aware
- **OpenAI** -- standard chat completions with function calling
- **xAI** -- OpenAI-compatible endpoint for Grok models

The model string determines the provider: `anthropic/claude-sonnet-4-20250514`, `openai/gpt-4o`, `xai/grok-3`. Bare model names (no prefix) default to Anthropic.

### Context windows

Each model has a known context window. Auto-compaction triggers at 75% capacity:

| Model | Context | Compact at |
|-------|---------|-----------|
| Claude Sonnet/Opus | 200K | 150K |
| Claude Haiku | 200K | 150K |
| GPT-4o | 128K | 96K |
| Grok-3 | 131K | 98K |

When the conversation exceeds the threshold, older messages are summarized by the LLM and replaced with a compact context block. Recent messages are preserved verbatim.

## Tools

| Tool | Risk | What it does |
|------|------|-------------|
| `read_file` | none | Read file contents with line numbers |
| `write_file` | low | Create or overwrite a file (creates parent dirs) |
| `edit_file` | low | Search-and-replace in existing files |
| `list_dir` | none | List directory contents with sizes |
| `execute` | high | Run shell commands with timeout |
| `glob` | none | Find files by pattern |
| `grep` | none | Search file contents (uses ripgrep) |
| `remember` | none | Store a memory in contextdb |
| `recall` | none | Retrieve relevant memories from contextdb |
| `delegate` | low | Spawn a child replicant to complete a subtask |

Tools declare their own risk level. The permission system gates execution based on your autonomy setting:

```bash
REPLICANT_AUTONOMY=normal  # auto-approve reads, confirm edits + shell (default)
REPLICANT_AUTONOMY=high    # auto-approve most, confirm destructive ops
REPLICANT_AUTONOMY=full    # never ask
REPLICANT_AUTONOMY=off     # confirm everything
```

When a tool needs confirmation, the TUI prompts inline. Press `y` to allow, `n` to deny.

## Slash commands

Type these during a session:

| Command | What it does |
|---------|-------------|
| `/auto` | Show current autonomy level |
| `/auto <level>` | Set autonomy: off, normal, high, full |
| `/model` | Show current model |
| `/session` | Show current session ID |
| `/help` | List available commands |
| `/quit` | Exit |

## Memory

Replicant uses [contextdb](https://github.com/antiartificial/contextdb) in embedded mode for cross-session agent memory. Data lives at `~/.replicant/memory/`.

Memories are stored with different decay rates:

| Category | Decay | Examples |
|----------|-------|---------|
| observation, task | episodic (fast) | "tests pass", "deployed to staging" |
| decision, learning | semantic (slow) | "chose RS256 over HS256", "auth uses middleware pattern" |
| skill, procedure | procedural (very slow) | "deploy with `make deploy-prod`" |

The `remember` tool stores explicitly. Session summaries are stored automatically on completion. The `recall` tool retrieves by semantic similarity, weighted by recency and source confidence.

## Sessions

Every conversation is logged as append-only JSONL in `~/.replicant/sessions/`. Each line is a typed entry: `session_start`, `message`, `tool_call`, or `tool_result`. Sessions are never modified after writing, useful for debugging, replay, and auditing what the agent did.

```bash
# List recent sessions
./replicant --list-sessions

# Resume a session (full history replays in the TUI)
./replicant --resume <session-id>
```

## Project layout

```
cmd/replicant/main.go           Entry point, wires config -> provider -> agent -> TUI
internal/
  agent/
    provider.go                 Provider interface, message types, factory
    anthropic.go                Anthropic SDK with SSE streaming
    openai.go                   OpenAI/xAI SDK with function calling
    agent.go                    ReAct loop (tool_use -> execute -> tool_result -> repeat)
    compact.go                  Context compaction via LLM summarization
    models.go                   Model registry (context windows, output limits)
    session.go                  JSONL session persistence + resume
  config/config.go              Environment-based configuration
  memory/memory.go              ContextDB-backed cross-session agent memory
  permission/permission.go      Risk levels and autonomy gating
  replicant/
    definition.go               Replicant definition struct
    loader.go                   Markdown + YAML frontmatter parser
    registry.go                 Discovers .md files from standard dirs
  tools/
    tool.go                     Tool interface
    registry.go                 Tool name -> implementation lookup
    readfile.go                 read_file
    editfile.go                 edit_file (search-and-replace)
    execute.go                  Shell execution with timeout
    glob.go                     Recursive file pattern matching
    grep.go                     Content search (ripgrep or grep fallback)
    remember.go                 Store memory in contextdb
    recall.go                   Retrieve memories from contextdb
    delegate.go                 Spawn child replicant as subagent
  tui/
    app.go                      Root bubbletea model, agent bridge, slash commands
    conversation.go             Scrollable message viewport with history replay
    input.go                    Multi-line text input
    statusbar.go                Model, tokens, autonomy, session timer
    spinner.go                  Thinking indicator
    theme.go                    Blade Runner color palette
    messages.go                 Bubbletea message types
replicants/                     Agent persona definitions (.md)
Makefile                        Build, run, test, docker
Dockerfile                      Multi-stage build, ships with ripgrep
```

## What this brings to the table

**Native binary, no runtime.** Single `go build` produces a 17MB binary. No Node, no Python, no Docker required to run.

**Markdown-defined agents.** The system prompt, model selection, tool access, and loop limits are all in one readable file. Fork a replicant by copying a markdown file and editing it.

**Provider-agnostic loop.** The ReAct loop doesn't know or care which LLM is behind the `Provider` interface. Swap Anthropic for OpenAI by changing a prefix. Add a new provider by implementing one method.

**Streaming-first.** Anthropic responses stream token-by-token to the TUI via SSE. You see the agent think in real time, not after a long wait.

**Permission model with teeth.** Tools declare risk. Autonomy levels gate execution. The TUI blocks on confirmation for dangerous operations. No silent `rm -rf`. Toggle autonomy mid-session with `/auto`.

**Cross-session memory.** Agents remember decisions, learnings, and outcomes across sessions via contextdb. Episodic memories fade, semantic knowledge persists. No manual context management.

**Subagent delegation.** A planner can spawn an implementer. An implementer can spawn a debugger. Each child gets its own model, tools, and system prompt. Results flow back to the parent.

**Auditable sessions.** Every tool call, every result, every message is logged to JSONL. Resume any session. Trace exactly what the agent did and why.

**Composable by design.** The agent loop, tools, providers, and TUI are separate packages with clean interfaces. Use the agent loop without the TUI. Add tools without touching the loop. Swap the TUI for a web interface.

## Testing

```bash
make test
```

Test coverage includes the tool implementations, replicant definition loader, agent ReAct loop (with mock provider), context compaction, session persistence and resume, and the permission model.

## Environment variables

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | | Anthropic API key |
| `OPENAI_API_KEY` | | OpenAI / xAI API key |
| `REPLICANT_MODEL` | `claude-sonnet-4-20250514` | Default model |
| `REPLICANT_AUTONOMY` | `normal` | Permission level |

## Docker

```bash
make docker
docker run --rm -it -e ANTHROPIC_API_KEY -v $(pwd):/work -w /work replicant:latest
```

## License

MIT
