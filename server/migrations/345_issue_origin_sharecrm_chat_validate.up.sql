-- VALIDATE the CHECK added in 344 under SHARE UPDATE EXCLUSIVE.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
