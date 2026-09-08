# 11 — LangGraph：何时引入、如何对接

## 不必一开始就上

| 条件 | 建议 |
|------|------|
| 队列 <10 `agent-safe` ticket/天 | **GHA + CI 足够**（见 `.delivery/README.md` FAQ） |
| 整任务重试可接受 | 暂不需要 LangGraph |
| 需要「测试 A 失败只修模块 A」 | **上 LangGraph** |
| 需要跨 repo 编排、长事务 checkpoint | **上 LangGraph** |

---

## LangGraph 在公司架构中的位置

```text
LangGraph（L2 编排内核）
    │ 节点：load_spec → dispatch_coding → wait_ci → branch_on_exit_code
    ▼
Multica webhook / cursor API / shell
    ▼
Worker 执行
    ▼
回调：CI status → LangGraph 条件边
```

**Multica 仍管：** 看板、并发、历史、人工指派。  
**LangGraph 管：** 细粒度状态机、重试策略、HITL。

---

## 最小图（伪代码结构）

```python
# 示意 — 非可运行完整代码，仅表达硬分支
from langgraph.graph import StateGraph

def node_dispatch(state):
    # multica CLI 或 POST autopilot webhook
    subprocess.run(["bash", "scripts/agent-delivery/dispatch-cursor-agent-cli.sh", issue], check=True)
    return state

def node_wait_ci(state):
    # gh pr checks --watch 或 GitHub API
    code = subprocess.run(["gh", "pr", "checks", pr, "--watch"], ...).returncode
    state["ci_exit"] = code
    return state

def branch_ci(state):
    if state["ci_exit"] == 0:
        return "done"
    if state["retry_count"] < 3:
        return "fix"
    return "hitl"

graph = StateGraph(...)
graph.add_node("dispatch", node_dispatch)
graph.add_node("wait_ci", node_wait_ci)
graph.add_conditional_edges("wait_ci", branch_ci, {"fix": "dispatch", "done": END, "hitl": "interrupt"})
```

**关键：** `branch_ci` 是 **Python 函数**，不是 LLM。

---

## 与 Multica 对接方式

| 方式 | 说明 |
|------|------|
| **Webhook** | LangGraph → `POST` Multica Autopilot trigger URL |
| **CLI** | `multica issue create` / assign agent |
| **GitHub 间接** | LangGraph 只操作 Issue label，GHA dispatch 消费队列 |

推荐：**LangGraph 写 Issue 状态 + 调 dispatch 脚本**；Multica 展示 task 进度。

---

## Checkpoint 与 HITL

- `interrupt()` 前状态：CI 红且 retry 耗尽、合规模糊、breaking API。
- CEO 在 Multica / GitHub 评论答复 → resume graph。

---

## 部署

- LangGraph Server 或自托管 Python worker（cron / queue consumer）。
- 状态持久化：Postgres 或 SQLite checkpoint（LangGraph 内置）。

---

## 相关文档

- [04-architecture.md](./04-architecture.md)  
- [05-stack-selection.md](./05-stack-selection.md)  
