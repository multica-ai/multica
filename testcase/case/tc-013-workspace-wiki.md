Purpose: Verify that the workspace Wiki feature allows creating, editing, auto-saving, and navigating multi-page wiki documents, including attachment uploads.

Preconditions: The Multica web app is reachable. The user is signed in. The sidebar shows a `Wiki` navigation entry.

User flow: Click `Wiki` in the sidebar to open the wiki page. Create a new wiki page by clicking the appropriate add/new button. Enter a title and body content in the editor. Navigate away from the page (click another wiki page or another sidebar item) and then return. Verify the content was auto-saved. Upload an attachment (image or file) using the attachment upload button in the wiki editor. Create a second wiki page and verify both pages appear in the wiki navigation/page list.

Expected results: The Wiki section is accessible from the sidebar. New wiki pages can be created with a title and rich-text body. Content is auto-saved when the user navigates away or loses focus. Attachments can be uploaded via the dedicated upload button and appear inline or as links in the wiki content. Multiple wiki pages coexist and are listed in a navigable page tree or list. The wiki URL follows the pattern `/{workspaceSlug}/wiki/{pageId}`.

Notes for automation: Use visible text labels for the Wiki sidebar link, new-page button, and upload button. Auto-save can be verified by navigating away and returning, then checking content persistence. The wiki editor is a rich-text editor that accepts markdown-like input.
