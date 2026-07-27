import re

def resolve_file(filepath):
    with open(filepath, 'r') as f:
        content = f.read()
    
    lines = content.split('\n')
    result = []
    skip_until = -1
    in_conflict = False
    head_lines = []
    ours_lines = []
    in_head = False
    in_ours = False
    
    for i, line in enumerate(lines):
        if i < skip_until:
            continue
        
        if line.strip() == '<<<<<<< HEAD':
            in_conflict = True
            in_head = True
            in_ours = False
            head_lines = []
            ours_lines = []
            continue
        elif line.strip() == '=======':
            in_head = False
            in_ours = True
            continue
        elif line.strip().startswith('>>>>>>>'):
            in_conflict = False
            in_head = False
            in_ours = False
            # Resolve: keep HEAD version but add qwenpaw if not present
            for hline in head_lines:
                result.append(hline)
                if 'qoderclicn' in hline and 'qwenpaw' not in hline:
                    # Add qwenpaw to the line
                    pass  # already handled by adding
            continue
        
        if in_head:
            head_lines.append(line)
            continue
        elif in_ours:
            ours_lines.append(line)
            continue
        
        result.append(line)
    
    return '\n'.join(result)

# File 1: config.go
filepath = 'server/internal/daemon/config.go'
with open(filepath, 'r') as f:
    content = f.read()

# Resolve conflict 1 (Agents comment) - keep both qoderclicn and qwenpaw
content = content.replace(
    '''<<<<<<< HEAD
	Agents                         map[string]AgentEntry // keyed by provider: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, qoderclicn, traecli, grok, qwen
=======
	Agents                         map[string]AgentEntry // keyed by provider: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, traecli, grok, qwen, qwenpaw
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '\tAgents                         map[string]AgentEntry // keyed by provider: claude, codebuddy, codex, copilot, opencode, openclaw, hermes, pi, cursor, kimi, kiro, antigravity, qoder, qoderclicn, traecli, grok, qwen, qwenpaw'
)

# Resolve conflict 2 (error message) - keep both qoderclicn and qwenpaw
content = content.replace(
    '''<<<<<<< HEAD
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor-agent, kimi, kiro-cli, agy, qodercli, qoderclicn, traecli, grok, or qwen and ensure it is on PATH")
=======
		return Config{}, fmt.Errorf("no agent CLI found: install claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor-agent, kimi, kiro-cli, agy, qodercli, traecli, grok, qwen, or qwenpaw and ensure it is on PATH")
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '\t\treturn Config{}, fmt.Errorf("no agent CLI found: install claude, codebuddy, codex, copilot, opencode, deveco, openclaw, hermes, pi, cursor-agent, kimi, kiro-cli, agy, qodercli, qoderclicn, traecli, grok, qwen, or qwenpaw and ensure it is on PATH")'
)

# Resolve conflict 3 (defaultAgentCommandNames) - keep both qoderclicn and qwenpaw
content = content.replace(
    '''<<<<<<< HEAD
	"pi", "cursor-agent", "copilot", "kimi", "kiro-cli", "codebuddy", "agy", "qodercli", "qoderclicn", "traecli", "grok", "qwen",
=======
	"pi", "cursor-agent", "copilot", "kimi", "kiro-cli", "codebuddy", "agy", "qodercli", "traecli", "grok", "qwen", "qwenpaw",
>>>>>>> 974558978 (feat: add QwenPaw ACP backend support)''',
    '\t"pi", "cursor-agent", "copilot", "kimi", "kiro-cli", "codebuddy", "agy", "qodercli", "qoderclicn", "traecli", "grok", "qwen", "qwenpaw",'
)

with open(filepath, 'w') as f:
    f.write(content)

print("config.go resolved")

# Verify no remaining conflicts
with open(filepath, 'r') as f:
    c = f.read()
if '<<<<<<<' in c or '=======' in c or '>>>>>>>' in c:
    print("WARNING: remaining conflict markers in config.go")
else:
    print("OK: no conflict markers")
