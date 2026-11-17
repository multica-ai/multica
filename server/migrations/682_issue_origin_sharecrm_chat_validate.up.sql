-- Validate the check migration 681 left behind.
--
-- When 667/668 already validated a check that includes sharecrm_chat, 681
-- did not invalidate it and this statement returns without scanning issue.
-- A database that reaches 681 still NOT VALID (upstream 667/668 never
-- allowed sharecrm_chat) scans here under SHARE UPDATE EXCLUSIVE, separate
-- from 681's ACCESS EXCLUSIVE catalog swap.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
