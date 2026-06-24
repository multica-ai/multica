# TC-217: 频道回复可靠性 — 多字节标题截断 + Agent 建线程外键修复（OPE-3457）

Purpose: Verify two distinct root causes that both surfaced as a generic "failed to create reply thread" and made channel messages unreplyable: (a) byte-slicing (`content[:N]`) that split a multi-byte UTF-8 rune (CJK / emoji) in the auto-derived thread title → invalid UTF-8 rejected by PostgreSQL on INSERT (SQLSTATE 22021); (b) `channel_thread.created_by REFERENCES "user"(id)` violated when an agent (which lives in the `agent` table, not `user`) creates a thread — the @-mention-then-agent-replies path, and the agent-creates-issue-from-channel path.

## Associated issue / PR
- OPE-3457（Channel 优化 v0.0.3 功能批次，本子特性随主线 PR 交付）
- Gitee PR: !430（同分支 `feat/ope-3457-channel-convert-issue`）
- Commit SHA: 待补（maintainer 补充）

## One-line summary
修复两条"回复失败仅提示通用错误"的根因：(1) 按 UTF-8 rune 边界安全截断线程标题，避免 CJK/emoji 被字节切断产生无效 UTF-8（SQLSTATE 22021）；(2) Agent 创建/回复建线程时 `created_by` 置 NULL，避免 agent UUID 违反 `channel_thread_created_by_fkey`（SQLSTATE 23503）。两者此前都仅回通用 "failed to create reply thread"。

## Root causes

### (a) 多字节标题截断（SQLSTATE 22021）
`ReplyToMessage`（及其它线程创建路径）用 `len(content) > N` + `content[:N]` 截断消息内容作为线程标题。`len()` 按字节计数，`[:N]` 会把一个多字节 rune（如中文/emoji）从中间切断，留下孤立的 lead byte，PostgreSQL INSERT 时报 `invalid byte sequence for encoding "UTF8"`（SQLSTATE 22021），整条 INSERT 失败 → 回复 500 → 用户看到通用 "failed to create reply thread"，且该消息此后无法被任何人回复。

实测复现（线上 preview 后端 slog）：消息 `ed5bad62…`（内容 "Issue WS-16 的内容如下：…"）第 50 字节恰为 "试"(e8 af 95) 的 lead byte `0xe8`，`[:50]` 截断后产生孤立 `0xe8` → 22021。这是用户本次实际命中的根因。

### (b) Agent 建线程外键违例（SQLSTATE 23503）
`channel_thread.created_by REFERENCES "user"(id)`，而 agent 在 `agent` 表不在 `user` 表。`ReplyToMessage` 把 `authorID`（agent 时 = agent UUID）直接塞 `created_by` → 外键违例 → 同样回通用 "failed to create reply thread"。触发场景：@mention agent 在一条无线程的顶层消息上 → agent 回复自动建线程（`prompt.go:108` 正是这么指示 agent 的）。`issue.go:2644` 的 `ensureThreadForMessage(..., issue.CreatorID)` 在 agent 创建 issue（`CreatorType=="agent"`）带 `source_message_id` 时同样炸 FK。`CreateThread` handler 早已写对（agent → NULL），`ReplyToMessage`/`issue.go` 未沿用该 pattern。

## Affected source files
- `server/internal/handler/channel.go` — 新增 `truncateUTF8(s, maxBytes)`：回退到 rune 边界，保证返回值是合法 UTF-8 且长度 ≤ maxBytes；新增 `unicode/utf8` import。
- `server/internal/handler/channel_v2.go` — 标题截断 4 处 `content[:N]` → `truncateUTF8`；`ReplyToMessage` 加 `threadCreatedBy` guard（`authorType == "agent"` → NULL），`ConvertMessageToIssue` 隐式建线程保持用人类 `userUUID`（安全）。
- `server/internal/handler/channel_reflow.go` — reflow 建线程标题 `content[:N]` → `truncateUTF8`；`ensureThreadForMessage` docstring 收紧 FK 契约（caller 传 agent UUID 时须传零值 NULL）。
- `server/internal/handler/issue.go` — `CreateIssue` 中 `ensureThreadForMessage(..., issue.CreatorID)` 改为按 `issue.CreatorType == "agent"` guard（agent → NULL）。

## Verification points
1. 任意含 CJK/emoji 且第 50（或 80）字节落在多字节 rune 中间的消息，回复返回 201（此前 500 / 22021）。
2. 自动派生的线程标题为合法 UTF-8（`utf8.ValidString`），且是被截断 rune 之前的内容前缀，无孤立 lead byte。
3. 纯 ASCII 内容截断行为不变（ASCII 下 rune 边界 == 字节边界，截断点与长度与旧逻辑一致）。
4. Agent 回复一条无线程的顶层消息，自动建线程返回 201（此前 500 / 23503），且线程 `created_by` 为 NULL。
5. Agent 从频道消息创建 issue（带 `source_message_id`）建隐式线程不再因 FK 失败；手工（人类）转换路径不变。
6. 回复失败时后端 `slog.Error` 仍记录真实 SQL 错误 + 上下文（`channel_id`/`root_message_id`/`author_id`/`error`），便于诊断；前端 toast 显示后端返回的 `error` 字段（`channels-page.tsx` `replyMutation.onError`）。

## Tests
- `server/internal/handler/channel_v2_test.go` `TestV2ReplyMultibyteThreadTitle`：发 `49 ASCII + "中"(3 字节) + 后缀` 的顶层消息（"中" lead byte 落在第 49 字节索引，使 `[:50]` 必切断该 rune），回复 → 断言 201、线程标题合法 UTF-8 且等于 49 个 ASCII。RED 验证：回退为 `content[:50]` 后以 22021 / 500 失败，错误体与用户实测一致。
- `server/internal/handler/channel_v2_test.go` `TestV2AgentReplyAutoCreatesThreadWithNullCreator`：用 `createHandlerTestAgent` + `createHandlerTestTaskForAgent` 建一个 test-workspace 的 agent + 运行中 task，带 `X-Agent-ID`/`X-Task-ID` 头让 `resolveActor` 识别为 agent，回复一条无线程顶层消息 → 断言 201、线程 `created_by` 为 NULL（response `CreatedBy` 为空 + DB 行 `created_by IS NULL`）。RED 验证：把 guard 置为恒 `authorID`（agent UUID）后以 23503 / 500 "failed to create reply thread" 失败。

## Notes
- `channel_thread.title` 为 `TEXT`（无长度约束），50/80 截断仅为预览标题的 UX 选择；切到 rune 安全截断不改变语义，仅修复多字节边界。
- `created_by` 仅用于人类删除线程权限判断（`channel_thread.go` DeleteChannelThread `thread.CreatedBy != requestUserID`）与 `isCreator`（`channel_v2.go`），均人类-only；agent → NULL 不丢功能，与 `CreateThread` handler 既有 convention 一致。
- `LinkQuickCreateIssueToSource` 的 `requesterID` 恒为人类（仅 `issue.go:2204` 人类 `/issues/quick-create` 端点经 `requireUserID` 入队 quick-create 任务），故该路径 FK 安全，无需 guard。
- 已知不相关失败：`TestV2LockedChannelPermission`（频道锁定权限门槛）与 `TestExportIssue_PDF`（测试环境缺 PDF 渲染二进制）在 clean tree 亦失败，非本特性引入。
