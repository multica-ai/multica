Purpose: Verify that issue comment drafts are preserved when navigating away and returning to the issue detail page.

Preconditions: The Multica web app is reachable. The user is signed in. At least one issue exists.

User flow: Open an issue detail page. In the comment input area, type a partial comment (e.g., `This is a draft comment that should be preserved`). Do NOT submit the comment. Navigate away from the issue (click another issue in the list, or navigate to a different page via sidebar). Return to the same issue detail page. Observe the comment input area.

Expected results: After returning to the issue, the comment input area contains the previously typed draft text intact. The draft is specific to each issue (drafting on Issue A does not pollute Issue B's input). The draft is cleared after the comment is successfully submitted.

Notes for automation: Type text into the comment textarea, then navigate away using sidebar or issue list clicks. After returning, check the textarea's value. The draft preservation mechanism uses local storage or session state keyed by issue ID.
