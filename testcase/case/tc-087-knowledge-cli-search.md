# TC-087 Knowledge CLI Search

Associated issues: OPE-2655
Gitee PRs: TBD

Summary: Adds `multica knowledge search` so agents can proactively search reviewed workspace knowledge with optional issue context.

Affected files:
- `server/cmd/multica/cmd_knowledge.go`
- `server/cmd/multica/main.go`
- `server/internal/handler/knowledge.go`
- `server/internal/service/knowledge.go`
- `server/internal/daemon/execenv/runtime_config.go`

Tests:
- `go test ./cmd/multica -run TestRunKnowledgeSearch`
- `go test ./internal/daemon/execenv -run TestBuildMetaSkillContentRelevantKnowledge`

Commit SHAs: TBD
