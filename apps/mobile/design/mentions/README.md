# iOS AI 会话：@ 引用与交互优化

原生实现已完成，改动范围限于 AI 会话及其必要依赖。
[ios-mentions-ui-v5.png](ios-mentions-ui-v5.png) 记录引用交互设计；图片和 HTML 保留为设计过程记录。
当前视觉以[移动端风格对齐记录](style-alignment.md)中的既有控件规范为准。
原生验证范围和待验证项见 [UI 审查记录](ui-review.md)。

## 使用 @ 引用

输入 `@` 或使用输入框工具栏的 @ 按钮，打开原生引用选择页。

| 分类 | 展示范围 | 发送时的行为 |
| --- | --- | --- |
| 会话 | 当前账号、工作区、电脑上的未归档会话，排除当前会话 | 提供引用 ID 和有界的文本摘录 |
| 任务 | 当前工作区内有权查看的任务 | 提供引用 ID、标题和描述摘录 |
| 智能体 | 当前工作区智能体，展示调用限制 | 发送后调用所选智能体，使用其自身配置 |

只有这三个分类。各分类首次展示最多 8 条，独立加载更多；切换分类保留分页和选择。
任务和会话在服务端搜索、分页；智能体沿用现有目录接口，在移动端分页展示。

## 会话列表分页

电脑会话列表按目录首次请求最多 8 条，每个目录有独立的「加载更多」。目录标题旁的数字来自完整计数，
不随已加载行数变化。收起的目录不请求会话页；展开时读取第一页。搜索与归档筛选在服务端执行。
@ 会话引用和 Side 会话选择也使用服务端分页，避免从其他入口重新下载整份会话目录。

| 接口 | 用途 |
| --- | --- |
| `GET /local/v2/session-groups?archived=false&q=...` | 目录元数据、完整计数与 `scope_seq`，不返回会话内容 |
| `GET /local/v2/sessions/browse?limit=8&folder_id=...&cursor=...` | 最多 8 条摘要、`has_more` 和 `next_cursor`；支持 `archived`、`q`、`exclude_id`、`side_source_id` |
| `GET /local/v2/hello?scoped=true` | 返回工作区当前 `scope_seq`，实时订阅从该位置开始，不重放完整目录 |

`folder_id` 缺省查询所有目录，空字符串查询未归入目录的会话。游标绑定账号工作区对应的 scope、
守护进程数据库代际和筛选条件；列表按置顶、更新时间降序、ID 升序读取。实时变化只刷新已请求的页面，
后续页沿用刷新后的游标链，避免页边界变化漏项。加载失败保留已获取的行，并提供重试。

这部分需要同步更新电脑端守护进程。原有 `/sessions`、未带 `scoped=true` 的 `/hello` 和 WebSocket
消息结构保持兼容；新手机端连接不支持分页的旧守护进程时显示升级提示，不退回全量拉取。

## 引用选择细节

会话、任务结果行不显示左侧图标或图标底色；智能体展示现有头像，未设置头像时沿用默认头像。右侧保留选中标记。最多选择 5 项，达到上限仍可取消选择。
完成后返回输入框，显示可移除的引用标签；取消或下滑关闭不会提交选择。
键盘出现时，选中数量保持在键盘上方。大字号时标题另起一行，内容仍可滚动。

## 发送、排队和重试

移动端自行管理结构化引用草稿；与文本、已上传附件的指针一起按账号、工作区、电脑和会话隔离。
发送前重新检查引用和智能体调用权限。不可用引用会阻止发送并保留草稿。
会话与任务仅提供上下文；选择智能体本身不会启动运行。

提交时固定请求 ID、上下文快照、引用、智能体和附件。排队与失败重试沿用相同内容，
服务端确认后只清理对应提交。历史消息与队列都能还原引用标签。

沿用现有命令字段：`text`、`prompt`、`references: { kind, id }[]`、`agents`、附件和请求 ID。
引用类型保持 `session | issue | agent`，数量上限遵循现有守护进程协议。

## 实现入口

- [引用与上下文](../../data/ai-sessions/references.ts)：标识、序列化、权限验证与摘录。
- [分页查询](../../data/ai-sessions/reference-queries.ts)：三个分类的独立查询。
- [会话列表查询](../../data/ai-sessions/list-queries.ts)与[列表状态](../../data/ai-sessions/use-session-list.ts)：目录计数、按需分页和实时刷新。
- [守护进程接口](../../../../server/internal/daemon/localhost/v2_browse.go)与[数据库分页](../../../../server/internal/localconversation/store/browse.go)。
- [草稿](../../data/ai-sessions/drafts.ts)与[提交](../../data/ai-sessions/submission.ts)：持久化、确认及重试。
- [原生选择页](../../components/ai-sessions/reference-picker.tsx)与[输入框](../../components/ai-sessions/session-content.tsx)。

交互保留原生 TextInput，引用标签放在输入框上方。完整的引用状态贯穿选择、发送、排队、重试和历史展示；
此方案无需引入富文本编辑器。UI 使用移动端现有 RNR 控件、主题 token 和 Expo Router formSheet。
没有引入 Web UI 依赖。

## 验证

在仓库根目录运行：

```bash
pnpm --filter @multica/mobile typecheck
pnpm --filter @multica/mobile lint
pnpm --filter @multica/mobile test
pnpm --filter @multica/core typecheck
pnpm --filter @multica/core exec vitest run local-conversation/v2.test.ts
(cd server && go test ./internal/localconversation/... ./internal/daemon/localhost -count=1)
```

原生开发构建沿用 [移动端 README](../../README.md) 中的 iOS 启动流程。
