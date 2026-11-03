-- The freshness review request a document last opened (plan 10.9). A pointer
-- with no foreign key: the Librarian reads the issue before it opens another.
ALTER TABLE knowledge_link ADD COLUMN IF NOT EXISTS review_issue_id UUID;
