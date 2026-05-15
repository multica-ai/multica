Purpose: Verify that a user can duplicate an agent they own, with environment variables properly masked, and that the duplicate creates a new independent agent.

Preconditions: The Multica web app is reachable. The user is signed in. The user owns at least one agent that has environment variables configured.

User flow: Navigate to the Agents page. Find an agent owned by the current user. Right-click on the agent card (or use the agent's context menu). Confirm a `Duplicate` option is available. Click `Duplicate`. In the create-agent dialog that appears, confirm the agent name is pre-filled as a copy (e.g., `Original Name (2)`) and the runtime field is set. Confirm that environment variable keys are copied but values are empty/masked (not exposing secret values). Submit the form.

Expected results: The duplicate dialog pre-fills the agent name with an incremented suffix. Environment variable keys are listed but their values are empty (security: no secret leakage). The runtime_id is NOT pre-filled if the original runtime is locked to another owner. After submission, a new agent appears in the list with the duplicated name. The new agent is owned by the current user and is independent from the original.

Notes for automation: Use the right-click context menu or the visible Duplicate button/menu item. Verify env key presence but empty values by inspecting the form fields in the dialog. The naming convention is `Name (N)` where N increments.
