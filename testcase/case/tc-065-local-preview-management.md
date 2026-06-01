Purpose: Verify that Fork local preview management can start previews, stream snapshots, clean previews by issue, and avoid isolated database regressions in self-host mode.

Preconditions: The Multica CLI and daemon are installed. The user has a local/self-host workspace profile. A disposable issue exists for preview attachment.

User flow:
1. Start a simple web app preview through `multica preview start --issue <issue-id> --cwd <app-dir> --port <port> -- <start-command>`.
2. Open the issue detail page and verify the preview is listed with status and URL.
3. Wait for preview snapshots to stream and verify the latest snapshot/thumbnail is visible where the UI exposes preview evidence.
4. Stop or clean previews associated with the issue.
5. In self-host mode, restart preview-related services and verify preview startup reuses the configured shared database instead of spawning an isolated Postgres unexpectedly.

Expected results:
- Preview startup attaches the preview to the requested issue and records a reachable URL.
- Snapshot streaming updates the preview evidence without manual refresh.
- Issue-based cleanup stops/removes only previews associated with that issue and is idempotent.
- Parallel preview build/start paths do not race when multiple previews are created.
- Self-host preview startup does not create an isolated database when the shared database is already configured.

Notes for automation: This case may be verified with CLI output plus browser evidence on the issue detail page. Preserve Docker volumes and local profile state; do not reset local data for this test.
