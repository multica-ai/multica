-- Drop bot_union_id. Application code is expected to be rolled back to a
-- version that does not read this column before the down migration is
-- applied — there is no transitional period in which the runtime expects
-- the column and the schema lacks it.
ALTER TABLE lark_installation
    DROP COLUMN IF EXISTS bot_union_id;
