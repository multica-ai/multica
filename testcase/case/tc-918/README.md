# TC: OPE-918

## 关联信息
- Issue: OPE-918
- 特性摘要: fix(gitee): handle ping events, use path/namespace for repo lookup, support sign mode (OPE-918)

## 涉及源文件
- packages/core/api/client.ts
- packages/core/types/github.ts
- packages/core/types/index.ts
- packages/views/issues/components/pull-request-list.tsx
- packages/views/settings/components/integrations-tab.tsx
- server/cmd/server/router.go
- server/internal/handler/gitee_test.go
- server/internal/handler/gitee.go
- server/internal/handler/github.go
- server/internal/handler/pullrequest_test.go
- server/internal/handler/pullrequest.go
- server/internal/issueguard/duplicate.go
- server/migrations/094_gitee_integration.down.sql
- server/migrations/094_gitee_integration.up.sql
- server/pkg/db/generated/github.sql.go

## Commit SHA
- ea4178a5b81ecd432ff184a5b14ce29129646781
- 29f8dd32a479715a1bd5837fac5c21b63bd4f23f
