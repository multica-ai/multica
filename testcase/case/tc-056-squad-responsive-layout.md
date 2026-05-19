Purpose: Verify that squad pages remain usable on narrow and desktop layouts.

Preconditions: The Multica web app is reachable. The user is signed in. At least one squad exists with multiple members and related issues.

User flow: Navigate to the Squads page on a desktop-width viewport. Open a squad detail page and verify member/status information and related issue sections are visible. Resize to a mobile-width viewport or use browser device emulation. Reopen or refresh the squad detail page and verify the same core sections remain reachable without horizontal overflow.

Expected results: Squad list and detail pages render correctly on desktop and mobile widths. Member cards, working status, related issues, and primary actions remain visible or accessible through responsive stacking. No key controls are clipped or hidden off-screen.

Notes for automation: Use agent-browser viewport controls or browser emulation to test a narrow viewport. Visual assertions can use visible section headings, action buttons, and absence of obvious horizontal overflow.
