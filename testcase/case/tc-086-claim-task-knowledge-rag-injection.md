# TC-086 ClaimTask Knowledge RAG Injection

Associated issues: OPE-2654
Gitee PRs: TBD

Summary: ClaimTask injects high-confidence published RAG knowledge into `knowledge_context`, records retrieval/injection events, and daemon briefs render a `Relevant Knowledge` section.

Affected files:
- `server/internal/service/knowledge.go`
- `server/internal/handler/agent.go`
- `server/internal/handler/daemon.go`
- `server/internal/daemon/types.go`
- `server/internal/daemon/daemon.go`
- `server/internal/daemon/execenv/execenv.go`
- `server/internal/daemon/execenv/runtime_config.go`
- `server/pkg/db/queries/agent.sql`
- `server/pkg/db/queries/knowledge.sql`
- `server/pkg/db/generated/agent.sql.go`
- `server/pkg/db/generated/knowledge.sql.go`

Tests:
- `go test ./internal/handler -run 'TestClaimTaskByRuntime_InjectsRelevantKnowledgeContext|TestClaimTaskByRuntime_DoesNotInjectArchivedDeprecatedKnowledge'`
- `go test ./internal/daemon/execenv -run 'TestBuildMetaSkillContentRelevantKnowledge|TestBuildMetaSkillContentOmitsRelevantKnowledgeWhenEmpty'`

Commit SHAs: TBD
