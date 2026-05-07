-- Fork-specific: drop the inbox folders feature. The concept didn't fit
-- mobile (drag-and-drop conflicted with swipe-to-archive) and saw little use.
-- Removed in JEH-650 along with all backend handlers and frontend UI.

DROP TABLE IF EXISTS inbox_folder_membership;
DROP TABLE IF EXISTS inbox_folder;
