-- Tags on a proposal (spec 4.4): a standing approval's match can name tags, so a
-- proposal carries them. The filing sets them from the payload's `tags` array
-- (proposal_kinds.ProposalTags); a proposal with none has an empty array. A
-- constant default is metadata-only, so the table is not rewritten.
ALTER TABLE proposal ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}';
