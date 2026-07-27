# Resolve runtime_config.go
with open('server/internal/daemon/execenv/runtime_config.go', 'r') as f:
    content = f.read()

content = content.replace(
    '''<<<<<<< HEAD
	case "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "qoderclicn", "traecli", "grok":
=======
	case "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "traecli", "grok", "qwenpaw":
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '\tcase "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "qoderclicn", "traecli", "grok", "qwenpaw":'
)

with open('server/internal/daemon/execenv/runtime_config.go', 'w') as f:
    f.write(content)
print("runtime_config.go resolved")

# Resolve agent.go
with open('server/pkg/agent/agent.go', 'r') as f:
    content = f.read()

content = content.replace(
    '''<<<<<<< HEAD
// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "qoderclicn", "traecli", "grok", "qwen".
=======
// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "traecli", "grok", "qwen", "qwenpaw".
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '// Supported types: "claude", "codebuddy", "codex", "copilot", "opencode", "deveco", "openclaw", "hermes", "pi", "cursor", "kimi", "kiro", "antigravity", "qoder", "qoderclicn", "traecli", "grok", "qwen", "qwenpaw".'
)

with open('server/pkg/agent/agent.go', 'w') as f:
    f.write(content)
print("agent.go resolved")

# Resolve agent_supported_types_test.go
with open('server/pkg/agent/agent_supported_types_test.go', 'r') as f:
    content = f.read()

content = content.replace(
    '''<<<<<<< HEAD
		"qoder": true, "qoderclicn": true, "traecli": true, "grok": true, "qwen": true,
=======
		"qoder": true, "traecli": true, "grok": true, "qwen": true, "qwenpaw": true,
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '\t\t"qoder": true, "qoderclicn": true, "traecli": true, "grok": true, "qwen": true, "qwenpaw": true,'
)

with open('server/pkg/agent/agent_supported_types_test.go', 'w') as f:
    f.write(content)
print("agent_supported_types_test.go resolved")

# Verify
for f in ['server/internal/daemon/execenv/runtime_config.go', 'server/pkg/agent/agent.go', 'server/pkg/agent/agent_supported_types_test.go']:
    with open(f) as fh:
        c = fh.read()
    if '<<<<<<<' in c or '=======' in c or '>>>>>>>' in c:
        print(f"WARNING: remaining conflicts in {f}")
    else:
        print(f"OK: {f}")
