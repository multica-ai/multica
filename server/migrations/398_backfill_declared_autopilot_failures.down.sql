-- Irreversible data correction: a failed automation must not become successful
-- again merely because the server binary is rolled back. The task result keeps
-- the original provider output for audit and future reclassification.
SELECT 1;
