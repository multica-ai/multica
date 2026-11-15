# Conditional issue writes

Every issue read carries a server-owned `revision` that advances by one on each
mutation. When another writer might touch the issue between your read and your
write, take the `revision` from the issue you read and pass it as
`--expected-revision <n>` on the mutating command, so the server applies the
change only if the issue is still at that revision:

```bash
multica issue status <issue-id> in_progress --expected-revision <revision>
multica issue update <issue-id> --title "..." --expected-revision <revision>
multica issue assign <issue-id> --to-id <agent-id> --expected-revision <revision>
```

On a match the write applies and the response carries the new revision. On a
mismatch the command fails with a revision conflict, changes nothing, and does
not advance the revision; the error names the current revision so you can re-read
and retry. Omit the flag to keep the prior unconditional behaviour. This closes
the read/compare/write race that a plain read followed by a write cannot.

