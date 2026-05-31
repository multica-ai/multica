# TC: OPE-20

## 关联信息
- Issue: OPE-20
- 特性摘要: fix: consolidate OPE-20 + OPE-88 duplicate binding queries and migrations

## 涉及源文件
- server/internal/handler/agent.go
- server/internal/handler/auth_dingtalk.go
- server/migrations/055_external_account_binding.down.sql
- server/migrations/055_external_account_binding.up.sql
- server/pkg/db/generated/agent.sql.go
- server/pkg/db/generated/external_account_binding.sql.go
- server/pkg/db/generated/models.go
- server/pkg/db/generated/notification.sql.go
- server/pkg/db/queries/external_account_binding.sql
- server/pkg/db/queries/notification.sql

## Commit SHA
- 8c54cc42066566e0b09c3a1ab5af0323923992ab
