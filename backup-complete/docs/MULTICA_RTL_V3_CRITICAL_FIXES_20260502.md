# Multica RTL v3 — Critical Fixes (2026-05-02)

## Summary
Six critical fixes applied after Claude Code Opus review of the initial RTL contextual auto-detect implementation. Docker image `multica-web-rtl:v3.20260502` deployed and verified.

## Changes Applied

### Fix 1+3: `lang="ar"` Restoration
- **File:** `apps/web/app/layout.tsx`
- **Change:** Reverted `lang="en"` back to `lang="ar"` for screen reader + SEO support
- **Reason:** Arabic-first product requires proper HTML language attribute

### Fix 2: Desktop Shortcut Order-Independence
- **File:** `apps/desktop/src/renderer/index.html`
- **Change:** Full rewrite of Ctrl+Shift handler to match DirectionScript logic
- **Logic:** Fires on ANY keydown when both `ctrlKey` and `shiftKey` are pressed (order-independent)
- **Also:** `stopImmediatePropagation()` to prevent editor conflicts

### Fix 4: `overflow-x: hidden` Removal
- **File:** `apps/web/app/globals.css`
- **Change:** Removed `overflow-x: hidden` from `html, body`
- **Reason:** Breaks `position: sticky` behavior

### Fix 5: Tracker Cleanup
- **File:** `~/.hermes/scripts/graph_updater.py`
- **Change:** `remove_orphan_nodes()` now returns `orphan_hashes` which are deleted from `tracker.jsonl`
- **Reason:** Prevents tracker from growing indefinitely with orphaned facts

### Fix 6: Shared Lock on read_new_facts
- **File:** `~/.hermes/scripts/fact_extractor.py`
- **Change:** Added `fcntl.LOCK_SH` / `fcntl.LOCK_UN` around JSONL read
- **Reason:** Prevents partial reads when writer holds exclusive lock

### Bonus: Dead Code Removal
- **Files:** `graph_updater.py`, `session_summarizer.py`
- **Change:** Removed `+ 0` noise from `node_count` / `fact_count`

## RTL Strategy (v3 Final)
| Layer | Setting |
|-------|---------|
| `<html>` | `lang="ar"` (NOT `dir="rtl"`) |
| Content containers | `dir="auto"` (browser auto-detects Arabic vs code) |
| Base layout | `direction: ltr` on `html, body` |
| Toggle | `[data-multica-dir] *` via `!important` |
| Code blocks | Forced `direction: ltr !important` |
| Keyboard | Ctrl+Shift (order-independent, any keydown) |

## Deployment
- **Docker Image:** `multica-web-rtl:v3.20260502`
- **Container:** `multica-frontend` on `localhost:3000`
- **Network:** `multica-net`
- **Git Commit:** `f0aa04eb`
- **Branch:** `fix/daemon-api-timeout-heartbeat`
- **Repo:** `AnwarPy/multica`

## Files Modified
1. `apps/web/app/layout.tsx`
2. `apps/web/app/globals.css`
3. `apps/desktop/src/renderer/index.html`
4. `~/.hermes/scripts/graph_updater.py`
5. `~/.hermes/scripts/fact_extractor.py`

## Claude Opus Review Score: 8.7/10 (up from 7/10)
