Purpose: Verify that long comment bodies in issue detail can be collapsed and expanded, and that the collapse state toggles correctly.

Preconditions: The Multica web app is reachable. The user is signed in. An issue exists with at least one comment whose body is long enough to trigger the collapse threshold (typically > 10 lines or > 300 characters).

User flow: Navigate to an issue that has a long comment. Observe that the comment body is partially hidden with a "展开" (Expand) or equivalent collapse toggle visible. Click the expand toggle to reveal the full comment body. Verify the full text is now visible. Click the "收起" (Collapse) toggle to hide the comment body again. Verify the comment returns to its collapsed state.

Expected results: Comments exceeding the length threshold display in a collapsed state by default, showing only the first portion of the text. A visible toggle control (button or link with text like "展开"/"收起" or "Show more"/"Show less") appears below the truncated content. Clicking expand reveals the complete comment body. Clicking collapse returns to the truncated view. Short comments that do not exceed the threshold are displayed in full without any collapse control.

Notes for automation: Look for collapse toggle elements by their visible text label ("展开", "收起", "Show more", "Show less"). To test, first create a comment with sufficient length (e.g., 20+ lines of text) if none exists. The threshold behavior is purely frontend — no API call is needed on toggle.
