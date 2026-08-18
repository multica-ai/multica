# Provider capability matrix

This file is generated from `server/pkg/agent/provider_capabilities.go`. Do not edit it by hand.

| Provider | MCP support/delivery | Sandbox/approval | Runtime config | Workspace skills | User skills | Instruction delivery | Compatibility |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Claude Code | yes: --mcp-config + --strict-mcp-config | bypassPermissions; daemon blocks approval prompts | `CLAUDE.md` | `.claude/skills` | `~/.claude/skills` | file | native CLAUDE.md and .claude/skills discovery |
| CodeBuddy | yes: --mcp-config; native user/project/local scopes remain enabled | bypassPermissions; disallowed approval tools | `CODEBUDDY.md` | `.codebuddy/skills` | `~/.codebuddy/skills` | file | native CODEBUDDY.md and .codebuddy/skills discovery |
| Codex | yes: task CODEX_HOME/config.toml | workspace-write; sandbox policy in config/args | `AGENTS.md` | `CODEX_HOME/skills` | `$CODEX_HOME/skills` | file | AGENTS.md is read from the task cwd; skills use task CODEX_HOME |
| GitHub Copilot CLI | no: not supported by agent.mcp_config | yolo/interactive policy owned by Copilot CLI | `AGENTS.md` | `.github/skills` | `~/.copilot/skills` | file | AGENTS.md and .github/skills project discovery |
| OpenCode | yes: OPENCODE_CONFIG_CONTENT | provider-native permission policy | `AGENTS.md` | `.opencode/skills` | `~/.config/opencode/skills` | file | --dir/PWD anchors AGENTS.md and .opencode/skills at the task workdir |
| DevEco Code | no: not supported by agent.mcp_config | provider-native permission policy | `AGENTS.md` | `.deveco/skills` | `~/.config/deveco/skills` | file | OpenCode-compatible AGENTS.md and .deveco/skills discovery |
| OpenClaw | yes: per-task openclaw-config.json wrapper | --local plus daemon-owned agent config | `AGENTS.md` | `skills` | `~/.openclaw/skills` | file+legacy-inline before 2026.5.5 | supported >= 2026.5.5: workspace-pinned AGENTS.md + skills/ only; older releases retain inline fallback |
| Hermes Agent | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `HERMES_HOME/skills` | `~/.hermes/skills` | file | per-task HERMES_HOME is seeded and AGENTS.md is read from the ACP cwd |
| Pi coding agent | no: not supported by agent.mcp_config | provider-native tool policy | `AGENTS.md` | `.pi/skills` | `~/.pi/agent/skills` | file | AGENTS.md and .pi/skills are loaded from the task cwd |
| Cursor Agent | yes: task Cursor MCP config/auth sidecars | --yolo | `AGENTS.md` | `.cursor/skills` | `~/.cursor/skills` | file | AGENTS.md and .cursor/skills are anchored with --workspace |
| Kimi CLI | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.kimi/skills` | `~/.kimi/skills` | inline | inline fallback remains required while Kimi cwd discovery is opaque |
| Reasonix | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.reasonix/skills` | `$REASONIX_HOME/skills` | file | AGENTS.md and .reasonix/skills follow the effective REASONIX_HOME |
| Kiro CLI | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.kiro/skills` | `~/.kiro/skills` | file | Kiro ACP smoke uses root AGENTS.md and .kiro/skills |
| Antigravity | no: not supported by agent.mcp_config | provider-native permission policy | `AGENTS.md` | `.agents/skills` | `~/.gemini/antigravity-cli/skills` | file | AGENTS.md and .agents/skills use Antigravity workspace discovery |
| Qoder CLI | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.qoder/skills` | `~/.qoder/skills` | file | Qoder project skills use .qoder/skills |
| Qoder CLI CN | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.qoder/skills` | `~/.qoder-cn/skills` | file | Qoder CN project skills use .qoder/skills and a separate user root |
| Trae CLI | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `.traecli/skills` | `~/.traecli/skills` | inline | Trae reads .trae/rules rather than AGENTS.md; inline delivery remains required |
| Grok Build | yes: ACP session/new MCP parameters | --always-approve | `AGENTS.md` | `.grok/skills` | `$GROK_HOME/skills` | file | Grok Build reads AGENTS.md and .grok/skills from the task cwd |
| Qwen Code | yes: Qwen native config/session parameters | provider-native permission policy | `QWEN.md` | `.qwen/skills` | `$QWEN_HOME/skills` | file | QWEN.md is the runtime context file; .qwen/skills is project-local |
| QwenPaw | yes: ACP session/new MCP parameters | ACP permission policy | `AGENTS.md` | `skill_pool` | `$QWENPAW_WORKING_DIR/skill_pool` | inline | QwenPaw workspace skill_pool is isolated; inline delivery remains required |

MCP config source details:

- `claude`: `~/.claude.json:mcpServers`
- `codebuddy`: `CodeBuddy native user/project/local scopes`
- `codex`: `CODEX_HOME/config.toml:mcp_servers`
- `copilot`: `native Copilot configuration`
- `opencode`: `XDG_CONFIG_HOME/opencode/opencode.json:mcp`
- `deveco`: `native DevEco configuration`
- `openclaw`: `CLAWDBOT_CONFIG_PATH or OPENCLAW_STATE_DIR/openclaw.json:mcp.servers`
- `hermes`: `HERMES_HOME native profile`
- `pi`: `Pi native extensions/config`
- `cursor`: `~/.cursor/mcp.json:mcpServers`
- `kimi`: `Kimi native config`
- `reasonix`: `REASONIX_HOME native config`
- `kiro`: `Kiro native config`
- `antigravity`: `Gemini/Antigravity native config`
- `qoder`: `Qoder native config`
- `qoderclicn`: `Qoder CN native config`
- `traecli`: `Trae CLI native config`
- `grok`: `GROK_HOME native config`
- `qwen`: `QWEN_HOME native config`
- `qwenpaw`: `QWENPAW_WORKING_DIR native skill/config store`
