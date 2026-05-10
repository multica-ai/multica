# Multica — `Josephkready/multica`

Personal fork of [`multica-ai/multica`](https://github.com/multica-ai/multica), pinned to whatever the trial is running on dante. Upstream is the source of truth for product docs, install scripts, and the CLI; this README is just the dante-specific operator's note.

## Why this fork exists

I'm trialling Multica as the kanban surface for the existing `claude-remote-*` + `nightshift` + `/start-work` agent workflow on dante. Full design + alternatives weighed in [Multi-Agent Task Board](https://dante.local/docs/projects/multi-agent-task-board.html) (`/home/jkready/docs/src/projects/multi-agent-task-board.md`). TL;DR from that doc:

- Two-week trial starting 2026-05-10. Decommission Taskwarrior `/todo` only if Multica sticks.
- Picked over Paperclip / Maestro / Symphony / Vibe Kanban because it's the only off-the-shelf option that ships a kanban board, multi-CLI agent auto-detection, and self-host-on-Linux without forcing me to re-author `~/.claude/skills/`.
- The fork is so I can carry local patches if needed during the trial. Default branch tracks `upstream/main`; sync with `git fetch upstream && git merge upstream/main` (or rebase, depending on what's diverged).

Upstream README — features, screenshots, the full CLI reference — is at [`multica-ai/multica#readme`](https://github.com/multica-ai/multica/blob/main/README.md). Don't duplicate it here.

## Where it runs

| Concern | dante answer |
|---|---|
| Host | dante (LAN box), Tailscale-accessible |
| Install | `multica setup self-host` via the upstream Docker compose |
| URL | `https://dante.local/multica/` (nginx reverse proxy, same pattern as `/docs/`, `/health/`) |
| Postgres state | `/var/lib/multica/` — **never** `/root/prod/multica/` (ansible-pull would clobber it) |
| Secrets | `/etc/multica.env` |
| Backups | Postgres volume + `pg_dump` daily; add to `~/.config/dante-sync/backup.yaml` before trial ends |
| Public access | None. LAN + Tailscale only. Not on `dante-live`. |

State / secrets / code split follows the dante convention documented in the global `CLAUDE.md`. If you're touching the deploy, read that first.

## How it fits the existing workflow

- **Code changes still go through `/start-work` → `/make-pr`.** Multica's daemon shells out to `claude` in the worktree path; the agent inherits `~/.claude/skills/`, so `/start-work`, `/make-pr`, `/todo`, etc. all keep working unchanged. (Verifying this end-to-end is open question #1 of the trial.)
- **Taskwarrior `/todo` runs in parallel for the full two weeks.** Don't migrate yet. End-of-trial decision: keep one, archive the other.
- **GitHub Issues → board sync is a ~50-line glue script**, not a Multica feature. Lives at `/usr/local/bin/gh-to-multica.sh` driven by `gh-to-multica.timer` (15-min cadence). One-way for now (GitHub → board); revisit bidirectional after a week of production. Sketch is in the design doc.
- **Codex / Gemini CLI cueing** lives in `~/.codex/AGENTS.md` and `~/.gemini/GEMINI.md` — small "when you surface a follow-up, create a Multica issue" block, mirroring how Claude's skill description cues `/todo` today.
- **Plan-mode span review is explicitly out of scope** for this tool. Pair with Outline per [Plan Mode Artifacts](https://dante.local/docs/projects/plan-mode-artifacts.html) when the pain crosses the threshold.

## Local development on this fork

If you're hacking on Multica itself (not just running it), upstream's [`CONTRIBUTING.md`](CONTRIBUTING.md) is canonical. Quick reminders for this fork:

```bash
# Sync with upstream
git fetch upstream
git merge upstream/main         # or rebase, depending on divergence

# Code changes — always via /start-work, never directly on main
# (the dante-global pre-commit hook blocks direct commits to main/master)

make dev                        # upstream dev script — Node 20+, pnpm 10.28+, Go 1.26+, Docker
```

Branch convention is the dante-global one: `<task>-<session-suffix>` worktrees at `/tmp/multica-<task>-<session>`, opened with `/start-work`, landed with `/make-pr`.

## Trial exit criteria (2026-05-10 → 2026-05-24)

Keep Multica if, by end of trial:

1. Claude / Codex / Gemini agents launched through the daemon find their existing skills/configs without modification.
2. The GitHub Issues sync glue is running idempotently against ≥3 repos.
3. The board is genuinely the place I look first to see "what's in flight" — and I haven't been re-falling back to `task list` more than once or twice.
4. Postgres backup is wired into the dante backup system.

If any of those are still red on day 14, drop Multica, stay on Taskwarrior + `/todo`, and revisit when a contender lands a built-in GitHub Issues importer.

## License

Inherits MIT from upstream. See [`LICENSE`](LICENSE).
