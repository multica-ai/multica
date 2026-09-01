# 跨员工 Agent 协作演示 · Squad Collaboration Demo

> 场景级演示：一个需求进入 squad，由 leader agent 编排，分派给架构师 / 工程师 / 测试 / 文档等专项 agent 协作完成。5 分钟内从「创建 squad」看到「协作结果」。
>
> 本文档面向产品演示 / 客户 demo / 新同学上手。每一条命令均可直接复制执行。

---

## 1. 这是什么

> **本 demo 的范围**：演示「单 workspace 内多 agent 协作」（leader 编排 + 成员专项分工 + 可观测交接）。WS-567 中更进一步的「跨员工 remote agent endpoint + 消息协议」是独立的 2 周 P0 MVP，目前 blocked 未实装，不在本 demo 范围（详见 §10 局限）。

Multica 的 **Squad（小队）** 是一类特殊的 issue assignee。把 issue 分派给 squad，系统不会让 squad 自己跑，而是**唤醒 squad 的 leader agent**。leader 拿到任务后只做一件事：**编排**——读需求、按成员技能挑人、在 issue 评论里用 `@mention` 把活派下去，然后记录自己的评估。

这就构成了一条**多 agent 协作链**：

```
issue 指派给 squad
   └─> leader agent 被唤醒（注入「Squad Operating Protocol + 成员花名册 + squad 指令」）
         └─> leader 在评论里 @mention 架构师  ──> 架构师出方案（评论回传）
               └─> leader @mention 工程师     ──> 工程师编码（评论回传）
                     └─> leader @mention 测试 ──> 测试验证（评论回传）
                           └─> leader 调 `multica squad activity` 记录评估
```

相比单个全能 agent 串行做完所有事，squad 的增益是：**专业化分工 + 可观测的编排过程 + 并行能力**。

---

## 2. 演示价值（对照 WS-567）

| 价值点 | 单 agent | Squad |
|---|---|---|
| **分工** | 一个 agent 干所有事，无专业边界 | 架构师出方案、工程师编码、测试验证、文档沉淀，各司其职 |
| **协作** | 无交接，全程黑盒 | leader 在 issue 评论里 `@mention` 派活，交接可追溯 |
| **价值** | 串行、无编排视角 | 专项化工作 + leader 编排，每一步在时间线上可见 |

> **关于「跨员工」**：本演示覆盖 WS-567 中「单 workspace 内多 agent（分属不同职能）协作」这一已通电部分。WS-567 里更进一步的「跨员工 remote agent endpoint + agent 间消息协议」是独立的 2 周 P0 MVP，目前 blocked、未实装，不在本演示范围（见 §10 局限）。

---

## 3. 演示场景：代码交付小队

**用户故事**：

> 作为 workspace owner，我提交一个产品需求（如「给用户菜单加一个深色模式开关」），不指定具体执行者，而是分派给「代码交付小队」。小队的 leader（资深工程师）评估后，把方案设计派给架构师、把编码派给工程师、把验证派给测试、把变更说明派给文档 agent。我在 issue 时间线和 squad 成员页上实时看到谁在干什么。

**角色配置**（demo squad「代码交付小队」）：

| 成员 | 角色 | 职责 |
|---|---|---|
| 资深工程师 | leader | 编排：评估需求、按技能派活、记录评估 |
| 量化架构师agent | 架构 | 出技术方案 / 接口设计 |
| 高级工程师 | 编码 | 实现 |
| 测试agent | 测试 | 端到端验证 |
| paper agent | 文档 | 变更说明 / changelog |

---

## 4. 前置条件

1. **workspace 内存在上述 5 个 agent**（名字匹配即可，见 §5 一键导入）。本工作区已自带这套阵容。
2. **相关 agent 的 runtime 已绑定且在线**。成员状态来自 runtime 心跳 + 活跃任务（`server/internal/handler/squad.go::deriveSquadMemberStatus`）：runtime 未绑定的成员会显示 `offline`，演示效果差。在 **Agents** 页确认 leader 与至少 2 个成员状态不是 offline。
3. **已安装 `multica` CLI** 并 `multica auth login` 到目标 workspace。

---

## 5. 一键导入 demo 数据

```bash
bash docs/demo/seed-squad-demo.sh
```

脚本幂等：已存在同名 squad 则复用并补齐成员与指令，不会重复创建。它做三件事：

1. 创建（或复用）squad **代码交付小队**，leader = 资深工程师；
2. 按 §3 表格加入 4 个成员（带角色）；
3. 写入 squad 指令（leader 编排手册，会被注入到 leader 的 prompt，见 `server/internal/handler/squad_briefing.go`）。

脚本只创建数据，**不会触发任何 agent 运行**（没有 issue 被分派）。导入完成后拿到的 squad id 用于后续步骤。

校验导入结果：

```bash
multica squad list --output json | python3 -c "import json,sys;[print(s['name'],s['id']) for s in json.load(sys.stdin)]"
multica squad member list <squad-id> --output json
```

---

## 6. 5 分钟演示步骤

> 计时从「创建并分派 issue」开始算（demo 数据已在 §5 导入）。实际 leader / 成员 agent 真正跑完各自的活可能超过 5 分钟，但**协作链的启动与可观测信号**在 5 分钟内必定出现。

### Step 1 ｜ 创建并分派 issue（~30 秒）

```bash
multica issue create \
  --title "Demo: 给用户菜单增加深色模式开关" \
  --description "演示用：由「代码交付小队」协作完成。需要架构方案、编码实现、测试验证、变更文档。" \
  --assignee "代码交付小队"
```

`--assignee` 接受 squad 名（模糊匹配，等价于 `--assignee-id <squad-id>`）。这一刻系统把 issue 的 `assignee_type` 设为 `squad`，并向 squad 的 leader 排一个 `is_leader_task=true` 的任务（`server/internal/service/issue.go::maybeEnqueueOnAssign`）。

### Step 2 ｜ 观察 leader 接管（~30–60 秒）

打开 issue 页（Web/Desktop）或 `multica issue comment list <issue-id> --recent 5`：

- leader（资深工程师）被唤醒，拿到注入的 **Squad Operating Protocol + 成员花名册**（每行带可粘贴的 `[@Name](mention://agent/<UUID>)` 链接 + 该 agent 的技能）。
- leader 发一条**简短的派活评论**，`@mention` 架构师并交代要做什么。
- leader 调 `multica squad activity <issue-id> action --reason "派给架构师出方案"`，在 issue 时间线记一条 `squad_leader_evaluated`。

> Operating Protocol 要求 leader「只编排、不亲自干活」「派完就停、被再次触发再评估」（`server/internal/handler/squad_briefing.go::squadOperatingProtocol`）。演示时讲清这条，能让观众理解为什么 leader 不直接写代码。

### Step 3 ｜ 观察成员协作（~2–3 分钟）

随着 leader 派活，被 `@mention` 的成员依次被唤醒：

- 架构师发方案评论 → leader 再次被触发 → `@mention` 工程师编码；
- 工程师编码回传 → leader `@mention` 测试验证；
- 测试验证回传 → leader `@mention` 文档沉淀。

每一轮 leader 都会调一次 `multica squad activity`。所有交接都在**同一 issue 的评论线程**里，可线性回放。

### Step 4 ｜ 看协作全貌

| 看哪里 | 看到什么 |
|---|---|
| **Squad 详情页** `/squads/<id>` | 成员行右侧的状态 pill 翻成 `working`（绿点），下方出现 active issue chip（`WS-xxx`）。空闲成员是 `idle`，离线是 `offline`。 |
| **Issue 活动** `multica issue comment list <issue-id>` | leader 的派活评论、成员的结果评论、带 `@mention` 的交接。 |
| **Issue 活动时间线**（Web issue 页） | 每轮 `squad_leader_evaluated` 条目（`action`/`no_action`/`failed`）。 |
| **CLI** | `multica agent tasks <leader-id>` 可看 leader 当前编排任务。 |

---

## 7. 预期输出（30 秒可感知信号）

观众 30 秒内应能说出这三件事，否则演示没到位：

1. **「leader 在派活，不是自己干」**——issue 评论里能看到 leader 的 `@mention` 派活评论。
2. **「不同 agent 在各自干活」**——squad 成员页上 ≥2 个成员状态 pill 是 `working` 且各挂一条 active issue。
3. **「过程可追溯」**——issue 时间线里有 `squad_leader_evaluated` 条目，评论线程能线性回放交接。

---

## 8. 单 agent vs Squad 价值对照（可现场对比）

```bash
# 对照组：同样需求分派给单个 agent（资深工程师）
multica issue create --title "对照: 深色模式开关（单 agent）" --assignee "资深工程师"
```

对照观察：单 agent 串行做所有事，无交接评论、无成员状态联动、无 leader 评估记录；squad 则有明确分工与可观测编排。

---

## 9. 清理

```bash
# 归档 demo squad（成员的 issue assignee 会自动转给 leader，不会丢失）
multica squad delete <squad-id>

# 归档 demo issue
multica issue status <issue-id> cancelled
```

---

## 10. 局限与后续

- **无专门的「Flow 可视化」图**。当前协作可视化形式是：squad 成员状态 pill + active issue chip + issue 活动时间线 + 评论线程。没有一张汇总的「squad 协作流程图」。`stageBarrierClosed`（`server/internal/handler/issue_child_done.go`）是父子 issue 的 stage 闭环，不是 squad 可视化。
- **无 agent 间消息协议**。agent 之间通过 issue 评论 + `@mention` 通信（`server/internal/util/mention.go`、`computeCommentAgentTriggers`），没有独立 inbox / 消息表。
- **跨员工 remote agent**（WS-567 MVP）：让员工 A 的 agent 与员工 B 的 agent 在同一 squad 协作、远程 endpoint 注册、跨员工消息审计——目前 blocked、未实装，是下一步重点。

---

## 11. 技术参考（源码位置）

| 能力 | 位置 |
|---|---|
| issue 分派给 squad → 唤醒 leader | `server/internal/service/issue.go`（`maybeEnqueueOnAssign` / `shouldEnqueueSquadLeaderOnAssign` / `enqueueSquadLeaderTask`） |
| leader 任务打标 | `server/internal/service/task.go`（`EnqueueTaskForSquadLeader`，`IsLeaderTask`/`SquadID`） |
| leader 拿任务时注入 briefing | `server/internal/handler/daemon.go` + `server/internal/handler/squad_briefing.go`（`buildSquadLeaderBriefing` = Protocol + Roster + Instructions） |
| `multica squad activity` 落库 | `server/internal/handler/squad.go`（`RecordSquadLeaderEvaluation`，写统一 `activity_log`） |
| 成员状态推导 | `server/internal/handler/squad.go`（`deriveSquadMemberStatus`） |
| 前端 squad 页 | `packages/views/squads/components/squad-detail-page.tsx` |
| 前端活动渲染 | `packages/views/issues/components/issue-detail.tsx`（`squad_leader_evaluated`） |
| demo 数据导入脚本 | `docs/demo/seed-squad-demo.sh` |
