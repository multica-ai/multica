Purpose: Verify that pasting images into the issue comment editor uploads them correctly and displays inline, with proper error feedback on upload failure.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists. The user has an image in their clipboard (e.g., from a screenshot).

User flow: Open an issue detail page. Click into the comment input area. Paste an image from the clipboard (Ctrl+V / Cmd+V). Observe the upload progress indicator. Wait for the upload to complete. Submit the comment.

Expected results: Pasting an image triggers an upload with a visible progress indicator (loading state or thumbnail placeholder). After upload completes, the image appears inline in the comment editor preview. If upload fails (e.g., file too large, network error), an error toast or message is shown explaining the failure. The submitted comment renders the image correctly in the timeline. Multiple images can be pasted sequentially.

Notes for automation: Image paste requires clipboard manipulation. Use a small test image. Verify upload by checking for the image element in the comment editor after paste. Error feedback testing requires simulating a failed upload (e.g., disconnected network or oversized file).
