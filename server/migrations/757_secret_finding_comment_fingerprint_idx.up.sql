-- One finding per secret per comment; rescanning the comment finds it again.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_secret_finding_comment_fingerprint
    ON secret_finding (comment_id, fingerprint);
