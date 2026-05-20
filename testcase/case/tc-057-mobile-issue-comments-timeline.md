Purpose: Verify that mobile issue detail shows comments and timeline activity.

Preconditions: The Multica mobile app is running, or the web app is opened in an H5/responsive mobile viewport. The user is signed in. An issue exists with at least one comment and one timeline/activity event.

User flow: Open the Issues list on mobile. Tap an issue that has comments and timeline activity. Scroll through the issue detail page. Locate the comment section and timeline/activity section. Add a short comment if the mobile UI supports it, then refresh or reopen the issue detail.

Expected results: Mobile issue detail displays the issue title/description plus existing comments and timeline/activity entries. Comments are not missing from the detail view. Timeline entries are visible in chronological context and remain visible after refresh. Adding a comment from mobile, when supported, appends it to the visible comments section.

Notes for automation: Use a mobile viewport when running against the web H5 experience. Locate sections by visible text such as "Comments", "Activity", "Timeline", "评论", or "时间线". This case specifically covers the recent mobile fix for missing comments/timeline on issue detail.
