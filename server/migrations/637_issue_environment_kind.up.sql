-- What an environment is: a server reached at its URL, or a desktop app whose
-- window only the machine running it can bring forward. A constant default
-- adds the column without rewriting the table.
ALTER TABLE issue_environment ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'server';
