# NEW31-MAP-v1 collector

This command is a single-purpose, read-only source adapter for the NEW31-MAP-v1
mapping review. It does not import data, run migrations, or start with the API
server.

The reviewed contract must classify every source column as either allowlisted or
denied. The collector fails closed when PostgreSQL is not exactly 17.10, a table
or column drifts, an enum is unknown, a core reference is orphaned, permissions
could expand, a task is nonterminal, an attachment is missing or corrupt, or
usage semantics are invalid.

Run it only against an isolated PostgreSQL 17.10 restore and its matching upload
snapshot. Set `DATABASE_URL` through the controlled runtime environment before
running:

```bash
go run ./cmd/new31_map_collector \
  --contract /controlled/path/new31-map-v1.json \
  --hmac-key-file /controlled/path/run.key \
  --attachments-root /controlled/path/uploads \
  --output /controlled/path/report.json
```

Requirements:

- `run.key` must contain at least 32 random bytes and grant no group/other
  access. The command overwrites, syncs, and removes it immediately after read,
  then clears the in-memory key on exit.
- The database role must be read-only. The command also sets
  `default_transaction_read_only=on`, uses one connection, and collects inside a
  `REPEATABLE READ READ ONLY` transaction.
- The output path must not exist. The report is created with mode `0600` and
  contains only contract field names, counts, keyed anonymous IDs, HMAC
  evidence, and rejection reason codes.
- A rejected collection still writes the redacted report and exits nonzero.

`internal/mapcollector/testdata` is synthetic PostgreSQL 17.10 fixture data. It
contains no production identifiers or source DDL and is not a production source
contract.
