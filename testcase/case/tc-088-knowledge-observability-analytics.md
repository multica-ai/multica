# TC-088 Knowledge Observability Analytics

Associated issues: OPE-2656
Gitee PRs: TBD

Summary: Records knowledge retrieval, injection, usage, feedback, and task outcome signals, then exposes workspace and per-knowledge analytics.

Affected files:
- `server/migrations/121_knowledge_observability.up.sql`
- `server/migrations/121_knowledge_observability.down.sql`
- `server/pkg/db/queries/knowledge.sql`
- `server/pkg/db/generated/knowledge.sql.go`
- `server/pkg/db/generated/models.go`
- `server/internal/service/knowledge.go`
- `server/internal/service/task.go`
- `server/internal/handler/knowledge.go`
- `server/cmd/server/router.go`

Tests:
- `go test ./internal/handler -run 'TestKnowledge|TestClaimTaskByRuntime_InjectsRelevantKnowledgeContext|TestClaimTaskByRuntime_DoesNotInjectArchivedDeprecatedKnowledge'`

Commit SHAs: TBD
