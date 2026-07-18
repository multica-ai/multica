-- Optional catalog icon key for workspace custom property definitions. The empty
-- string represents no icon so existing rows and older clients keep their
-- current appearance. PostgreSQL can add this constant default without a
-- table rewrite.
ALTER TABLE issue_property
    ADD COLUMN icon TEXT NOT NULL DEFAULT '';
