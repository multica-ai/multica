-- VALIDATE the CHECK added in 314 under SHARE UPDATE EXCLUSIVE.
ALTER TABLE issue VALIDATE CONSTRAINT issue_origin_type_check;
