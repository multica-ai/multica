Purpose: Verify that issue identifiers (e.g., OPE-123) are used in URLs and links throughout the application, replacing raw UUIDs for a better user experience.

Preconditions: The Multica web app is reachable. The user is signed in. At least one issue exists with an assigned identifier (project prefix + number).

User flow: Open the Issues list page. Click on an issue to open its detail. Observe the browser URL — it should contain the issue identifier (e.g., `/issues/OPE-123`) rather than a UUID. Copy the issue link (if a copy-link feature exists). Create a comment that mentions another issue using its identifier. Click the mention link to verify it navigates correctly.

Expected results: Issue detail URLs use the human-readable identifier format (`/{workspaceSlug}/issues/{identifier}`). Issue mention links in comments resolve to the correct issue when clicked (cmd-click opens in new tab). The copy-link feature produces a URL with the identifier. The application handles both UUID and identifier formats in URLs gracefully (redirecting UUID URLs to identifier URLs). WebSocket event handlers correctly resolve identifiers to UUIDs internally.

Notes for automation: Check the browser URL after opening an issue — it should match the identifier pattern. Test issue mention linking by creating a comment with `#OPE-123` or `@issue` syntax and clicking the rendered link.
