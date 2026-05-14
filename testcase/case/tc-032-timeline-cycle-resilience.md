Purpose: Verify that the issue timeline handles recursive/cyclic reply chains gracefully without crashing, using cycle detection and error boundaries.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with a complex comment thread (preferably one that previously triggered a crash, or can be artificially constructed with deeply nested replies).

User flow: Open an issue that has a deep or complex comment thread structure. Scroll through the timeline. If a cycle or corruption exists in the reply chain data, the page should render gracefully rather than crashing.

Expected results: The timeline renders without crashing or showing a white screen. If a cycle is detected in reply relationships (comment A → B → A), the cycle is broken and the comments render independently without infinite recursion. If any single comment thread fails to render, an error boundary catches it and shows a fallback message without bringing down the entire timeline. The error boundary message is informative (not a raw stack trace).

Notes for automation: This is primarily a stability/resilience test. Normal usage should not trigger cycles, but verify the timeline renders for issues with many threaded replies. Check that the page does not show a blank/error state.
