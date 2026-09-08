# Agent delivery scripts

See [.delivery/README.md](../../.delivery/README.md) for the full setup guide.

## Requirements

- `gh` CLI authenticated
- `jq`, `curl`
- `cursor-agent` logged in on the CEO machine (`cursor-agent login`)

## Examples

```bash
# Build prompt from issue #123 (stdout)
gh issue view 123 --json title,body,url,number > /tmp/issue.json
bash scripts/agent-delivery/build-prompt.sh /tmp/issue.json

# Dispatch via local cursor-agent CLI (CEO machine)
bash scripts/agent-delivery/dispatch-cursor-agent-cli.sh 123

# Merge to main and return primary checkout to main (local CLI / worktree)
bash scripts/agent-delivery/finalize-to-main.sh --issue 123
bash scripts/agent-delivery/finalize-to-main.sh --branch cursor-issue-123 --via-pr pr

# Check auto-merge eligibility for PR
bash scripts/agent-delivery/check-merge-eligible.sh 456
```

Portfolio dispatch (all repos in registry):

```bash
bash scripts/ai-company/portfolio-dispatch.sh --max-total 3
```

Make scripts executable locally:

```bash
chmod +x scripts/agent-delivery/*.sh
```
