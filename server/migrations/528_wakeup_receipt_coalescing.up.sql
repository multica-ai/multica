-- Existing receipts retain their original evidence and drain normally.
ALTER TABLE issue_wakeup_receipt ADD COLUMN coalesce_key text;
