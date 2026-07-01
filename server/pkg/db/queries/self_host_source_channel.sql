-- name: UpsertSelfHostSourceChannel :one
INSERT INTO self_host_source_channel (
    schema_version,
    channel,
    instance_hash,
    subject_hash,
    source_other,
    domain,
    domain_md5
) VALUES (
    $1, $2, $3, $4, sqlc.narg('source_other'), sqlc.narg('domain'), sqlc.narg('domain_md5')
)
ON CONFLICT (instance_hash, subject_hash) DO UPDATE SET
    schema_version = EXCLUDED.schema_version,
    channel = EXCLUDED.channel,
    source_other = EXCLUDED.source_other,
    domain = EXCLUDED.domain,
    domain_md5 = EXCLUDED.domain_md5,
    last_received_at = now(),
    report_count = self_host_source_channel.report_count + 1
RETURNING *;
