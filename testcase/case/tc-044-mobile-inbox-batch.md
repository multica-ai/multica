Purpose: Verify that the mobile inbox screen supports batch mark-as-read and batch archive operations.

Preconditions: The Multica mobile app is running (or H5/responsive mobile view). The user is signed in. The inbox contains multiple unread notification items (at least 3-5 items for meaningful batch testing).

User flow: Open the Inbox screen on mobile. Verify there are multiple unread notifications displayed. Locate the batch operation controls — this may be a "全部已读" (Mark all read) button or a selection mode with checkboxes. Trigger "batch mark as read" action. Verify that all previously unread items now show as read (visual indicator changes). Generate new notifications to populate the inbox again. Locate the "批量归档" (Batch archive) or "归档已读" (Archive read) control. Trigger the batch archive action. Verify that archived items are removed from the inbox list or moved to an archived section.

Expected results: Batch mark-as-read changes the visual state of all unread items to read in one operation. The unread count badge (if present) updates to reflect the batch read. Batch archive removes read items from the active inbox view. The archive operation is not destructive — items should be accessible in an archive view if one exists. Both operations provide feedback (toast, animation, or count update) confirming the action completed. Operations handle empty-state gracefully (no error if no items match the batch criteria).

Notes for automation: Look for batch action buttons by their text labels ("全部已读", "批量归档", "归档已读", "Mark all read", "Archive"). The inbox may require generating test notifications first (e.g., by having another user mention the test user in comments). Verify state changes by checking notification item styling (read vs unread visual differentiation).
