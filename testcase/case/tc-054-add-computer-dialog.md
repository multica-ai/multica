Purpose: Verify that the Runtimes "Add a computer" dialog uses the simplified connection flow.

Preconditions: The Multica web app is reachable. The user is signed in and can view Runtimes or Computers.

User flow: Navigate to Runtimes/Computers. Click the Add computer button. Observe the dialog content. Select the local or remote computer option if the dialog asks for one. Verify the install or daemon connection command is shown clearly. Close the dialog and reopen it to confirm the same simplified path is available.

Expected results: The Add computer dialog opens without layout overflow. It presents a simplified set of choices and command instructions rather than the older cluttered flow. CLI command literals are readable and copyable. Closing and reopening the dialog does not lose required connection guidance.

Notes for automation: Locate controls by visible labels such as "Add computer", "Local machine", "Remote", "Copy", or command text beginning with `multica`. Do not execute the shown install command during browser regression.
