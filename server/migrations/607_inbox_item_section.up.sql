-- MUL-8483: the Inbox section ("action" or "activity") a notification was
-- routed to by its recipient's preferences at write time.
--
-- NULL means "classify by type", the rule every row followed before routing
-- existed, so existing rows need no backfill and older servers keep writing
-- valid rows. Values are validated in code (inbox.ItemSection ignores anything
-- else); there is no CHECK constraint so the ALTER stays a catalog-only change
-- with no table scan.
ALTER TABLE inbox_item ADD COLUMN IF NOT EXISTS section TEXT;
