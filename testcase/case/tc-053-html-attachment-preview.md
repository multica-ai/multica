Purpose: Verify that HTML attachments and attachment previews render through the unified Attachment component and support preview navigation.

Preconditions: The Multica web app is reachable. The user is signed in. An issue or wiki page can contain an uploaded HTML attachment with an internal fragment link such as `#section-two`.

User flow: Open an issue or wiki page containing an HTML attachment. Open the attachment preview. Verify the preview appears in a modal or preview route without downloading the file immediately. Click an internal fragment link inside the preview. Use the "open in new tab" action if it is available. Return to the original issue or wiki page and verify the attachment card still renders normally.

Expected results: HTML attachments render with the same attachment card/preview behavior as other supported attachments. The preview opens successfully. Internal `#fragment` links scroll to the matching section inside the iframe preview. The open-in-new-tab action is visible where expected and opens the preview without leaving the original issue context. Existing image/file attachment cards continue to display correctly.

Notes for automation: Use a small fixture HTML file with visible headings and a fragment anchor. Assert preview success through visible heading text inside the preview and through the original attachment filename/card on return.
