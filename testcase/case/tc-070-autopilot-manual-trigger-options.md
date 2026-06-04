Purpose: Verify that AutoPilot manual trigger options let users choose a predefined payload for manual runs, and that the chosen value is visible in both run-only and create-issue execution contexts.

Related issue: OPE-2270.

Preconditions: The Multica web app and API are reachable. The user is signed in. At least one runnable agent or squad exists in the workspace, with an online runtime for run-only assertions.

User flow:
1. Open the Autopilot page and create a new autopilot.
2. In the configuration panel, add manual trigger options `production` and `test`.
3. Save the autopilot in `run_only` mode.
4. Reopen the autopilot detail page and verify the properties section shows both manual trigger options.
5. Click `Run now`. Verify a selector opens, choose `production`, and confirm.
6. Open the newest run detail/payload preview and verify `trigger_payload` is `production`.
7. Edit the autopilot, clear the manual trigger options, save, then click `Run now` again. Verify it triggers directly without a selector.
8. Repeat with a `create_issue` autopilot configured with `production` and `test`, choose `test` on manual run, and open the created issue.

Expected results: Create and edit forms persist the exact predefined options after trimming empty input and removing duplicates. When options exist, manual run requires choosing one option and sends that value as `trigger_payload`. The created `autopilot_run` has `source = manual` and a JSON string payload matching the selected option. In `run_only`, the agent task context includes the trigger payload. In `create_issue`, the created issue description includes `Trigger source: manual` and a `Trigger payload` block containing the selected value. When no options are configured, manual run keeps the legacy direct-trigger behavior and payload is empty. Webhook triggers and webhook payload display continue to work unchanged.

Notes for automation: Use the create/update API to seed an autopilot when UI setup is slow. Negative API checks should assert that configured options reject missing or unknown `trigger_payload` with HTTP 400 and do not create an `autopilot_run` row. Capture evidence from the autopilot detail properties, the manual run selector, and either run detail payload or the created issue description.
