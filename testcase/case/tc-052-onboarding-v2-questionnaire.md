Purpose: Verify that onboarding presents the v2 per-question flow for source, role, and use-case selection.

Preconditions: The Multica web app is reachable. A fresh or reset test user is available so onboarding is shown after login.

User flow: Sign in as a user who has not completed onboarding. Start the onboarding flow. Answer the Source question, then proceed to the Role question, then the Use case question. Continue through runtime connection or workspace setup until the authenticated workspace experience is reached.

Expected results: Onboarding presents source, role, and use-case as separate question steps rather than one combined questionnaire. Each selected option is visibly retained when moving forward. Completing the required steps leads to the normal workspace page without a broken redirect. The runtime connection step still offers the expected install/connect guidance.

Notes for automation: Use a fresh seeded user or reset onboarding state through test fixtures before running. Locate options by visible card text and headings rather than internal step names.
