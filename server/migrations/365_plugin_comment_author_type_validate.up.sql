-- Validates the constraint migration 362 added NOT VALID.
--
-- Split out because VALIDATE CONSTRAINT takes SHARE UPDATE EXCLUSIVE — readers
-- and writers continue — while doing the scan that ADD CONSTRAINT would have
-- done under ACCESS EXCLUSIVE, stopping the comment table for its duration.
ALTER TABLE comment VALIDATE CONSTRAINT comment_author_type_check;
