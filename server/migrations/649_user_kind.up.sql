-- Service accounts (MUL-9163): a machine identity is a user row that belongs
-- to exactly one workspace and authenticates with an ordinary mul_ PAT. The
-- kind column classifies the row without inventing a new actor type: every
-- polymorphic *_type constraint already enumerates 'member' for workspace
-- authorship, so a service account flows through existing membership-based
-- authorization untouched. Interactive login (email OTP) is refused for
-- kind='service' rows; they have no mailbox to receive a code.
ALTER TABLE "user"
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'human'
        CONSTRAINT user_kind_check CHECK (kind IN ('human', 'service'));
