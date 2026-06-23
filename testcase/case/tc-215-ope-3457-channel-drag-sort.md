# TC-215: 频道列表同组拖拽排序持久化（OPE-3457 #5 stretch）

Purpose: Verify that dragging a channel within the same group reorders it (not just moves group membership), that the new order persists after a page refresh, and that the within-group sort order is deterministic (no two channels sharing one position).

Preconditions: The Multica web app is reachable on the PR build. The user is signed in and can manage channels. A channel group (or the ungrouped zone) with ≥3 channels exists.

User flow:
1. Open the channel list. Note the current within-group order of ≥3 channels.
2. Drag channel A to a new position within the same group (e.g. drop it before channel B).
3. Verify the channel list reorders so A now appears before B (and the relative order of the others is preserved).
4. Reload the page.
5. Verify the new order is preserved (persisted).
6. Repeat a couple of swaps to confirm the midpoint-position logic keeps the sort deterministic (no flicker / reorder jitter).

Expected results: Same-group drag reorders the channel (not a no-op / not just a group move). The order persists across reload. Sort is deterministic — dragging onto a channel does not produce two channels sharing one position.

Notes for automation: `resolveDropTarget` computes a midpoint position `(prev+cur)/2` (position is DOUBLE PRECISION); the no-op guard is now `over.id === draggedId` (not a same-position equality). Assert DOM order of channel list items before/after drag and after reload. The persistence is verified by reload, not by a local-only optimistic update.
