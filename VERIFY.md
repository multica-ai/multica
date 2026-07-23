# FIR-3388 local verification

This flow verifies the user-visible capability result without production data.

1. Run `make setup-worktree` and `make start-worktree`.
2. Sign in to the local web app with a local owner account.
3. Open an existing local agent, then open the `Capabilities` tab.
4. Confirm the page loads the same effective tool decisions returned by:
   `multica agent capabilities <agent-id> --output json`.
5. For one allowed tool and one denied tool, open the permission detail and
   confirm the displayed result and explanation match the CLI response.
6. Change one local-only policy, refresh the page, and confirm both the
   `Capabilities` tab and CLI reflect the same effective result.
7. Restore the local policy fixture after the check.

Do not connect this flow to production or use a production database copy.
