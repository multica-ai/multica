Purpose: Verify that Fork agent dashboard and runtime usage views include owner filters, default time range, local-run usage, runtime token usage, and runtime owner search.

Preconditions: The Multica web app is reachable. The user is signed in. The workspace has at least two agents owned by different users and at least one local runtime with completed runs.

User flow:
1. Open the Agent Dashboard.
2. Verify the default time range is set to the expected recent-hour window.
3. Use the owner filter to switch between all owners and a specific owner.
4. Open an issue with local run history and compare issue total usage against the run list.
5. Open Runtime/Machines and search by owner username, runtime name, and provider.
6. Open a runtime detail/usage view and verify token usage fields are populated for Copilot/Cursor/DeepSeek-style providers where fixture data exists.

Expected results:
- Dashboard defaults to the configured hour range and does not show an empty or excessive historical window by default.
- Owner filter narrows dashboard rows and labels owners clearly.
- Issue total usage includes local run usage instead of only remote task usage.
- Runtime list displays owner avatar/name and supports username search.
- Runtime token usage fields handle provider-specific gaps without showing misleading zeroes or crashing.

Notes for automation: Use seeded runs with distinct owners where possible. If token usage is unavailable for a provider, verify the UI shows an explicit unavailable/blank state rather than an incorrect total.
