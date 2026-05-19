Purpose: Verify that the agent transcript dialog supports switching sort direction.

Preconditions: The Multica web app is reachable. The user is signed in. At least one issue or agent task has a transcript containing multiple messages or events with visible ordering.

User flow: Open an issue with a completed agent run or open an agent task from the Tasks panel. Open the transcript dialog. Observe the initial message order. Click the sort direction control. Verify the message order reverses. Click the control again and verify the original order returns.

Expected results: The transcript dialog has a visible sort direction control. Switching the control reverses the transcript ordering between newest-first and oldest-first without losing messages. The selected order is reflected immediately in the dialog and remains usable when the transcript has many entries.

Notes for automation: Locate the transcript dialog by visible labels such as "Transcript", "Execution log", or task details. Use the first and last visible message timestamps or unique message snippets to confirm order reversal.
