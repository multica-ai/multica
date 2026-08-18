-- Validates the format constraint added NOT VALID in 330. Existing rows all
-- hold one of the 7 built-in keys, which match the pattern, so this scan
-- confirms rather than repairs. SHARE UPDATE EXCLUSIVE — reads and writes
-- continue during the scan.
ALTER TABLE issue VALIDATE CONSTRAINT issue_status_format_check;
