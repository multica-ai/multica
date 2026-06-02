# Nanjing VPS Artifact Relay

Use this workflow when a mainland China VPS should keep domestic-agent
workloads but must update Multica without direct GitHub release access.

This is not a general proxy. The relay caches verified Multica release
artifacts on a local Mac, US server, or other host that can reach GitHub, then
serves only cached release metadata and cached asset files from `127.0.0.1`.

## Relay Host

Run these commands on the local Mac, US server, or other relay host:

```bash
python3 scripts/artifact-relay.py --sync
python3 scripts/artifact-relay.py --serve --port 9876
```

For a one-shot start:

```bash
python3 scripts/artifact-relay.py --all --port 9876
```

The script downloads release metadata, `checksums.txt`, and release assets,
then verifies asset SHA-256 values before serving the cache.

## Nanjing VPS

Create a tunnel from the Nanjing VPS to the relay host:

```bash
ssh -N -L 9876:127.0.0.1:9876 youruser@relay-host
```

Configure the Multica updater to use the relay:

```bash
export MULTICA_UPDATE_GH_API_BASE=http://localhost:9876
export MULTICA_UPDATE_GH_DOWNLOAD_BASE=http://localhost:9876/assets
multica daemon restart
```

Test relay connectivity before updating:

```bash
curl -s http://localhost:9876/repos/multica-ai/multica/releases/latest | python3 -m json.tool | head
```

## Safe Codex Removal

Do discovery first. Do not delete paths until the real installation and config
locations are listed and reviewed.

```bash
command -v codex
codex --version 2>/dev/null
npm list -g @openai/codex 2>/dev/null
npm list -g @anthropic/codex 2>/dev/null
brew list --formula 2>/dev/null | grep -i codex
echo "MULTICA_CODEX_PATH=${MULTICA_CODEX_PATH:-<unset>}"
find ~/.npm -maxdepth 8 -path '*/@openai/codex' -type d 2>/dev/null
find ~/.npm -maxdepth 8 -path '*/@anthropic/codex' -type d 2>/dev/null
ls -la ~/.codex/ 2>/dev/null
ls -la ~/.config/codex/ 2>/dev/null
ls -la ~/.cache/codex/ 2>/dev/null
```

If config or session data should be preserved, back it up before uninstalling:

```bash
tar czf ~/codex-backup-$(date +%Y%m%d).tar.gz \
  ~/.codex/ ~/.config/codex/ ~/.cache/codex/ 2>/dev/null
```

After reviewing the discovery output and backup, uninstall Codex:

```bash
npm uninstall -g @openai/codex 2>/dev/null
npm uninstall -g @anthropic/codex 2>/dev/null
brew uninstall codex 2>/dev/null
```

Only after confirming the backup is no longer needed, optionally clear Codex
state:

```bash
rm -rf ~/.codex/
rm -rf ~/.config/codex/
rm -rf ~/.cache/codex/
```

Restart and verify:

```bash
multica daemon restart
command -v codex
multica daemon show 2>&1 | grep -i codex
```
