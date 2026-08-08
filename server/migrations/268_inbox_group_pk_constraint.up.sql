-- Step 2 of the two-step primary key: adopt the index built in 267. This takes
-- a brief ACCESS EXCLUSIVE lock to flip catalog state, but performs no scan and
-- no build.
ALTER TABLE inbox_group
    ADD CONSTRAINT inbox_group_pkey PRIMARY KEY USING INDEX inbox_group_pkey_idx;
