Purpose: Verify that Fork Gitee integration handles webhook ping events, repository lookup by path/namespace, signed webhook mode, and pull request display in issue context.

Preconditions: The Multica web app and backend are reachable. A Gitee integration test repository is configured for the workspace, or backend webhook fixtures are available. A disposable issue exists.

User flow:
1. Configure or open the workspace Gitee integration settings.
2. Send a Gitee ping webhook event for the configured repository.
3. Send a pull request webhook event that references the repository by path/namespace rather than only numeric ID.
4. Repeat with sign mode enabled and include a valid signature.
5. Open the related issue detail page and verify the pull request list/card displays the Gitee PR.
6. Send an invalid signature request and verify it is rejected.

Expected results:
- Ping events are accepted and logged as connectivity checks without creating duplicate PR records.
- Repository lookup succeeds using Gitee path/namespace.
- Sign mode validates valid signatures and rejects invalid signatures.
- Pull request metadata is linked to the correct issue and rendered in the issue pull request list.
- UI labels and links point to Gitee, not GitHub-only wording or URL assumptions.

Notes for automation: If external Gitee webhooks are unavailable, use handler-level fixtures or local HTTP requests that mirror the Gitee payload and signature headers.
