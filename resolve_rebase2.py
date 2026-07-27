# Resolve daemon.go
with open('server/internal/daemon/daemon.go', 'r') as f:
    content = f.read()

content = content.replace(
    '''<<<<<<< HEAD
	"traecli":    "Trae",
	"grok":       "Grok",
	"qoderclicn": "Qoder CN",
	"qwen":       "Qwen Code",
=======
	"traecli": "Trae",
	"grok":    "Grok",
	"qwen":    "Qwen Code",
	"qwenpaw": "QwenPaw",
>>>>>>> 08dcd704a (fix: address Bohan-J review comments on qwenpaw backend (PR #5986))''',
    '\t"traecli":    "Trae",\n\t"grok":       "Grok",\n\t"qoderclicn": "Qoder CN",\n\t"qwen":       "Qwen Code",\n\t"qwenpaw":    "QwenPaw",'
)

with open('server/internal/daemon/daemon.go', 'w') as f:
    f.write(content)
print("daemon.go resolved")

# Resolve agent.go
with open('server/pkg/agent/agent.go', 'r') as f:
    content = f.read()

content = content.replace(
    '''<<<<<<< HEAD
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, qoderclicn, traecli, grok, qwen)", agentType)
=======
		return nil, fmt.Errorf("unknown agent type: %q (supported: claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, traecli, grok, qwen, qwenpaw)", agentType)
>>>>>>> 08dcd704a (fix: address Bohan-J review comments on qwenpaw backend (PR #5986))''',
    '\t\treturn nil, fmt.Errorf("unknown agent type: %q (supported: claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, qoderclicn, traecli, grok, qwen, qwenpaw)", agentType)'
)

with open('server/pkg/agent/agent.go', 'w') as f:
    f.write(content)
print("agent.go resolved")

# Verify
for f in ['server/internal/daemon/daemon.go', 'server/pkg/agent/agent.go']:
    with open(f) as fh:
        c = fh.read()
    if '<<<<<<<' in c or '=======' in c or '>>>>>>>' in c:
        print(f"WARNING: remaining conflicts in {f}")
    else:
        print(f"OK: {f}")
