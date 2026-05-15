Purpose: Verify that System Agent Defaults and Personal Agent Defaults can be configured and are applied when agents claim tasks.

Preconditions: The Multica web app is reachable. The user is signed in as a workspace admin (for system defaults) or any member (for personal defaults). The Agents page has a section or tab for Agent Defaults.

User flow: Navigate to the Agents page. Locate the Agent Defaults section (system defaults should appear as inline rows in the agent list). Click on a system default to view its detail. Confirm it shows configuration fields (environment variables, instructions, etc.). For personal defaults: find or create a personal override for an agent. Fill in custom environment keys/values and save. Then trigger an agent task and verify the merged configuration is applied.

Expected results: System defaults appear in the agent list as distinguishable rows (labeled or visually distinct). Clicking a system default opens a detail view with editable configuration. Personal defaults can override system defaults per-member. The configuration merge follows the priority: personal override > system default > agent base config. Admin users can edit system defaults; non-admin users can only view system defaults but can create/edit their own personal defaults.

Notes for automation: The Agent Defaults detail page uses a tab layout similar to agent detail. Identify system vs personal defaults by their labels or visual treatment. Verify the duplicate action is available for creating personal copies from system defaults.
