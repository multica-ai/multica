# Issue 详情页 — Workflow 执行全景图实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Issue 详情页中，当 Issue 关联 Workflow 时，将旧的 `WorkflowDagViewer` 替换为 Workflow 执行全景图（阶段泳道 + 运行时节点卡片 + SVG 连线）。

**Architecture:** 后端新增 `include_workflow_origin` 查询参数过滤子 Issue；前端创建 `ExecutionPanoramaPage` 系列组件（复用 Panorama 的 `StageLane`/`PanoramaSvgOverlay`/`StageTransitionBar`），通过 `IssueDetail` 中 `assignee_type === "workflow"` 的条件分支切换新旧视图。

**Tech Stack:** Go (Chi router + sqlc), TypeScript (React + TanStack Query), Vitest + jsdom

## Global Constraints

- API 响应必须经过 `parseWithFallback(zodSchema, fallback)` 解析，绝不裸 `as` 转型
- 共享组件放 `packages/views/`，不引入 `next/*` 或 `react-router-dom`
- 新 UI 字符串同时添加中英文 i18n key
- Go handler UUID 参数用 `parseUUIDOrBadRequest` 校验
- 测试跟随代码所在包：Go 后端测试在 `server/`，前端测试在 `packages/views/`

---

### Task 1: Go 后端 — SQL 查询添加 `include_workflow_origin` 过滤

**Files:**
- Modify: `server/pkg/db/queries/issue.sql:11-12, 155-156, 200`

**Interfaces:**
- Produces: `ListIssues` / `ListOpenIssues` / `CountIssues` 新增 `sqlc.narg('exclude_workflow_origin')::bool` 参数

- [ ] **Step 1: 修改 `ListIssues` 查询添加过滤条件**

在 `server/pkg/db/queries/issue.sql` 中，找到 `ListIssues`（第 1 行附近），在 `WHERE i.workspace_id = $1` 之后添加一行：

```sql
  AND (sqlc.narg('exclude_workflow_origin')::bool IS NULL
       OR i.origin_type IS DISTINCT FROM 'workflow')
```

插入位置紧接第 12 行 `WHERE i.workspace_id = $1` 之后。

- [ ] **Step 2: 修改 `ListOpenIssues` 查询添加相同过滤**

在 `ListOpenIssues`（第 148 行附近）的 `WHERE i.workspace_id = $1` 之后，第 156 行 `i.status NOT IN ('done', 'cancelled')` 之后添加同一 SQL 片段：

```sql
  AND (sqlc.narg('exclude_workflow_origin')::bool IS NULL
       OR i.origin_type IS DISTINCT FROM 'workflow')
```

- [ ] **Step 3: 修改 `CountIssues` 查询添加相同过滤**

在 `CountIssues`（第 197 行附近）的 `WHERE i.workspace_id = $1` 之后添加：

```sql
  AND (sqlc.narg('exclude_workflow_origin')::bool IS NULL
       OR i.origin_type IS DISTINCT FROM 'workflow')
```

- [ ] **Step 4: 重新生成 sqlc**

```bash
make sqlc
```

Expected: 无错误生成 `server/pkg/db/generated/issue.sql.go`，`ListIssuesParams` / `ListOpenIssuesParams` / `CountIssuesParams` 结构体新增 `ExcludeWorkflowOrigin pgtype.Bool` 字段。

- [ ] **Step 5: 提交**

```bash
git add server/pkg/db/queries/issue.sql server/pkg/db/generated/issue.sql.go
git commit -m "feat(server): add exclude_workflow_origin filter to issue list queries"
```

---

### Task 2: Go 后端 — Handler 解析 `include_workflow_origin` 参数

**Files:**
- Modify: `server/internal/handler/issue.go:712-866`

**Interfaces:**
- Consumes: `ListIssuesParams.ExcludeWorkflowOrigin`, `ListOpenIssuesParams.ExcludeWorkflowOrigin`, `CountIssuesParams.ExcludeWorkflowOrigin`
- Produces: `GET /api/issues?include_workflow_origin=false` 默认排除 `origin_type='workflow'` 的子 Issue

- [ ] **Step 1: 在 `ListIssues` handler 中解析参数并传递**

在 `server/internal/handler/issue.go` 的 `ListIssues` 函数中，在 `metadataFilter` 解析之后（约第 780 行），添加：

```go
// include_workflow_origin defaults to false — auto-generated child issues
// (origin_type = 'workflow') are excluded from the list by default.
excludeWorkflowOrigin := pgtype.Bool{Bool: true, Valid: true}
if r.URL.Query().Get("include_workflow_origin") == "true" {
    excludeWorkflowOrigin = pgtype.Bool{Bool: false, Valid: true}
}
```

- [ ] **Step 2: 传入 `open_only` 路径的查询参数**

修改约第 785 行 `ListOpenIssuesParams` 字面量，添加：

```go
ExcludeWorkflowOrigin: excludeWorkflowOrigin,
```

- [ ] **Step 3: 传入标准分页路径的查询参数**

修改约第 849 行 `ListIssuesParams` 字面量，添加：

```go
ExcludeWorkflowOrigin: excludeWorkflowOrigin,
```

- [ ] **Step 4: 传入 `CountIssues` 查询参数**

修改约第 869 行 `CountIssuesParams` 字面量，添加相同字段：

```go
ExcludeWorkflowOrigin: excludeWorkflowOrigin,
```

- [ ] **Step 5: 编译验证**

```bash
cd server && go build ./...
```

Expected: 编译通过，无报错。

- [ ] **Step 6: 提交**

```bash
git add server/internal/handler/issue.go
git commit -m "feat(server): default-exclude workflow-origin child issues from issue list"
```

---

### Task 3: Go 后端 — Issue 列表过滤测试

**Files:**
- Create: `server/internal/handler/issue_list_filter_test.go`（如不存在则新建；如已有 issue 测试文件则追加）

**Interfaces:**
- Consumes: `GET /api/issues` 端点

- [ ] **Step 1: 写测试 — 默认排除 workflow 子 Issue**

```go
func TestListIssues_ExcludesWorkflowOriginByDefault(t *testing.T) {
    // Setup: create a parent issue + a workflow-origin child issue
    // Call GET /api/issues
    // Assert: child issue with origin_type='workflow' is NOT in response
    // Assert: parent issue IS in response
}
```

- [ ] **Step 2: 写测试 — `include_workflow_origin=true` 时包含子 Issue**

```go
func TestListIssues_IncludeWorkflowOrigin(t *testing.T) {
    // Setup: same as above
    // Call GET /api/issues?include_workflow_origin=true
    // Assert: BOTH issues appear in response
}
```

- [ ] **Step 3: 运行测试**

```bash
cd server && go test ./internal/handler/ -run TestListIssues_ -v
```

Expected: 两个测试 PASS。

- [ ] **Step 4: 提交**

```bash
git add server/internal/handler/
git commit -m "test(server): verify issue list excludes workflow-origin children by default"
```

---

### Task 4: 前端 — Zustand store 添加 `include_workflow_origin` 过滤参数

**Files:**
- Modify: `packages/core/api/client.ts:491-513`（`listIssues` 方法）
- Modify: `packages/core/types/api.ts`（`ListIssuesParams` 类型）

**Interfaces:**
- Produces: `ListIssuesParams.include_workflow_origin?: boolean`

- [ ] **Step 1: 给 `ListIssuesParams` 类型添加字段**

在 `packages/core/types/api.ts` 的 `ListIssuesParams` 接口中添加：

```typescript
export interface ListIssuesParams {
  // ... existing fields ...
  include_workflow_origin?: boolean;  // defaults to false on server
}
```

- [ ] **Step 2: 在 API client 中传递参数**

在 `packages/core/api/client.ts` 的 `listIssues` 方法中（约第 507 行 `scheduled` 之后），添加：

```typescript
if (params?.include_workflow_origin) search.set("include_workflow_origin", "true");
```

- [ ] **Step 3: 类型检查**

```bash
pnpm typecheck
```

Expected: 无类型错误。

- [ ] **Step 4: 提交**

```bash
git add packages/core/api/client.ts packages/core/types/api.ts
git commit -m "feat(core): add include_workflow_origin param to listIssues API"
```

---

### Task 5: 前端 — `NodeRunStatusIcon` 组件

**Files:**
- Create: `packages/views/issues/components/execution/node-run-status-icon.tsx`
- Create: `packages/views/issues/components/execution/node-run-status-icon.test.tsx`

**Interfaces:**
- Produces: `NodeRunStatusIcon({ status, className? }: { status: NodeRunStatus; className?: string }) => JSX.Element`
- 16 状态 → 11 视觉态，未知状态 fallback 为 `CircleOff`

- [ ] **Step 1: 写失败测试**

```tsx
// node-run-status-icon.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, it, expect } from "vitest";
import { NodeRunStatusIcon } from "./node-run-status-icon";

describe("NodeRunStatusIcon", () => {
  it("renders pending as empty circle", () => {
    render(<NodeRunStatusIcon status="pending" />);
    const icon = screen.getByTestId("status-icon-pending");
    expect(icon).toBeInTheDocument();
    expect(icon).toHaveClass("text-muted-foreground/40");
  });

  it("renders completed as green check", () => {
    render(<NodeRunStatusIcon status="completed" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-green-500");
  });

  it("renders working as spinning loader", () => {
    render(<NodeRunStatusIcon status="working" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("animate-spin");
    expect(icon).toHaveClass("text-blue-500");
  });

  it("renders critic_rework as orange RotateCcw", () => {
    render(<NodeRunStatusIcon status="critic_rework" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-orange-500");
  });

  it("renders blocked as red AlertCircle", () => {
    render(<NodeRunStatusIcon status="blocked" />);
    const icon = screen.getByTestId("status-icon");
    expect(icon).toHaveClass("text-red-500");
  });

  it("falls back on unknown status", () => {
    // @ts-expect-error testing invalid status
    render(<NodeRunStatusIcon status="unknown_future_status" />);
    expect(screen.getByTestId("status-icon-fallback")).toBeInTheDocument();
  });
});
```

运行: `pnpm --filter @multica/views exec vitest run issues/components/execution/node-run-status-icon.test.tsx`
Expected: FAIL — component does not exist yet.

- [ ] **Step 2: 实现组件**

```tsx
// node-run-status-icon.tsx
"use client";

import { AlertCircle, CheckCircle2, Circle, CircleOff, Clock, Loader2, MinusCircle, RotateCcw, UserCheck } from "lucide-react";
import type { NodeRunStatus } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

const STATUS_MAP: Record<NodeRunStatus, {
  icon: typeof Circle;
  className: string;
  spin?: boolean;
}> = {
  pending:             { icon: Circle,        className: "text-muted-foreground/40" },
  format_checking:     { icon: Loader2,       className: "text-blue-500", spin: true },
  format_ok:           { icon: CheckCircle2,  className: "text-amber-500" },
  format_failed:       { icon: AlertCircle,   className: "text-red-500" },
  worker_assigned:     { icon: UserCheck,     className: "text-amber-500" },
  working:             { icon: Loader2,       className: "text-blue-500", spin: true },
  awaiting_input:      { icon: Clock,         className: "text-amber-500" },
  awaiting_critic:     { icon: Clock,         className: "text-amber-500" },
  critic_reviewing:    { icon: Loader2,       className: "text-blue-500", spin: true },
  critic_approved:     { icon: CheckCircle2,  className: "text-green-500" },
  critic_rework:       { icon: RotateCcw,     className: "text-orange-500" },
  completed:           { icon: CheckCircle2,  className: "text-green-500" },
  failed:              { icon: AlertCircle,   className: "text-red-500" },
  blocked:             { icon: AlertCircle,   className: "text-red-500" },
  skipped:             { icon: MinusCircle,   className: "text-muted-foreground" },
  cancelled:           { icon: MinusCircle,   className: "text-muted-foreground" },
};

export interface NodeRunStatusIconProps {
  status: NodeRunStatus;
  className?: string;
}

export function NodeRunStatusIcon({ status, className }: NodeRunStatusIconProps) {
  const config = STATUS_MAP[status];

  if (!config) {
    return (
      <CircleOff
        data-testid="status-icon-fallback"
        className={cn("h-4 w-4 text-muted-foreground", className)}
      />
    );
  }

  const Icon = config.icon;
  return (
    <Icon
      data-testid={status === "pending" ? `status-icon-${status}` : "status-icon"}
      className={cn(
        "h-4 w-4 shrink-0",
        config.className,
        config.spin && "animate-spin",
        className,
      )}
    />
  );
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution/node-run-status-icon.test.tsx
```

Expected: 全部 PASS。

- [ ] **Step 4: 提交**

```bash
git add packages/views/issues/components/execution/node-run-status-icon.tsx packages/views/issues/components/execution/node-run-status-icon.test.tsx
git commit -m "feat(views): add NodeRunStatusIcon component with 16-state mapping"
```

---

### Task 6: 前端 — `ArtifactList` 组件

**Files:**
- Create: `packages/views/issues/components/execution/artifact-list.tsx`
- Create: `packages/views/issues/components/execution/artifact-list.test.tsx`

**Interfaces:**
- Produces: `ArtifactList({ nodeRun, wsId }: { nodeRun: WorkflowNodeRun; wsId: string }) => JSX.Element`
- Consumes: `useQuery` for attachments

- [ ] **Step 1: 写测试**

```tsx
// artifact-list.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { ArtifactList } from "./artifact-list";
import type { WorkflowNodeRun } from "@multica/core/types";

// Mock @multica/core/api
vi.mock("@multica/core/api", () => ({
  api: {
    listAttachments: vi.fn().mockResolvedValue({ attachments: [] }),
  },
}));

const baseRun: WorkflowNodeRun = {
  id: "run-1",
  workflow_run_id: "wr-1",
  workflow_node_id: "wn-1",
  node_title: "test",
  status: "completed",
  retry_count: 0,
  worker_type: "agent",
  worker_id: "agent-1",
  worker_output: { summary: "PR #42 created" },
  worker_agent_task_id: null,
  critic_type: "agent",
  critic_id: "agent-2",
  critic_output: { approved: true, score: 92 },
  critic_comment: "LGTM",
  critic_agent_task_id: null,
  agent_task_id: null,
  session_id: null,
  runtime_id: null,
  device_id: null,
  started_at: "2026-06-25T10:00:00Z",
  completed_at: "2026-06-25T10:05:00Z",
  created_at: "2026-06-25T10:00:00Z",
  updated_at: "2026-06-25T10:05:00Z",
};

describe("ArtifactList", () => {
  it("renders nothing when no outputs or attachments", () => {
    const empty = { ...baseRun, worker_output: null, critic_output: null };
    const { container } = render(<ArtifactList nodeRun={empty} wsId="ws-1" />);
    expect(container.firstChild).toBeNull();
  });

  it("renders worker output section when present", () => {
    render(<ArtifactList nodeRun={baseRun} wsId="ws-1" />);
    expect(screen.getByText(/Worker Output/i)).toBeInTheDocument();
  });

  it("renders critic output section when present", () => {
    render(<ArtifactList nodeRun={baseRun} wsId="ws-1" />);
    expect(screen.getByText(/Critic Output/i)).toBeInTheDocument();
  });
});
```

运行: `pnpm --filter @multica/views exec vitest run issues/components/execution/artifact-list.test.tsx`
Expected: FAIL.

- [ ] **Step 2: 实现组件**

```tsx
// artifact-list.tsx
"use client";

import type { WorkflowNodeRun } from "@multica/core/types";
import { useT } from "@multica/views/i18n";

export interface ArtifactListProps {
  nodeRun: WorkflowNodeRun;
  wsId: string;
}

export function ArtifactList({ nodeRun }: ArtifactListProps) {
  const { t } = useT("issues");
  const hasWorkerOutput = nodeRun.worker_output != null;
  const hasCriticOutput = nodeRun.critic_output != null;

  if (!hasWorkerOutput && !hasCriticOutput) return null;

  return (
    <div className="space-y-3" data-testid="artifact-list">
      {hasWorkerOutput && (
        <div>
          <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
            {t("execution.detail_panel.worker_output")}
          </h4>
          <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
            {JSON.stringify(nodeRun.worker_output, null, 2)}
          </pre>
        </div>
      )}
      {hasCriticOutput && (
        <div>
          <h4 className="text-[11px] font-medium text-muted-foreground mb-1">
            {t("execution.detail_panel.critic_output")}
          </h4>
          <pre className="text-xs bg-muted/50 rounded p-2 max-h-24 overflow-auto whitespace-pre-wrap">
            {JSON.stringify(nodeRun.critic_output, null, 2)}
          </pre>
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution/artifact-list.test.tsx
```

Expected: PASS.

- [ ] **Step 4: 提交**

```bash
git add packages/views/issues/components/execution/artifact-list.tsx packages/views/issues/components/execution/artifact-list.test.tsx
git commit -m "feat(views): add ArtifactList component for node run outputs"
```

---

### Task 7: 前端 — `RuntimeNodeCard` 组件

**Files:**
- Create: `packages/views/issues/components/execution/runtime-node-card.tsx`
- Create: `packages/views/issues/components/execution/runtime-node-card.test.tsx`

**Interfaces:**
- Produces: `RuntimeNodeCard({ node, nodeRun, workerName, criticName, onClick, isSelected, elementRef }: RuntimeNodeCardProps) => JSX.Element`
- Consumes: `NodeRunStatusIcon`, `NodeRunStatus` from `@multica/core/types`

- [ ] **Step 1: 写测试**

```tsx
// runtime-node-card.test.tsx
import { render, screen } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import userEvent from "@testing-library/user-event";
import { RuntimeNodeCard } from "./runtime-node-card";

const baseNode = {
  id: "node-1",
  workflowId: "wf-1",
  title: "需求收集",
  description: "",
  positionX: 0, positionY: 0,
  workerType: "agent" as const,
  workerId: "agent-1",
  criticType: "agent" as const,
  criticId: "agent-2",
  sortOrder: 0,
};

const completedRun = {
  id: "run-1",
  workflow_run_id: "wr-1",
  workflow_node_id: "node-1",
  node_title: "需求收集",
  status: "completed" as const,
  retry_count: 0,
  worker_type: "agent" as const,
  worker_id: "agent-1",
  worker_output: null,
  worker_agent_task_id: null,
  critic_type: "agent" as const,
  critic_id: "agent-2",
  critic_output: null,
  critic_comment: "",
  critic_agent_task_id: null,
  agent_task_id: null, session_id: null, runtime_id: null, device_id: null,
  started_at: null, completed_at: null,
  created_at: "2026-01-01", updated_at: "2026-01-01",
};

describe("RuntimeNodeCard", () => {
  it("shows green left border for completed status", () => {
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={vi.fn()}
      />,
    );
    const card = screen.getByTestId("runtime-node-card-node-1");
    expect(card).toBeInTheDocument();
  });

  it("shows no left border when nodeRun is null (not started)", () => {
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={null}
        workerName={null}
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    const card = screen.getByTestId("runtime-node-card-node-1");
    expect(card).toBeInTheDocument();
  });

  it("does not render critic row when criticType is empty", () => {
    const noCriticNode = { ...baseNode, criticType: "" as const, criticId: null };
    render(
      <RuntimeNodeCard
        node={noCriticNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName={null}
        onClick={vi.fn()}
      />,
    );
    expect(screen.queryByText(/审核员/)).not.toBeInTheDocument();
  });

  it("calls onClick on click", async () => {
    const onClick = vi.fn();
    render(
      <RuntimeNodeCard
        node={baseNode}
        nodeRun={completedRun}
        workerName="小助手"
        criticName="审核员"
        onClick={onClick}
      />,
    );
    await userEvent.click(screen.getByTestId("runtime-node-card-node-1"));
    expect(onClick).toHaveBeenCalledWith("node-1");
  });
});
```

运行: `pnpm --filter @multica/views exec vitest run issues/components/execution/runtime-node-card.test.tsx`
Expected: FAIL.

- [ ] **Step 2: 实现组件**

```tsx
// runtime-node-card.tsx
"use client";

import type { WorkflowNode, WorkflowNodeRun, NodeRunStatus } from "@multica/core/types";
import { workerTypeToActorType } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { NodeRunStatusIcon } from "./node-run-status-icon";
import { Bot, User, Paperclip } from "lucide-react";

const LEFT_BORDER_COLORS: Record<string, string> = {
  completed:         "border-l-green-500",
  critic_approved:   "border-l-green-500",
  format_checking:   "border-l-blue-500",
  working:           "border-l-blue-500",
  critic_reviewing:  "border-l-blue-500",
  pending:           "border-l-amber-500",
  format_ok:         "border-l-amber-500",
  worker_assigned:   "border-l-amber-500",
  awaiting_input:    "border-l-amber-500",
  awaiting_critic:   "border-l-amber-500",
  critic_rework:     "border-l-orange-500",
  failed:            "border-l-red-500",
  blocked:           "border-l-red-500",
  format_failed:     "border-l-red-500",
  skipped:           "border-l-muted",
  cancelled:         "border-l-muted",
};

export interface RuntimeNodeCardProps {
  node: WorkflowNode;
  nodeRun: WorkflowNodeRun | null;
  workerName: string | null;
  criticName: string | null;
  onClick: (nodeId: string) => void;
  isSelected?: boolean;
  elementRef?: (el: HTMLButtonElement | null) => void;
}

export function RuntimeNodeCard({
  node,
  nodeRun,
  workerName,
  criticName,
  onClick,
  isSelected = false,
  elementRef,
}: RuntimeNodeCardProps) {
  const status = nodeRun?.status;
  const borderClass = status ? (LEFT_BORDER_COLORS[status] ?? "") : "";
  const hasWorkerOutput = (nodeRun?.worker_output as unknown) != null;
  const artifactCount = hasWorkerOutput ? 1 : 0;

  return (
    <button
      type="button"
      data-testid={`runtime-node-card-${node.id}`}
      ref={elementRef}
      aria-pressed={isSelected}
      onClick={() => onClick(node.id)}
      className={cn(
        "flex min-w-[240px] min-h-[104px] flex-col gap-2 rounded-lg border border-border/80 bg-background p-3 text-left shadow-[0_1px_2px_rgba(15,23,42,0.06)]",
        "transition-all hover:-translate-y-0.5 hover:border-primary/45 hover:shadow-md",
        isSelected && "border-primary/55 shadow-[inset_0_0_0_1px_rgba(59,130,246,0.08),0_2px_12px_rgba(15,23,42,0.06)]",
        borderClass && `border-l-[3px] ${borderClass}`,
      )}
    >
      {/* Row 1: node title + status icon */}
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium truncate">{node.title}</span>
        {nodeRun ? (
          <NodeRunStatusIcon status={nodeRun.status} className="h-4 w-4" />
        ) : (
          <NodeRunStatusIcon status="pending" className="h-4 w-4" />
        )}
      </div>

      {/* Row 2: Worker */}
      <div className="flex items-center gap-2 h-6 text-[11px] text-muted-foreground">
        {node.worker_type === "agent" ? (
          <Bot className="h-3 w-3 shrink-0" />
        ) : node.worker_type === "human" ? (
          <User className="h-3 w-3 shrink-0" />
        ) : null}
        <span className="font-medium">Worker:</span>
        <span className={cn(!workerName && "italic")}>
          {workerName ?? "--"}
        </span>
      </div>

      {/* Row 3: Critic (only when configured) */}
      {(node.critic_type || node.critic_id) && (
        <div className="flex items-center gap-2 h-6 text-[11px] text-muted-foreground">
          {node.critic_type === "agent" ? (
            <Bot className="h-3 w-3 shrink-0" />
          ) : (
            <User className="h-3 w-3 shrink-0" />
          )}
          <span className="font-medium">Critic:</span>
          <span className={cn(!criticName && "italic")}>
            {criticName ?? "--"}
          </span>
        </div>
      )}

      {/* Row 4: Artifact count (only when > 0) */}
      {artifactCount > 0 && (
        <div className="flex items-center gap-1.5 text-[11px] text-muted-foreground">
          <Paperclip className="h-3 w-3" />
          <span>{artifactCount} artifact{artifactCount > 1 ? "s" : ""}</span>
        </div>
      )}
    </button>
  );
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution/runtime-node-card.test.tsx
```

Expected: PASS.

- [ ] **Step 4: 提交**

```bash
git add packages/views/issues/components/execution/runtime-node-card.tsx packages/views/issues/components/execution/runtime-node-card.test.tsx
git commit -m "feat(views): add RuntimeNodeCard with status border, worker/critic rows"
```

---

### Task 8: 前端 — 扩展 `StageLane` 支持 `mode="runtime"`

**Files:**
- Modify: `packages/views/workflows/components/overview/stage-lane.tsx`

**Interfaces:**
- Consumes: `RuntimeNodeCard`, `WorkflowNodeRun`
- Produces: `StageLaneProps` 新增可选字段 `mode?: "template" | "runtime"`, `nodeRuns?: Map<string, WorkflowNodeRun>`, `onNodeClick?: (nodeId: string) => void`

- [ ] **Step 1: 修改 Props 接口并实现条件渲染**

修改 `StageLaneProps`：

```typescript
export interface StageLaneProps {
  // ... existing props ...
  mode?: "template" | "runtime";
  nodeRuns?: Map<string, WorkflowNodeRun>;
  onNodeClick?: (nodeId: string) => void;
}
```

在 `StageLane` 函数体解构中添加默认值：

```typescript
export function StageLane({
  stage, nodeIds, getActorName, agentLookup, pluginLookup,
  onCardClick, selectedCard = null, nodeElementRefs, criticElementRefs,
  mode = "template",
  nodeRuns,
  onNodeClick,
}: StageLaneProps) {
```

在节点渲染部分（约第 98 行循环处），根据 `mode` 切换：

```tsx
{sortedNodes.map((node) => {
  if (mode === "runtime") {
    const nodeRun = nodeRuns?.get(node.id) ?? null;
    const workerName = node.worker_id
      ? getActorName(workerTypeToActorType(node.worker_type), node.worker_id)
      : null;
    const criticName = node.critic_id
      ? getActorName(node.critic_type ?? "agent", node.critic_id)
      : null;

    return (
      <RuntimeNodeCard
        key={node.id}
        node={node}
        nodeRun={nodeRun}
        workerName={workerName}
        criticName={criticName}
        onClick={(id) => onNodeClick?.(id)}
      />
    );
  }
  // ... existing template mode code ...
})}
```

需要新增 import：
```typescript
import { RuntimeNodeCard } from "../../../issues/components/execution/runtime-node-card";
import type { WorkflowNodeRun } from "@multica/core/types";
```

- [ ] **Step 2: 运行已有 Panorama 测试确保不破坏**

```bash
pnpm --filter @multica/views exec vitest run workflows/components/overview/stage-lane.test.tsx
```

Expected: 现有测试全部 PASS。

- [ ] **Step 3: 提交**

```bash
git add packages/views/workflows/components/overview/stage-lane.tsx
git commit -m "feat(views): extend StageLane with runtime mode for issue execution view"
```

---

### Task 9: 前端 — `ExecutionDetailPanel` 组件

**Files:**
- Create: `packages/views/issues/components/execution/execution-detail-panel.tsx`
- Create: `packages/views/issues/components/execution/execution-detail-panel.test.tsx`

**Interfaces:**
- Produces: `ExecutionDetailPanel({ node, nodeRun, workerName, criticName, onClose, wsId }: ExecutionDetailPanelProps) => JSX.Element`

- [ ] **Step 1: 写测试**

```tsx
// execution-detail-panel.test.tsx
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import { ExecutionDetailPanel } from "./execution-detail-panel";

const node = { id: "n1", workflowId: "w1", title: "编码", description: "",
  positionX: 0, positionY: 0, workerType: "agent" as const, workerId: "a1",
  criticType: "agent" as const, criticId: "a2", sortOrder: 0 };

const run = {
  id: "r1", workflow_run_id: "wr1", workflow_node_id: "n1",
  node_title: "编码", status: "working" as const,
  retry_count: 0, worker_type: "agent" as const, worker_id: "a1",
  worker_output: { pr: "#42" }, worker_agent_task_id: null,
  critic_type: "agent" as const, critic_id: "a2",
  critic_output: null, critic_comment: "", critic_agent_task_id: null,
  agent_task_id: null, session_id: null, runtime_id: null, device_id: null,
  started_at: "2026-06-25T10:00:00Z", completed_at: null,
  created_at: "2026-06-25T10:00:00Z", updated_at: "2026-06-25T10:05:00Z",
};

describe("ExecutionDetailPanel", () => {
  it("renders node title in header", () => {
    render(
      <ExecutionDetailPanel
        node={node} nodeRun={run} workerName="后端助手"
        criticName="审核员" onClose={vi.fn()} wsId="ws-1"
      />,
    );
    expect(screen.getByText("编码")).toBeInTheDocument();
  });

  it("calls onClose when clicking mask", async () => {
    const onClose = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node} nodeRun={run} workerName="后端助手"
        criticName="审核员" onClose={onClose} wsId="ws-1"
      />,
    );
    await userEvent.click(screen.getByTestId("detail-panel-mask"));
    expect(onClose).toHaveBeenCalled();
  });

  it("calls onClose on Escape key", async () => {
    const onClose = vi.fn();
    render(
      <ExecutionDetailPanel
        node={node} nodeRun={run} workerName="后端助手"
        criticName="审核员" onClose={onClose} wsId="ws-1"
      />,
    );
    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalled();
  });

  it("shows 'Not configured' when no critic", () => {
    render(
      <ExecutionDetailPanel
        node={{ ...node, criticType: "" as const, criticId: null }}
        nodeRun={run} workerName="后端助手" criticName={null}
        onClose={vi.fn()} wsId="ws-1"
      />,
    );
    expect(screen.getByText(/Not configured/i)).toBeInTheDocument();
  });
});
```

运行: `pnpm --filter @multica/views exec vitest run issues/components/execution/execution-detail-panel.test.tsx`
Expected: FAIL.

- [ ] **Step 2: 实现组件**

```tsx
// execution-detail-panel.tsx
"use client";

import { useEffect } from "react";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";
import { X, Bot, User, Clock, RotateCcw } from "lucide-react";
import { useT } from "@multica/views/i18n";
import { NodeRunStatusIcon } from "./node-run-status-icon";
import { ArtifactList } from "./artifact-list";
import { cn } from "@multica/ui/lib/utils";

export interface ExecutionDetailPanelProps {
  node: WorkflowNode;
  nodeRun: WorkflowNodeRun | null;
  workerName: string | null;
  criticName: string | null;
  onClose: () => void;
  wsId: string;
}

export function ExecutionDetailPanel({
  node,
  nodeRun,
  workerName,
  criticName,
  onClose,
  wsId,
}: ExecutionDetailPanelProps) {
  const { t } = useT("issues");

  useEffect(() => {
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", handleKey);
    return () => document.removeEventListener("keydown", handleKey);
  }, [onClose]);

  const status = nodeRun?.status;
  const duration = nodeRun?.started_at && nodeRun?.completed_at
    ? Math.round((new Date(nodeRun.completed_at).getTime() - new Date(nodeRun.started_at).getTime()) / 1000)
    : null;

  return (
    <>
      {/* Mask */}
      <div
        data-testid="detail-panel-mask"
        className="fixed inset-0 z-40 bg-slate-950/18 backdrop-blur-[1px]"
        onClick={onClose}
      />

      {/* Panel */}
      <aside className="fixed right-0 top-0 bottom-0 z-50 w-[520px] bg-background/98 backdrop-blur shadow-xl border-l border-border/60 flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-border/60 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <h2 className="text-base font-semibold truncate">{node.title}</h2>
            {status && <NodeRunStatusIcon status={status} />}
          </div>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-muted" aria-label="Close">
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto px-5 py-4 space-y-6">
          {/* Status path visualization */}
          {status && (
            <section>
              <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
                {t("execution.detail_panel.status_path")}
              </h3>
              <div className="flex items-center gap-2 text-xs">
                <span className={cn("px-2 py-0.5 rounded", status === "format_checking" || status === "format_ok" ? "bg-blue-50 text-blue-700" : "bg-muted/50")}>Format</span>
                <span className="text-muted-foreground">→</span>
                <span className={cn("px-2 py-0.5 rounded", status === "working" ? "bg-blue-50 text-blue-700" : "bg-muted/50")}>Worker</span>
                <span className="text-muted-foreground">→</span>
                <span className={cn("px-2 py-0.5 rounded", status === "critic_reviewing" || status === "critic_approved" ? "bg-green-50 text-green-700" : "bg-muted/50")}>Critic</span>
              </div>
            </section>
          )}

          {/* Worker info */}
          <section>
            <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
              {t("execution.detail_panel.worker")}
            </h3>
            <div className="flex items-center gap-2 text-sm">
              {node.worker_type === "agent" ? <Bot className="h-4 w-4" /> : <User className="h-4 w-4" />}
              <span className="font-medium">{workerName ?? "--"}</span>
              {nodeRun && <NodeRunStatusIcon status={nodeRun.status} className="h-3.5 w-3.5" />}
            </div>
          </section>

          {/* Critic info */}
          <section>
            <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
              {t("execution.detail_panel.critic")}
            </h3>
            {(node.critic_type || node.critic_id) ? (
              <>
                <div className="flex items-center gap-2 text-sm">
                  {nodeRun?.critic_type === "agent" ? <Bot className="h-4 w-4" /> : <User className="h-4 w-4" />}
                  <span className="font-medium">{criticName ?? "--"}</span>
                </div>
                {nodeRun?.critic_comment && (
                  <p className="text-xs text-muted-foreground mt-1 italic">
                    &ldquo;{nodeRun.critic_comment}&rdquo;
                  </p>
                )}
              </>
            ) : (
              <p className="text-xs text-muted-foreground italic">{t("execution.detail_panel.not_configured")}</p>
            )}
          </section>

          {/* Artifacts */}
          {nodeRun && <ArtifactList nodeRun={nodeRun} wsId={wsId} />}

          {/* Metadata */}
          {nodeRun && (
            <section>
              <h3 className="text-[11px] font-medium text-muted-foreground uppercase tracking-wide mb-2">
                {t("execution.detail_panel.metadata")}
              </h3>
              <dl className="text-xs space-y-1">
                {nodeRun.started_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">{t("execution.detail_panel.started_at")}</dt>
                    <dd>{new Date(nodeRun.started_at).toLocaleString()}</dd>
                  </div>
                )}
                {nodeRun.completed_at && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">{t("execution.detail_panel.completed_at")}</dt>
                    <dd>{new Date(nodeRun.completed_at).toLocaleString()}</dd>
                  </div>
                )}
                {duration != null && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">{t("execution.detail_panel.duration")}</dt>
                    <dd>{duration}s</dd>
                  </div>
                )}
                {nodeRun.retry_count > 0 && (
                  <div className="flex justify-between">
                    <dt className="text-muted-foreground">{t("execution.detail_panel.retry_count")}</dt>
                    <dd>{nodeRun.retry_count}</dd>
                  </div>
                )}
              </dl>
            </section>
          )}
        </div>
      </aside>
    </>
  );
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution/execution-detail-panel.test.tsx
```

Expected: PASS.

- [ ] **Step 4: 提交**

```bash
git add packages/views/issues/components/execution/execution-detail-panel.tsx packages/views/issues/components/execution/execution-detail-panel.test.tsx
git commit -m "feat(views): add ExecutionDetailPanel with status path, worker/critic, artifacts, metadata"
```

---

### Task 10: 前端 — `ExecutionPanoramaPage` 主组件 + barrel export

**Files:**
- Create: `packages/views/issues/components/execution/execution-panorama-page.tsx`
- Create: `packages/views/issues/components/execution/execution-panorama-page.test.tsx`
- Create: `packages/views/issues/components/execution/index.ts`

**Interfaces:**
- Produces: `ExecutionPanoramaPage({ workflowId, runId, wsId }: ExecutionPanoramaPageProps) => JSX.Element`
- Consumes: `workflowDetailOptions`, `workflowStagesOptions`, `workflowNodesOptions`, `workflowEdgesOptions`, `workflowNodeRunsOptions`, `agentListOptions`, `builtinPluginListOptions`, `StageLane`, `PanoramaSvgOverlay`, `StageTransitionBar`

- [ ] **Step 1: 写测试**

```tsx
// execution-panorama-page.test.tsx
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ExecutionPanoramaPage } from "./execution-panorama-page";

// Mock queries
vi.mock("@multica/core/workflows/queries", () => ({
  workflowDetailOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
  workflowStagesOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
  workflowNodesOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
  workflowEdgesOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
  workflowNodeRunsOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
}));

vi.mock("@multica/core/agents/queries", () => ({
  agentListOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
}));

vi.mock("@multica/core/api/schemas", () => ({
  builtinPluginListOptions: vi.fn(() => ({ queryKey: [], queryFn: vi.fn() })),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  const qc = new QueryClient();
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

describe("ExecutionPanoramaPage", () => {
  it("renders loading state when data is loading", () => {
    render(
      <Wrapper>
        <ExecutionPanoramaPage workflowId="wf-1" runId={null} wsId="ws-1" />
      </Wrapper>,
    );
    expect(screen.getByRole("status")).toBeInTheDocument(); // aria-busy spinner
  });

  it("renders unassigned lane when no stages defined", async () => {
    // ...mock data with no stages...
    // Expect "Unassigned" heading
  });
});
```

运行: `pnpm --filter @multica/views exec vitest run issues/components/execution/execution-panorama-page.test.tsx`
Expected: FAIL.

- [ ] **Step 2: 实现主组件**

```tsx
// execution-panorama-page.tsx
"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  workflowDetailOptions,
  workflowStagesOptions,
  workflowNodesOptions,
  workflowEdgesOptions,
  workflowNodeRunsOptions,
} from "@multica/core/workflows/queries";
import { agentListOptions } from "@multica/core/agents/queries";
import { builtinPluginListOptions } from "@multica/core/api/schemas";
import type { WorkflowNode, WorkflowNodeRun } from "@multica/core/types";
import { workerTypeToActorType } from "@multica/core/types";
import { StageLane } from "../../../workflows/components/overview/stage-lane";
import { PanoramaSvgOverlay } from "../../../workflows/components/overview/panorama-svg-overlay";
import { ExecutionDetailPanel } from "./execution-detail-panel";
import { useT } from "@multica/views/i18n";
import { Loader2 } from "lucide-react";

export interface ExecutionPanoramaPageProps {
  workflowId: string;
  runId: string | null;
  wsId: string;
}

export function ExecutionPanoramaPage({ workflowId, runId, wsId }: ExecutionPanoramaPageProps) {
  const { t } = useT("issues");

  const { data: workflow, isLoading: wfLoading } = useQuery(workflowDetailOptions(wsId, workflowId));
  const { data: stages, isLoading: stLoading } = useQuery(workflowStagesOptions(wsId, workflowId));
  const { data: nodes, isLoading: ndLoading } = useQuery(workflowNodesOptions(wsId, workflowId));
  const { data: edges, isLoading: edLoading } = useQuery(workflowEdgesOptions(wsId, workflowId));
  const { data: nodeRuns } = useQuery({
    ...workflowNodeRunsOptions(wsId, workflowId, runId ?? ""),
    enabled: !!runId,
  });
  const { data: agents } = useQuery(agentListOptions(wsId));
  const { data: plugins } = useQuery(builtinPluginListOptions(wsId));

  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);

  const nodeRunMap = useMemo(() => {
    const map = new Map<string, WorkflowNodeRun>();
    if (nodeRuns) {
      for (const nr of nodeRuns) {
        map.set(nr.workflow_node_id, nr);
      }
    }
    return map;
  }, [nodeRuns]);

  const agentLookup = useMemo(() => {
    const map = new Map<string, Agent | null>();
    if (agents) {
      for (const a of agents) map.set(a.id, a);
    }
    return map;
  }, [agents]);

  const getActorName = (type: string, id: string) => {
    if (type === "agent" || type === "human") {
      return agentLookup.get(id)?.name ?? null;
    }
    return null;
  };

  const isLoading = wfLoading || stLoading || ndLoading || edLoading;

  if (isLoading) {
    return (
      <div role="status" className="flex items-center justify-center py-20">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  const allStages = stages ?? [];
  const allNodes = nodes ?? [];
  const nodesByStage = new Map<string | null, WorkflowNode[]>();
  for (const node of allNodes) {
    const key = node.stageId ?? null;
    if (!nodesByStage.has(key)) nodesByStage.set(key, []);
    nodesByStage.get(key)!.push(node);
  }

  const unassignedNodes = nodesByStage.get(null) ?? [];
  const selectedNode = allNodes.find(n => n.id === selectedNodeId) ?? null;
  const selectedRun = selectedNodeId ? nodeRunMap.get(selectedNodeId) ?? null : null;

  return (
    <div className="relative flex flex-col min-h-0" data-testid="execution-panorama">
      <div className="relative overflow-auto p-3" data-testid="panorama-canvas">
        {/* SVG overlay for edges (only when run exists) */}
        {runId && (
          <PanoramaSvgOverlay
            stages={allStages}
            nodes={allNodes}
            edges={edges ?? []}
            nodeElementRefs={new Map()} // populated by StageLane refs
            criticElementRefs={new Map()}
          />
        )}

        {allStages.length === 0 ? (
          <StageLane
            stage={{ id: "unassigned", workflowId, name: t("execution.panorama.unassigned"), description: "", sortOrder: 0, nodeCount: unassignedNodes.length, createdAt: "", updatedAt: "" }}
            nodeIds={unassignedNodes}
            getActorName={getActorName}
            agentLookup={new Map()}
            pluginLookup={new Map()}
            onCardClick={() => {}}
            nodeElementRefs={new Map()}
            criticElementRefs={new Map()}
            mode="runtime"
            nodeRuns={nodeRunMap}
            onNodeClick={(id) => setSelectedNodeId(id)}
          />
        ) : (
          allStages.sort((a, b) => a.sortOrder - b.sortOrder).map((stage, i) => (
            <div key={stage.id}>
              {i > 0 && <div className="h-2 bg-gradient-to-b from-slate-50/40 to-stone-50/40" data-testid="stage-transition-gradient" />}
              <StageLane
                stage={stage}
                nodeIds={nodesByStage.get(stage.id) ?? []}
                getActorName={getActorName}
                agentLookup={agentLookup}
                pluginLookup={pluginLookup}
                onCardClick={() => {}}
                nodeElementRefs={new Map()}
                criticElementRefs={new Map()}
                mode="runtime"
                nodeRuns={nodeRunMap}
                onNodeClick={(id) => setSelectedNodeId(id)}
              />
            </div>
          ))
        )}

        {/* Unassigned nodes (stage_id = NULL) when stages exist */}
        {allStages.length > 0 && unassignedNodes.length > 0 && (
          <StageLane
            stage={{ id: "unassigned", workflowId, name: t("execution.panorama.unassigned"), description: "", sortOrder: 999, nodeCount: unassignedNodes.length, createdAt: "", updatedAt: "" }}
            nodeIds={unassignedNodes}
            getActorName={getActorName}
            agentLookup={new Map()}
            pluginLookup={new Map()}
            onCardClick={() => {}}
            nodeElementRefs={new Map()}
            criticElementRefs={new Map()}
            mode="runtime"
            nodeRuns={nodeRunMap}
            onNodeClick={(id) => setSelectedNodeId(id)}
          />
        )}
      </div>

      {/* Detail panel */}
      {selectedNodeId && selectedNode && (
        <ExecutionDetailPanel
          node={selectedNode}
          nodeRun={selectedRun}
          workerName={selectedNode.worker_id ? getActorName(workerTypeToActorType(selectedNode.worker_type), selectedNode.worker_id) : null}
          criticName={selectedNode.critic_id ? getActorName(selectedNode.critic_type ?? "agent", selectedNode.critic_id) : null}
          onClose={() => setSelectedNodeId(null)}
          wsId={wsId}
        />
      )}
    </div>
  );
}
```

- [ ] **Step 3: 创建 barrel export**

```typescript
// index.ts
export { ExecutionPanoramaPage } from "./execution-panorama-page";
export { RuntimeNodeCard } from "./runtime-node-card";
export { NodeRunStatusIcon } from "./node-run-status-icon";
export { ExecutionDetailPanel } from "./execution-detail-panel";
export { ArtifactList } from "./artifact-list";
```

- [ ] **Step 4: 运行测试确认通过**

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution/execution-panorama-page.test.tsx
```

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add packages/views/issues/components/execution/execution-panorama-page.tsx packages/views/issues/components/execution/execution-panorama-page.test.tsx packages/views/issues/components/execution/index.ts
git commit -m "feat(views): add ExecutionPanoramaPage main component with stage lanes, SVG edges, detail panel"
```

---

### Task 11: 前端 — 在 `IssueDetail` 中集成新的全景图

**Files:**
- Modify: `packages/views/issues/components/issue-detail.tsx:1913-1924`

**Interfaces:**
- Consumes: `ExecutionPanoramaPage`
- Modifies: `assignee_type === "workflow"` 条件分支，从 `WorkflowDagViewer` 切换到 `ExecutionPanoramaPage`

- [ ] **Step 1: 替换 WorkflowDagViewer 为 ExecutionPanoramaPage**

找到 `issue-detail.tsx` 中的 WorkflowDagViewer 引用（约第 1914-1924 行），替换为：

```tsx
{/* Workflow Execution Panorama — shown when the issue is assigned to a workflow */}
{issue.assignee_type === "workflow" && issue.assignee_id && (
  <div className="mt-10">
    <ExecutionPanoramaPage
      workflowId={issue.assignee_id}
      runId={issue.workflow_run_id}
      wsId={wsId}
    />
  </div>
)}
```

同时更新 import：
```typescript
import { ExecutionPanoramaPage } from "./execution";
```

注意：旧的 `onRunningChange` prop 不再需要——`setIsWorkflowRunning` 可以通过另一种方式推导（如从 `nodeRuns` 数据中判断），或暂时保留 `isWorkflowRunning` 状态以兼容 `AssigneePicker`/`StagePicker` 的 disabled 逻辑。

- [ ] **Step 2: 更新 `isWorkflowRunning` 状态来源**

在 `ExecutionPanoramaPage` 中新增 `onRunningChange` 回调 prop（可选），或者在 `IssueDetail` 中用 `useQuery(workflowNodeRunsOptions)` 自行判断：

```tsx
// 在 IssueDetail 中
const { data: nodeRuns } = useQuery({
  ...workflowNodeRunsOptions(wsId, issue.assignee_id, issue.workflow_run_id ?? ""),
  enabled: issue.assignee_type === "workflow" && !!issue.workflow_run_id,
});

const isWorkflowRunning = useMemo(() => {
  if (!nodeRuns) return false;
  const terminal = new Set(["completed", "critic_approved", "failed", "blocked", "skipped", "cancelled"]);
  return !nodeRuns.every((nr) => terminal.has(nr.status));
}, [nodeRuns]);
```

- [ ] **Step 3: 类型检查 + 运行已有测试**

```bash
pnpm typecheck
pnpm --filter @multica/views exec vitest run issues/components/issue-detail.test.tsx
```

Expected: 类型无错误，测试通过。

- [ ] **Step 4: 提交**

```bash
git add packages/views/issues/components/issue-detail.tsx
git commit -m "feat(views): replace WorkflowDagViewer with ExecutionPanoramaPage in issue detail"
```

---

### Task 12: 前端 — i18n 中英文 key

**Files:**
- Modify: `packages/views/locales/zh-Hans/issues.json`
- Modify: `packages/views/locales/en/issues.json`

**Interfaces:**
- Produces: `issues.execution.*` 命名空间完整

- [ ] **Step 1: 追加中文 i18n**

在 `packages/views/locales/zh-Hans/issues.json` 末尾（最后一个键值对之前）追加：

```json
"execution": {
  "panorama": {
    "not_started": "未启动",
    "no_worker": "未配置 Worker",
    "no_run": "Workflow 尚未启动",
    "empty_stage": "此阶段暂无节点",
    "unassigned": "未分组"
  },
  "card": {
    "worker_label": "Worker",
    "critic_label": "Critic",
    "artifacts_count": "{{count}} 个产物",
    "artifacts_count_plural": "{{count}} 个产物"
  },
  "detail_panel": {
    "title": "节点详情",
    "status_path": "状态路径",
    "worker": "Worker",
    "critic": "Critic",
    "worker_output": "Worker 输出",
    "critic_output": "Critic 输出",
    "attachments": "附件",
    "not_configured": "未配置",
    "no_output": "暂无输出",
    "review_comment": "审核意见",
    "metadata": "元数据",
    "started_at": "开始时间",
    "completed_at": "完成时间",
    "duration": "耗时",
    "retry_count": "重试次数",
    "error": "错误",
    "view_full_issue": "查看完整 Issue"
  }
}
```

- [ ] **Step 2: 追加英文 i18n**

在 `packages/views/locales/en/issues.json` 中追加同样的结构，值为英文：

```json
"execution": {
  "panorama": {
    "not_started": "Not started",
    "no_worker": "No worker configured",
    "no_run": "Workflow not started yet",
    "empty_stage": "No nodes in this stage",
    "unassigned": "Unassigned"
  },
  "card": {
    "worker_label": "Worker",
    "critic_label": "Critic",
    "artifacts_count": "{{count}} artifact",
    "artifacts_count_plural": "{{count}} artifacts"
  },
  "detail_panel": {
    "title": "Node Detail",
    "status_path": "Status Path",
    "worker": "Worker",
    "critic": "Critic",
    "worker_output": "Worker Output",
    "critic_output": "Critic Output",
    "attachments": "Attachments",
    "not_configured": "Not configured",
    "no_output": "No output yet",
    "review_comment": "Review Comment",
    "metadata": "Metadata",
    "started_at": "Started",
    "completed_at": "Completed",
    "duration": "Duration",
    "retry_count": "Retries",
    "error": "Error",
    "view_full_issue": "View full issue"
  },
  "status": {
    "pending": "Pending",
    "format_checking": "Format Checking",
    "format_ok": "Format OK",
    "format_failed": "Format Failed",
    "worker_assigned": "Assigned",
    "working": "Working",
    "awaiting_input": "Awaiting Input",
    "awaiting_critic": "Awaiting Critic",
    "critic_reviewing": "Reviewing",
    "critic_approved": "Approved",
    "critic_rework": "Rework",
    "completed": "Completed",
    "failed": "Failed",
    "blocked": "Blocked",
    "skipped": "Skipped",
    "cancelled": "Cancelled"
  }
}
```

- [ ] **Step 3: 验证 JSON 格式**

```bash
node -e "JSON.parse(require('fs').readFileSync('packages/views/locales/en/issues.json','utf8'))" && echo "en OK"
node -e "JSON.parse(require('fs').readFileSync('packages/views/locales/zh-Hans/issues.json','utf8'))" && echo "zh-Hans OK"
```

Expected: 两者均输出 `OK`。

- [ ] **Step 4: 提交**

```bash
git add packages/views/locales/zh-Hans/issues.json packages/views/locales/en/issues.json
git commit -m "feat(i18n): add execution panorama i18n keys for issue detail"
```

---

### Task 13: 最终验证 — 全量检查

**Files:** 无（验证步骤）

- [ ] **Step 1: 运行完整检查**

```bash
make check
```

Expected: `typecheck` + `pnpm test` + `make test` + `e2e` 全部通过。

- [ ] **Step 2: 如果失败，阅读错误输出，修复，重复直到全部通过**

常见需要修复的点：
- `StageLane` 循环 import（`stage-lane.tsx` 导入 `RuntimeNodeCard` 而 `RuntimeNodeCard` 可能在执行包中）→ 必要时将 `RuntimeNodeCard` 作为 prop 注入而非直接 import
- 测试 mock 路径需匹配 `@multica/core` 的模块解析规则
- Go sqlc 生成的参数名需与 SQL 注释中的命名一致

- [ ] **Step 3: 全部通过后提交**

```bash
git add -A
git commit -m "chore: final verification fixes for issue execution panorama"
```
