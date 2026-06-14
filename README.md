# EVVAgent (evva)

A ReAct coding agent in your terminal. Multi-provider LLM, parallel tool dispatch, async sub-agents, swappable UI.

---

## What is EVVAgent?

![evva_logo.png](docs/assets/logo-3.jpg)

`evva` runs a tool-using LLM agent in your terminal. It speaks Anthropic Claude, DeepSeek, OpenAI, and Ollama through one `llm.Client` interface; dispatches multiple tool calls per turn in parallel; tracks tasks and sub-agents through an observable store; and renders into a bubbletea TUI or a plain-text CLI sink.

The architecture is small on purpose — adding a new LLM provider, panel, or UI implementation is roughly one package each.


---

## Install

macOS, Linux, and Windows (10 1903+) are supported. `go install` and source builds require Go 1.25+; prebuilt binaries for every platform ship with each [GitHub Release](https://github.com/johnny1110/evva/releases).

### Quick install (recommended)

```bash
go install github.com/johnny1110/evva/cmd/evva@latest
```

The binary lands in `$GOBIN` (or `$GOPATH/bin`). Make sure it's on your `PATH`.

### Build from source

```bash
git clone https://github.com/johnny1110/evva
cd evva
make install
```

Override the location if you want it elsewhere:

```bash
sudo make install PREFIX=/usr/local/bin     # system-wide
make install PREFIX=$HOME/.local/bin        # user-local
```

### Windows

Download `evva-windows-amd64.zip` (or `-arm64`) from the [latest release](https://github.com/johnny1110/evva/releases/latest), unzip `evva.exe` somewhere on your `PATH`, and run it from Windows Terminal — or use `go install` as above (the binary lands in `%USERPROFILE%\go\bin`).

Prerequisites:

- **[Git for Windows](https://gitforwindows.org)** — the agent's `bash` tool runs through its bundled Git Bash. Without it evva still starts, but shell-backed tools (bash, monitor, hooks) are unavailable and a startup warning says so. Set `EVVA_SHELL` to any other `bash.exe` to override autodetection.
- **Windows Terminal** recommended for the TUI (legacy conhost works on Windows 10 1903+, but rendering is only validated on Windows Terminal).

If SmartScreen flags the downloaded exe, unblock it via file Properties → Unblock — the binaries are unsigned for now. Full details, including what `evva service` does and doesn't do on Windows: [docs/user-guide/en/windows.md](docs/user-guide/en/windows.md).

### Verify

```bash
evva -version
```

Uninstall removes only the binary; your `~/.evva/` config is preserved:

```bash
make uninstall
```

---

## Update

To update evva to the latest version without Go:

```bash
evva update                  # newest stable release (GitHub "Latest")
evva update latest           # same as above
evva update v1.4.3           # pin to an exact stable version
evva update v1.4.3-beta.1    # opt into a specific beta (pre-release)
```

With no argument (or `latest`) this resolves GitHub's Latest release — the newest **stable** build on `main`. Passing a tag pins to that exact build, including a `-beta.N` pre-release or an older version to downgrade. In every case it downloads the pre-built binary for your OS/arch and replaces the current one atomically. No Go toolchain required.

You can also check for updates from inside the TUI with `/update`.

To see your current version:

```bash
evva -version
```

---

## First run

Just type `evva` from any directory. On the first launch evva auto-creates:

```
~/.evva/
├── config/
│   └── evva-config.yml      # user-tunable settings (auto-created with defaults)
└── skills/                  # optional skill scripts (your own)
```

A one-line stderr notice fires the first time only:

```
evva: wrote new config to ~/.evva/config/evva-config.yml — fill in your API keys to use cloud providers.
```

`~/.evva/.env` is **optional**. If you want to override deployment knobs (`LOG_LEVEL`, `LOG_DIR`, `APP_ENV`, `LOG_FORMAT`, `SKILLS_DIR`, `USER_PROFILE`), create it; otherwise the built-in defaults apply.

### Adding an API key

Two ways:

1. **From inside the TUI:** type `/config`, navigate to `<provider>.api_key`, press Enter, paste your key, press Enter again. Saved immediately.
2. **By hand:** open `~/.evva/config/evva-config.yml` and fill in `providers.<provider>.api_key`.

Cloud providers (Anthropic, DeepSeek, OpenAI) need a key; Ollama is local and key-less.

---

## TUI reference

![tui.png](docs/assets/tui.png)

---

## User Guide

Full usage documentation covering the TUI interface, slash commands, keybindings, yank mode, the permission system, sub-agents, and all configuration options:

- [English](docs/user-guide/en/user-guide.md)
- [正體中文](docs/user-guide/zh-tw/user-guide.md)

### Swarm & service (multi-agent workstation)

Run a team of collaborating agents with `evva service` + `evva swarm`. A 0→hero
walkthrough — concepts, building a swarm from scratch, the web workstation,
day-2 ops, restart-resume:

- [English](docs/user-guide/swarm/en.md)
- [简体中文](docs/user-guide/swarm/zh.md)

For a field reference — every manifest field, tool, and coordination pattern —
see the [agent guide](docs/user-guide/agent-guide/).

Or just try a ready-to-run example: the minimal 3-agent
[starter swarm](examples/evva-swarm/starter/) (`evva swarm .`, watch a small team
build a site) or the 7-member [tech-team swarm](examples/evva-swarm/tech-team/).
More live under [`examples/evva-swarm/`](examples/evva-swarm/).

Running it 24/7? `evva service install-unit` wires the host into launchd /
systemd so it survives crashes and reboots —
[setup runbook (EN)](docs/user-guide/en/service-autostart.md) ·
[正體中文](docs/user-guide/zh-tw/service-autostart.md).

**CLI quick reference** (`evva swarm help` for the full list). Spaces are
Docker-style: a stable id plus a unique **name**, and every `<ref>` below accepts
either:

| Command | What it does |
| --- | --- |
| `evva swarm . [--name <n>]` | register `./evva-swarm.yml` as a new space (name: `--name` → manifest `name:` → generated) |
| `evva swarm ls` | list spaces — running and stopped (like `docker ps -a`) |
| `evva swarm run <ref>` | (re)start a stopped space, under its same id / URL |
| `evva swarm stop <ref>` | stop a space but keep it (restart with `run`) |
| `evva swarm rm <ref>` | forget a space entirely (its workdir data is left intact) |
| `evva swarm reset <ref>` | wipe a space — fresh ledger + cleared agent context, same id |
| `evva swarm add <ref> <m>` | hot-load member `<m>` into a space |

**External-event webhook.** An outside app can drive a swarm's leader by POSTing
an event — a webhook is just a message dropped on the leader's mailbox, so the
leader wakes (or folds it into its current run) and decides what to do. The
endpoint is **unauthenticated** (the service binds loopback only; this is for
local integrations in the current phase):

```
POST http://127.0.0.1:8888/api/swarm/<ref>/event
Body: { "title"?, "body" (required), "source"?, "data"?, "to"? (default leader), "idempotency_key"? }
→ 202 { "messageId": "<id>" }   (200 if the idempotency_key was already seen)
```

```python
# in your engine — one function is all you need
import requests
EVVA = "http://127.0.0.1:8888/api/swarm/trader/event"
def notify(title, body, data=None):
    requests.post(EVVA, json={"title": title, "body": body, "data": data}, timeout=2)

notify("BTC volatility spike", "BTC 1m vol > 3σ; 64210→66800 in 4m", {"symbol": "BTC", "z": 3.4})
```

```bash
curl -XPOST http://127.0.0.1:8888/api/swarm/trader/event \
  -d '{"title":"BTC volatility spike","body":"vol>3σ","data":{"symbol":"BTC","z":3.4}}'
```

---

## How to integrate EVVA agent in your Go project?

- [English](docs/user-guide/en/integration.md)

---

## LSP integration

- [English](docs/user-guide/en/lsp.md)

---


## Configuration

### `~/.evva/config/evva-config.yml`

User-tunable settings. Created automatically on first launch. Edit live via `/config` in the TUI, or by hand:

```yaml
# Agent loop
max_iterations: 30
max_tokens: 4096
auto_compact_threshold: 0.8
display_thinking: true

# Default model used at startup (overwritten by /model swap)
default_provider: deepseek
default_model: deepseek-v4-pro

# Permission stance at startup. Cycle at runtime with Shift+Tab; -permission-mode CLI flag overrides.
permission_mode: default     # default | accept_edits | plan | bypass

# Web tooling
fetch_max_bytes: 100000
tavily_api_key: ""

# Per-provider credentials. Empty api_url falls back to the constant's default.
providers:
  anthropic: { api_key: "", api_url: "" }
  deepseek:  { api_key: "", api_url: "" }
  openai:    { api_key: "", api_url: "" }
  ollama:    { api_url: "" }
```

### `.env` (optional)

Place in your working directory or at `~/.evva/.env`. Only used for deployment / logging knobs — never user preferences:

```bash
APP_ENV=dev            # dev | prod
LOG_LEVEL=info         # debug | info | warn | error
LOG_FORMAT=text        # text | json
LOG_DIR=               # unset → $EVVA_HOME/logs (default); path → custom dir; explicit empty → stdout-only
SKILLS_DIR=skills      # subpath under ~/.evva/
USER_PROFILE=user_profile.md
```

### CLI flags

```bash
evva                                # interactive TUI (when stdout is a TTY)
evva -version                       # print version, commit, and build date
evva update                         # self-update to newest stable (no Go required)
evva update v1.4.3-beta.1           # self-update to a specific tag (stable or beta)
evva -temp 0.7                      # sampling temperature (default unset)
evva -max-tokens 2048               # per-completion output cap (overrides YAML)
evva -max-iters 40                  # loop iteration cap (overrides YAML)
evva -permission-mode=plan          # boot in plan mode (read-only; see "Permission modes")
evva -permission-mode=bypass        # boot with the gate disabled
evva -no-tui "explain loop.go"      # one-shot plain-text mode
echo "list files in /tmp" | evva -no-tui   # piped prompt
```

---

## Features

**Agent loop**
- ReAct-style: LLM call → parallel tool dispatch → tool results → repeat.
- Multiple `tool_use` blocks per turn, executed concurrently.
- Iteration cap surfaces as a pausable state.
- Cancellable via `ctx`; Esc / Ctrl+C honored end-to-end.

**LLM providers**
- Anthropic Claude (extended thinking + cryptographic signature round-trip).
- DeepSeek (OpenAI-compatible chat, reasoning_content echoed back).
- OpenAI.
- Ollama (local).
- Per-provider option pattern (`WithTemperature`, `WithEffort`, ...).

**Tools**
- File system: `read`, `write`, `edit` — strict-absolute paths, structured `*FileDiff` metadata for diff rendering; `read` also handles PDFs (with `pages`), notebooks, and images.
- Shell / search: `bash` (sync + background), `grep`, `tree`, `glob`.
- Meta (always active): `agent` (sub-agents), `tool_search` (lazy schema loading), `skill`, `schedule_wakeup`, `todo_write`.
- Deferred (loaded on demand via `tool_search`): `web_fetch` / `web_search` / `http_request`, `monitor`, `daemon_list` / `daemon_output` / `daemon_stop`, `lsp_request`, `ask_user_question` / `push_notification`, `alarm_*`, `json_query`, `calc`, `excel`, `repl`, plus per-server `mcp__<server>__<tool>` tools.
- Still stubbed (planned): `cron_*`, `remote_trigger`, `notebook_edit`.

**Sub-agents**
- `explore` (read-only) and `general-purpose` presets.
- Sync mode (parent blocks) and async mode (parent continues, result lands on next iteration).
- Two-layer hierarchy: sub-agents can't spawn sub-agents.

**Observable store framework** (`pkg/observable`)
- One pub/sub primitive any store can embed. Adding a new panel costs zero edits to the agent or event packages.

**Swappable UI** (`pkg/ui`)
- Narrow `UI` and `Controller` interfaces. Reference Bubble Tea TUI under `internal/ui/` (built on the `pkg/ui/bubbletea` contract); `pkg/ui/lp` is a compact line-based UI; `-no-tui` falls back to a plain CLI sink.

**Streaming completions** (chunked text + thinking).

**2-level compaction**
- micro: compress tool-result blocks when context budget approaches threshold.
- full: summarize the whole session into a single assistant brief.

---

## Project structure

```
evva/
├── cmd/evva/        # CLI entry point — TUI + `service` / `swarm` / `update` subcommands
├── pkg/             # stable public SDK (embed evva as a library)
│   ├── agent/       #   agent constructor + controller interface
│   ├── llm/         #   provider abstraction + claude/ deepseek/ openai/ ollama/ builtins/
│   ├── tools/       #   Tool interface + fs/ shell/ web/ lsp/ repl/ cron/ … families
│   ├── toolset/     #   tool registry + catalog
│   ├── ui/          #   UI plugin contract + bubbletea/ + lp/ reference impls
│   ├── observable/  #   pub/sub mixin for backing stores
│   ├── event/ config/ constant/ permission/ skill/ hooks/ mcp/ update/ version/ …
├── internal/        # runtime-specific impls (not part of the public API)
│   ├── agent/       #   agent struct, main loop, spawn, profiles, sysprompt/, loader/
│   ├── swarm/       #   Veronica multi-agent swarm subsystem (:8888 host)
│   ├── tools/       #   evva-specific tool families (meta/ mode/ ux/ config/ dev/)
│   ├── ui/          #   Bubble Tea v2 TUI implementation
│   ├── session/ toolset/ permission/ question/ memdir/ hooks/ skills/ logger/
├── web2/            # Vue 3 + TypeScript swarm web workstation (FE v2)
├── examples/        # embedding hosts + ready-to-run example swarms
├── docs/            # documentation (see docs/README.md for the map)
├── scripts/         # dev / release scripts
└── ref/             # reference TypeScript source ported from
```

The full package-by-package breakdown lives in **[EVVA.md](EVVA.md)** (vision +
architecture). Key boundaries:
- `agent` knows about `event.Sink`, never about a concrete UI.
- `pkg/tools/*` and `internal/tools/*` produce `tools.Result` (text + opaque `Metadata`); the UI type-asserts on `Metadata` to render structured payloads.
- `pkg/observable` has no dependencies on agent or UI.
- `pkg/ui` defines two narrow interfaces; implementations live under `internal/ui/` and `pkg/ui/`.

---

## Roadmap

Much of the original roadmap has shipped: multimodal `read` (images, PDFs,
notebooks), the Veronica multi-agent swarm with a Vue web workstation (`:8888`),
Windows support, MCP client, typed memory, LSP intelligence, and session
persistence. The living roadmap — waves, PRDs, and design docs — lives under
**[docs/roadmap/](docs/roadmap/)**.

### Known limitations
- Sub-agent hierarchy is exactly two layers (no nested spawning).
- Token counts depend on provider reporting — Ollama only reports prompt / eval, not cache or reasoning splits.
- The swarm web workstation binds loopback only and its event webhook is unauthenticated (local integrations only, current phase).

---

## Contributing

Bug reports, features, and PRs are welcome — start with
**[CONTRIBUTING.md](CONTRIBUTING.md)** for dev setup, the branch/PR flow, and
where things live. Architecture and vision: **[EVVA.md](EVVA.md)**.

## License

See [LICENSE](LICENSE).
