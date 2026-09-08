# IM 审核推送与决策回执

2026-09-03

## 目标

需要人拍板的时刻,把收件箱里的那一条主动推到用户绑定的 IM 私信;用户**直接回复那条推送**即可完成决策,不必回到应用。

## 范围

**做**:把一个很窄的收件箱事件白名单转发到 IM 私信,并让该推送可被回复——回复注入到对应 issue,由既有的评论触发器唤醒 agent；当回复是精确的「审核通过」或「确认审核」时，同时以回复成员身份把 `in_review` 类别的任务流转到 `done`。

**不做**(已确认):

- 交互式卡片 / 按钮。WeCom 的 `Send` 已经明确拒绝通用外发路径(见「约束」),统一卡片抽象会和它对着干。
- 硬编码的 `/approve` 命令。理由见「为什么不做命令」。
- 新的 `decision_request` 实体。收件箱已经是「待人处理的事」的持久化表示。
- 群聊推送。只发私信。
- 超时升级 / 催办。
- 聊天会话发起的工作(Case 1)。聊天本身是交互式的,今天已经能用。
- IM 专属的通知偏好开关。现有静音已经覆盖,见「收件人」。

## 现状:今天怎么做的

用户在与 agent 的 IM 会话里回复审核结论,回复会作为普通 issue 评论唤醒 agent。仅靠这一步不能完成审核:agent 遵守「`done` 由人类确认」的约束,会记录回复但保持 `in_review`。因此回执需要区分两类语义:精确的「审核通过」或「确认审核」是明确的人类终态决定,由平台流转到 `done`;带附加要求的回复和修改意见只写评论并唤醒 agent,由它结合任务上下文处理后续工作。

三点事实,决定了设计形状:

1. **agent 能写 `done`,代码里没有拦截。** `cmd/multica/cmd_issue.go:429` 的 `validateIssueStatus` 只校验 key 形状(1-32 位小写字母/数字/下划线),不校验调用者身份,没有 `done` 特例。SKILL.md 的 `done` 条目那句 "you do not also need to flip it manually" 本身就预设了 agent 平时就是手动 flip 的。

2. **`multica-working-on-issues/SKILL.md:236` 的 "Questions, discussion, or acknowledgements never move the status" 不适用于此。** 那句话在 `in_progress` / `in_review` 那一段里,讲的是 agent **汇报自己工作状态**的规则——别因为有人问了个问题就改状态。「确认审核」是人下达的指令,不是 acknowledgement。

3. **`done` 之后的级联是服务端做的,不是 agent。** `internal/handler/issue_child_done.go` 的 `notifyParentOfChildDone`:child 进入 terminal 状态 → `stageBarrierClosed` 判定最低未完成 stage 是否全部终结 → `dispatchParentAssigneeTrigger` 唤醒父 issue 的 assignee。注释明写这是 MUL-2538 用平台驱动替换旧 agent-prompt 规则的结果("replaces the agent-prompt rule that caused self-mention loops in PR #2918")。

   所以任何一次 `done` 写入都白拿这套级联,不需要在本设计里重复。

## 为什么不做 `/approve` 命令

不增加另一套斜杠命令语法。推送已经明确提示用户回复「审核通过」,这两个自然语言短语就是审核动作入口。状态流转后仍保留原评论并唤醒 agent,所以不会跳过收尾工作。

`/approve` 可以将来作为「不想打字就一键通过」的快捷方式叠加,但它不是底座。

## 为什么不需要「agent 运行中阻塞等人」原语

`server/internal/daemon/daemon.go` 里 `Status: "blocked"` 只有三个来源:`classifyPoisonedOutput`、`timeout`、`idle_watchdog`——全是失败分类,全部经由 `FailTask`。没有一个是「agent 主动提问」。`reportTaskResult` 的注释写死了这个立场:

> Fail closed: only an explicit 'completed' status is reported as success. Anything else — 'blocked', 'cancelled', or any future status we forget to enumerate — must go through FailTask.

这些 blocked 以 `pkg/taskfailure` 的 `ReasonAgentBlocked` 落到 `task_failed` 收件箱行(mobile 文案:"Waiting on human input")。「agent 卡住需要人」已经有载体,推送它即可。

## 一、推送半程

### 推什么

「白名单」指新包里一段**硬编码的 Go 判定**(`map[string]bool` 量级),不是配置项、不是表、不是后台开关;改它就是改代码。同形态的先例在 `cmd/server/notification_listeners.go` 里已有三份:`parentBubbleNotifTypes`(:75)、`delegatedAlwaysNotifTypes`(:83)、`delegatedStatusNotify`(:100),都是 map 加一段解释产品判断的注释。

分工:白名单答「这类事值不值得打扰人」——产品判断,全局一致,代码里定;用户「我个人想不想被打扰」由既有的 `notification_preference` 回答(见「收件人」)。不引入第二个配置面。

不新建收件箱类型。`cmd/server/notification_listeners.go:756-763` 已经在每次状态变更时创建 `status_changed` 行,包括进入 `in_review`。同文件 `:89-104` 的 `delegatedStatusNotify` 注释已经替我们论证过优先级:

> in_review is the important one: in Multica's agent flow an agent parks completed work in in_review, so that is the dominant "this needs you now" transition, not done.

推送白名单:

| 收件箱 type | 条件 | 可回复决策 |
| --- | --- | --- |
| `status_changed` | `issuestatus.Effective(to) == "in_review"` | 是 |
| `status_changed` | `issuestatus.Effective(to) == "blocked"` | 是 |
| `task_failed` | 无(含 `agent_blocked` reason) | 是 |
| `quick_create_failed` / quick-create 未确认 | 无 | 否,无关联 issue |

其余一律不推。

**不用 severity 做闸门。** `status_changed` 的 severity 是 `"info"`,和路过噪音同级;「需要你了」的语义在 type + 目标状态类别里,不在 severity 里。

状态判定走 `issuestatus.Effective`,自定义状态按类别继承——与 `deliverToSubscriber` 的既有做法一致(该处注释:allowlist "keys off behavior rather than a [literal key]")。

### 收件人

跟着收件箱走,不另立路由:

- 只处理 `recipient_type == "member"`,收件人即行上的 `recipient_id`。订阅者扇出、`delegated` 分级、父 issue 冒泡的取舍已在上游 `notifyIssueSubscribers` 做完,推送层不复制。
- 静音天然继承:`notifTypeToGroup` + `isNotifMuted` 在**创建收件箱行之前**生效。被静音 → 没有行 → 没有 `EventInboxNew` → 没有推送。这是「IM 偏好开关先不做」成立的原因。

**收件人可能不止一个。** `notifyIssueSubscribers`(`notification_listeners.go:345-448`)对每个成员订阅者各建一行 inbox、各发一个 `EventInboxNew`,订阅原因有七种(`:108-111`:creator / assignee / commenter / mentioned / manual / autopilot / delegated)。动作发起人被排除(`:394`),agent 订阅者被跳过,delegated 档再经 `deliverToSubscriber` 收窄。实际常见是一个人——提需求的那位(creator,或 agent 代建时的 `delegated`)——但评论过、被 @ 过的人同样在列。

后果:同一条 `in_review` 可能推给两个人,两人各自回复「确认审核」,产生两条评论、两次触发。**不需要为此新做防护**:`handler/comment.go` 的 `mergeCommentIntoPendingTask` 会把撞上 pending task 的第二条评论并入,而不是起第二个 run。实现时不要重复造这层。

目的地:`FindChannelBindingForMember`。一个成员在一个工作区可能绑定多个 channel_type;取**最近绑定的一个**,只发一处,与该查询自身的 tiebreak("the most-recently-bound wins")一致。

未绑定 → 无操作,用户照常在应用内收件箱看到。这是 WeCom 现有路径已确立的降级语义。

### 分层:共享层只做决策,发送归适配器

这条线是被代码逼出来的。`wecom/wecom_channel.go:566`:

```go
// Not used. Outbound for wecom goes through OutboundReplier / Outbound
// (EventChatDone + EventInboxNew), which know the message's real
// Source.ChatType and address the correct chat. ...
// Return not-supported rather than keep a second, heuristic outbound path alive.
return channel.SendResult{}, ErrSendNotSupported
```

任何调用 `channel.Channel.Send` 的共享推送层在 WeCom 上必然是死路。所以:

**共享层**(新包,建议 `server/internal/integrations/channel/notify`)负责:订阅 `protocol.EventInboxNew`、白名单过滤、`recipient_type` 闸门、绑定查找、文案渲染、降级语义、独立指标、**登记回执反查行**。

**适配器**负责发送,实现:

```go
DeliverDM(ctx, binding, text) (DeliverResult, error)
```

`DeliverResult` 三态,且必须带回平台消息 id:

- `Delivered{MessageID}` —— 已送达。**MessageID 是回执反查的钥匙**;有它才写反查行、才可回复。**允许为空**:WeCom 发得出去但拿不到 id(见「适配器能力矩阵」),此时只推不登记,推送不可回复。
- `HandedOff` —— 本副本发不了,已把消息转手给持有连接的另一个副本;发不发得出去、平台消息 id 是多少,本副本都不知道。

  这个状态只为 WeCom 存在。`relay_outbound.go` 开头:"WeCom is the one channel with no outbound REST path: every write goes over the aibot WebSocket, and the WS lease means exactly one replica holds it." 而 `EventInboxNew` 在负载均衡挑中的副本上触发,两者常常不是同一个;`outbound.go:420` 于是把帧扔进 Redis Stream relay 交给持有 socket 的副本。转手必须异步(同文档第 2 条:同步等网络往返会让一个不健康的 bot 卡住整个 shard),所以发起方拿不到任何回执。

  今天的代码这里 `return true`。单列出来是因为:算作 `Delivered` 会让指标撒谎;算作失败会误触发降级。
- `Unsupported` —— 该适配器不支持私信,静默无操作。

三条 WeCom 约束必须存活:chat_type 诚实(共享层不猜单聊/群聊,只交 binding 给适配器)、跨副本 relay 有位置(`HandedOff`)、指标单位隔离(推送计数不得进 agent 回复计数器——`outbound.go` 明写其单位是 "agent replies")。

**`HandedOff` 的回执缺口**(已定:接受降级):走 relay 的推送拿不到消息 id,那条推送**不可回复**——用户回复它会落回普通会话路径,靠深链回应用决策。不为此改造 `relayFrame`(目前单向 fire-and-forget,加回执要引入一条反向通道);WeCom 无活跃 socket 是异常态,不是常态,回带 id 列为后续项。

### 适配器能力矩阵:推送半程和回复半程的第一个适配器不是同一个

回复半程要求适配器两端都通:发送时拿得到平台消息 id,入站时带得回 `ReplyTo`。按代码实测:

| 适配器 | 发送返回消息 id | 入站 `ReplyTo` | 可回复 |
| --- | --- | --- | --- |
| Lark | ✅ `feishu_channel.go:85` `SendResult{MessageID: msgID}` | ✅ `feishu_channel.go:132` 逐条 `ReplyCtx{MessageID: lm.ParentID, RootID: lm.RootID}` | 是 |
| Telegram | ✅ `sender.go:114` | ✅ `inbound.go:89` 逐条 | 是 |
| Slack | ✅ `channel.go:76`(ts) | ⚠️ `inbound.go:175` 只有 thread 级 `threadTS`;私信推送自成一个 thread,故等价可用 | 是 |
| WeCom | ❌ | ❌ | **否,只推不收** |

WeCom 两端都不通,且都不是疏忽:

- `ws_sender.go:297-304` 的 `sendTextCtx` 写成 `_, err = s.request(...)`——ack body 直接丢弃,包内没有任何地方从发送 ack 里解 msgid。`wecomChannel.Send` 本身是 `ErrSendNotSupported` 桩。
- `ws_frame.go:74-94` 的 `aibotMsgCallback` 没有引用/回复字段。全仓只有 lark / telegram / slack 三处构造 `channel.ReplyCtx`,WeCom 一处也没有。所以 WeCom 用户回复推送时 `ReplyTo == nil`,「入站识别」第 1 条直接放行到原路径。

**结论:推送半程的第一个适配器是 WeCom,回复半程的第一个适配器是 Lark。**

- **WeCom 必须迁移**,即使它不可回复。它今天已有完整 inbox 推送(`handleInboxNew` / `tryDeliverInbox`);共享层落地时订阅与过滤上移,`tryDeliverInbox` 的函数体下沉为 WeCom 的 `DeliverDM`,返回 `Delivered{MessageID: ""}`。并列共存会让 WeCom 用户收到两条。**空 MessageID 不写反查行**——与 `HandedOff` 同样处理。
- **Lark 是回复半程唯一的落地验证路径**。全链路测试(推送 → 回复 → 评论 → 唤醒 agent)在 Lark 上做。

这也说明 `DeliverResult` 里 `Delivered` 的 MessageID 必须允许为空:"发出去了" 和 "可回复" 是两件事,WeCom 是前者不是后者。

**行为变更**(已定:统一走白名单):WeCom 今天只按 `recipient_type == "member"` 过滤,即转发**全部**收件箱条目。接入白名单后 WeCom 用户收到的推送会变少。这是有意的——全量转发是噪音源,且与 `delegatedStatusNotify` 已确立的取舍不一致。白名单不按 channel 可配。

## 二、回复半程

### 回执反查表

需要一张新表,不能复用 `channel_outbound_message`:migration 425 里它的 `binding_id` 和 `route_revision` 都是 `NOT NULL`,而这两个都是 chat_session 路由的概念,inbox 推送两个都没有。把它们改成可空并塞进一个 `issue_id`,会把一张身份明确的表("属于某条会话路由的外发消息")搅浑。

新表 `channel_push_message`:

| 列 | 说明 |
| --- | --- |
| `installation_id`, `channel_type`, `channel_message_id` | 平台消息坐标,唯一索引 |
| `workspace_id` | 作用域 |
| `recipient_user_id` | 被推送的那个人 |
| `issue_id` | 回复要注入到哪个 issue,可空(quick-create 失败无 issue) |
| `inbox_item_id` | 溯源 |
| `created_at` | 用于清理 |

按仓库规则:不加外键,索引用 `CREATE UNIQUE INDEX CONCURRENTLY` 且单独一个迁移文件,up/down 成对。

### 入站识别

`channel/message.go:109-117` 的 `InboundMessage.ReplyTo *ReplyCtx`(`MessageID` + `RootID`)已经是跨适配器归一化的「你回复了哪条」,不需要新的适配器能力。

路由判定,置于会话解析**之前**:

1. `ReplyTo == nil` → 走原路径,本设计不介入。
2. `ReplyTo.MessageID` 命中 `channel_push_message` → 进入注入路径。未命中则回退试 `RootID`。都不中 → 走原路径。
3. 发信人经 `channel_user_binding` 解析为 Multica user;必须等于该行的 `recipient_user_id`。不等 / 未绑定 / 非该工作区成员 / 无该 issue 访问权 → 回复「你没有权限」,不落任何写。
4. `issue_id` 为空(quick-create 失败类推送)→ 回复说明该条不可回复决策,附深链。

命中反查行时,该回复**不进 chat_session**。`/issue` 等既有命令在此路径下不解析——这条回复的语义已由被回复的那条推送确定。

### 注入 issue

以该成员身份在目标 issue 上创建评论,内容为回复原文。评论落地后既有的评论触发器接管:`issue_assignee` / `conversation_continuation`(`handler/comment.go:1495-1499`)在**任意状态**下唤醒 agent——`issue_trigger.go` 明写这一点:

> issue writes park on backlog while comments fire in any status.

于是 agent 带着 issue 上下文醒来,理解审核回复并决定后续动作。若去除首尾空白后的全文精确等于「审核通过」或「确认审核」,且任务当前有效状态类别为 `in_review`,回执处理器同时以回复成员身份流转到 `done`,发出标准 `issue:updated(status_changed=true)` 事件并执行 child-done 父任务级联。状态更新与评论在同一事务中提交,评论触发仍然保留。任何带附加内容的回复(例如「审核通过,继续下一阶段,父任务暂不 done」)都只写评论、唤醒 agent,不自动改变状态。

**接线点**:评论触发机制目前完全在 handler 层(`handler/comment.go` 约 1900 行,全是 `*Handler` 方法),没有 service 层入口,抽一个不在本次范围。复用的办法是既有的口子:`cmd/server/router.go:521` 已经是 `Lifecycle: h`——Handler 自己实现引擎接口。照此加一个窄接口(如 `IssuePushReplyPoster`,单方法),由 `*Handler` 实现、在同一处注入,引擎调它。共享层不碰触发逻辑。

不新增 `ChannelChatLifecycle` 的方法:那个接口的三个方法都是 chat 会话生命周期,塞入 issue 评论会撑坏它的语义。

### 无 assignee 的情况

目标 issue 没有 agent assignee 时,评论落地但不触发任何 run。与应用内评论行为一致,不做特殊处理。

## 测试

- 白名单矩阵:表驱动,放在过滤器旁的 `_test.go`,不经收件箱扇出跑。
- 自定义状态经 `issuestatus.Effective` 归入 `in_review` 类别时同样推送。
- 任务进入 `blocked`（包括自定义 blocked 类别）时推送明确的受阻文案，并允许回复所需信息或处理意见。
- `DeliverResult` 三态各自的指标与降级行为,用 fake 适配器覆盖;`HandedOff` 单独断言不计入送达、且**不写**反查行。
- 反查命中/未命中/回退 `RootID`/表中无 `issue_id` 四种入站分支。
- 授权矩阵:发信人非 `recipient_user_id`、未绑定、已绑定非成员、无 issue 访问 —— 全部只回文案不落写。
- 命中反查行时不创建 chat_session、不解析 `/issue`。
- 精确「审核通过」/「确认审核」完整落为评论,并把 `in_review` 类别任务流转到 `done`;事件 actor 是回复成员,同时覆盖 activity、inbox 与 child-done 父任务级联。
- 带附加内容的审核回复和修改意见完整落为评论但不自动修改状态;用「审核通过,继续下一阶段,父任务暂不 done」覆盖非终态审核语义。
- 评论落地后触发器按既有路径 fire(与 `handler/comment.go` 现有用例同层断言,不重跑其矩阵)。
- WeCom 迁移后不重复推送;WeCom 的 `Delivered` 带空 MessageID 时**不写**反查行。
- Lark 全链路:推送 → 带 `ReplyTo` 的回复 → 命中反查 → 落评论 → 触发器 fire。这是回复半程唯一有适配器支撑的端到端路径。
- DB 用例走 `internal/testutil` 的 `dbfx.Issue` / `dbfx.Insert` 与 `testutil.Call`,不手写 INSERT/cleanup 对。

## 风险

- 共享层若误用 `channel.Channel.Send`,WeCom 直接 `ErrSendNotSupported`。接口上就不暴露该 seam。
- **WeCom 推送一律不可回复**(两端都不通,见「适配器能力矩阵」)。WeCom 用户只能点深链回应用决策。若要打通,需要先确认 aibot 的 `aibot_send_msg` ack 是否回带 msgid(`sendTextCtx` 目前丢弃 ack body),以及 WeCom 是否有引用回复的入站字段——两个都是未知,不在本次范围。
- `HandedOff` 推送不可回复,用户需点深链回应用(已接受的降级)。
- WeCom 用户的推送量会下降(已接受,见上)。
- 决策经 LLM 解读而非确定性命令。这是刻意的:今天已在用,且 agent 的收尾工作正是命令给不了的。
