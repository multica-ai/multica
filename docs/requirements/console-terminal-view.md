# 助理控制台（Assistant Console）需求文档

## 需求背景

当前 Multica 的 Chat 功能是一个右下角浮动窗口，适合快速对话场景。但对于需要并行监控多个 Agent 执行情况、查看详细终端输出、快速切换上下文的场景，需要一个专门的全屏页面。

**用户场景**：
- 同时运行多个任务，需要一眼看到所有会话的状态（运行中、完成、失败）
- 需要查看详细的终端输出和日志流
- 在不同 Agent CLI（Claude Code、Codex、OpenClaw 等）之间快速切换
- 希望有持久化的会话列表，而不是通过下拉菜单查找

## 功能目标

在左侧导航栏增加**"助理"（Assistant）**入口，点击后进入一个**全屏双栏布局页面**，作为现有 Chat 窗口的补充（不替代），给需要深度监控和多会话管理的用户使用。

## 核心功能

### 1. 左侧导航栏新增入口

在现有的左侧导航栏（Issues、项目、自动化、智能体、小队、用量）中，增加一个**"助理"（Assistant）**入口：

```
工作区
  📋 Issues
  📁 项目
  ⚡ 自动化
  🤖 智能体
  👥 小队
  📊 用量
  💬 助理        ← 新增入口
```

**图标建议**：💬（对话气泡）或 🎯（控制台）

### 2. 双栏布局页面

点击"助理"后，进入全屏双栏布局页面：

```
┌─────────────────────────────────────────────────────────┐
│  Workspace > 助理                        [设置] [最小化] │
├────────────────┬────────────────────────────────────────┤
│  会话列表       │  消息输出区域                           │
│  (Sessions)    │  (Messages)                           │
│                │                                        │
│  🟣 Claude Code│  You                        10:23 AM  │
│  Fix login bug │  Fix the scroll bug in snake game    │
│  🟢 Running 2m │                                        │
│                │  Claude Code                 10:23 AM  │
│  🟣 Codex      │  I'll analyze the issue...            │
│  Add feature   │  [Terminal Output]                    │
│  ✅ Completed  │  $ git status                         │
│                │  On branch main...                    │
│  🟣 OpenClaw   │                                        │
│  Review PR     │  ● Task running for 2m 15s            │
│  ⚪ Idle       │                                        │
│                │                                        │
│  + 新建会话    │  ─────────────────────────────────────│
│                │  [Agent] [📎] Input...        [Send]  │
└────────────────┴────────────────────────────────────────┘
```

### 3. 左侧会话列表（Session List）

**显示内容**：
- Agent 头像 + 名称（Claude Code、Codex、OpenClaw 等）
- 会话标题（自动生成或用户重命名）
- 实时状态：
  - 🟢 Running（运行中） - **显示实时滚动的运行时长**（如"2m 15s"）
  - ✅ Completed（已完成） - 显示完成时间
  - ❌ Failed（失败） - 显示错误摘要
  - ⚪ Idle（空闲）
- 未读标记（有新消息时高亮）

**交互**：
- 点击切换到对应会话
- 右键菜单或 hover 显示操作按钮：
  - 重命名
  - 停止执行（如果正在运行）
  - 删除会话

**布局示意**：

```
┌────────────────────────────────────┐
│ 🟣 Claude Code        🟢 Running   │  ← Agent 头像 + 名称 + 状态
│ Fix snake game scroll bug          │  ← 会话标题
│ 2m 15s                             │  ← 实时运行时长（每秒更新）
└────────────────────────────────────┘

┌────────────────────────────────────┐
│ 🟣 Codex              ✅ Completed │
│ Add multi-terminal console         │
│ 5m ago                             │
└────────────────────────────────────┘
```

**状态颜色**：
- 🟢 Running - 绿色脉动动画 + **实时滚动时长**
- ✅ Completed - 灰色
- ❌ Failed - 红色
- ⚪ Idle - 灰色

### 4. 右侧消息输出区域（Messages Output）

**显示内容**：
- 完整的对话历史（User / Agent 消息）
- 等宽字体的代码输出（语法高亮）
- 实时流式输出（Agent 执行时逐字显示）
- 任务状态指示器（类似现有的 TaskStatusPill）
- 附件（文件上传/下载）

**交互**：
- 滚动查看历史
- 选中文本复制
- 点击代码块复制
- 点击附件预览/下载

**复用现有组件**：
- ✅ **直接复用 `<ChatMessageList>` 组件** (`packages/views/chat/components/chat-message-list.tsx`)
  - 已支持完整的消息渲染、实时滚动、任务状态显示
- ✅ **复用 `<Markdown>` 组件** (`packages/views/common/markdown.tsx`)
  - 支持 issue mentions、附件、图片预览
- ✅ **复用 `<CodeBlock>` 组件** (`packages/ui/markdown/CodeBlock.tsx`)
  - Shiki 语法高亮（200+ 语言）
  - 支持 `terminal` 模式（等宽字体、保留控制字符）
  - 自动深色/浅色主题
  - 内置复制按钮

**无需新开发**：所有输出渲染组件已就绪，只需调整布局和样式。

### 5. 输入区域（Input Area）

**功能**：
- 多行文本输入
- 文件上传（拖拽或点击 📎 按钮）
- Agent 切换器（快速切换当前对话的 Agent）
- 发送按钮 / 停止按钮（执行中时）

**复用现有组件**：
- ✅ **复用 `<ChatInput>` 组件** (`packages/views/chat/components/chat-input.tsx`)

### 6. 顶部导航栏

**显示内容**：
- 面包屑导航：`Workspace > 助理`
- 设置按钮（可选：字体大小、显示选项）
- 最小化/关闭按钮（返回工作区）

## 技术实现方案

### 前端架构

**复用现有基础设施**（无需新开发）：
- **API 层**：完全复用现有的 Chat API（`/api/chat/sessions`、`/api/chat/messages` 等）
- **State 管理**：复用 `useChatStore`（`packages/core/chat/store.ts`）
- **WebSocket**：复用现有的实时更新机制（`packages/core/realtime/`）
- **Queries**：复用 `chatSessionsOptions`、`chatMessagesOptions` 等
- **UI 组件**：
  - ✅ `<ChatMessageList>` - 消息列表渲染
  - ✅ `<ChatInput>` - 输入区域
  - ✅ `<Markdown>` + `<CodeBlock>` - 代码高亮和 Markdown 渲染
  - ✅ `<TaskStatusPill>` - 任务状态指示器

**新增组件**（路径：`packages/views/assistant/`）：
- `assistant-page.tsx` - 主页面容器（双栏布局）
- `session-list.tsx` - 左侧会话列表
- `session-list-item.tsx` - 单个会话项组件
- `running-duration.tsx` - 实时运行时长显示组件

**路由**：
- Web：`/[slug]/assistant`
- Desktop：`/:slug/assistant`

### 左侧导航栏修改

在 `packages/views/layout/sidebar.tsx`（或对应的导航组件）中增加"助理"入口：

```tsx
// 现有导航项
<NavItem icon={<Inbox />} label="Issues" href={`/${slug}/issues`} />
<NavItem icon={<Folder />} label="项目" href={`/${slug}/projects`} />
// ... 其他项目
<NavItem icon={<MessageSquare />} label="助理" href={`/${slug}/assistant`} /> // 新增
```

### 数据流

```typescript
// 1. 获取所有会话
const { data: sessions } = useQuery(chatSessionsOptions(wsId));

// 2. 选中某个会话
const activeSessionId = useChatStore(s => s.activeSessionId);

// 3. 获取该会话的消息
const { data: messages } = useQuery(chatMessagesOptions(activeSessionId));

// 4. 获取该会话的运行状态
const { data: pendingTask } = useQuery(pendingChatTaskOptions(activeSessionId));

// 5. WebSocket 自动同步状态（无需额外实现）
```

### 实时运行时长实现

使用 `useEffect` + `setInterval` 实现每秒更新：

```typescript
function RunningDuration({ startedAt }: { startedAt: string }) {
  const [elapsed, setElapsed] = useState(0);
  
  useEffect(() => {
    const start = new Date(startedAt).getTime();
    const timer = setInterval(() => {
      const now = Date.now();
      setElapsed(Math.floor((now - start) / 1000));
    }, 1000);
    
    return () => clearInterval(timer);
  }, [startedAt]);
  
  return <span>{formatDuration(elapsed)}</span>; // "2m 15s"
}
```

### 后端

**无需新增 API**。完全复用现有的 Chat 接口：
- `POST /api/chat/sessions` - 创建会话
- `GET /api/chat/sessions` - 获取会话列表
- `GET /api/chat/sessions/:id/messages` - 获取消息
- `POST /api/chat/sessions/:id/messages` - 发送消息
- `DELETE /api/chat/sessions/:id` - 删除会话
- `PATCH /api/chat/sessions/:id` - 重命名会话
- `POST /api/tasks/:id/cancel` - 停止任务
- WebSocket 事件（`chat:message`, `chat:done`, `task:started` 等）

## UI/UX 细节

### 会话列表项设计

```
┌────────────────────────────────────┐
│ 🟣 Claude Code        🟢 2m 15s    │  ← Agent 头像 + 名称 + 实时运行时长
│ Fix snake game scroll bug          │  ← 会话标题（hover 显示全文）
│                             [⋮]    │  ← Hover 显示操作菜单
└────────────────────────────────────┘
```

**状态显示**：
- 🟢 Running - 绿色圆点 + 实时滚动时长（每秒更新："2m 15s" → "2m 16s"）
- ✅ Completed - 灰色对勾 + 完成时间（"5m ago"）
- ❌ Failed - 红色叉号 + 错误摘要
- ⚪ Idle - 灰色圆点

### 消息输出样式

**直接复用现有的 `<ChatMessageList>` 渲染**，样式已经完善：

```
┌────────────────────────────────────────────────┐
│ You                                  10:23 AM  │
│ Fix the scroll bug in snake game              │
│                                                │
│ Claude Code                          10:23 AM  │
│ I'll analyze the issue...                     │
│                                                │
│ [Code Block with Syntax Highlighting]         │
│ ┌──────────────────────────────────────────┐  │
│ │ javascript                    [Copy]     │  │
│ ├──────────────────────────────────────────┤  │
│ │ function fixScroll() {                   │  │
│ │   // ...                                 │  │
│ │ }                                        │  │
│ └──────────────────────────────────────────┘  │
│                                                │
│ ● Task running for 2m 15s                     │  ← 状态指示器
└────────────────────────────────────────────────┘
```

### 响应式设计

- **Desktop / Web**：默认左右分栏（25% / 75%）
- **可拖拽调整**：左侧栏宽度可在 20%-40% 之间调整（P1 功能）

## 与现有 Chat 窗口的关系

**两者共存，互为补充**：

| 特性 | Chat 窗口 | 助理控制台 |
|------|----------|-------------|
| **定位** | 快速对话 | 深度监控 |
| **形态** | 浮动窗口 | 全屏页面 |
| **会话列表** | 下拉菜单 | 左侧持久化列表 |
| **并行监控** | ❌ | ✅（一眼看到所有会话） |
| **实时状态** | 单个会话 | 所有会话 + 实时运行时长 |
| **适用场景** | 随时随地快速对话 | 需要监控多个任务 |

**数据共享**：
- 两者使用**完全相同的 API 和数据源**
- 在 Chat 窗口创建的会话，在助理控制台中可见（反之亦然）
- WebSocket 实时同步，无论在哪个界面操作，另一个界面自动更新

**导航**：
- 用户可以随时在 Chat 窗口和助理控制台之间切换
- 切换时保持当前活跃的会话

## 实现优先级（仅 P0 和 P1）

### P0（MVP - 核心功能）
- [ ] 左侧导航栏增加"助理"入口
- [ ] 创建助理页面路由（`/[slug]/assistant`）
- [ ] 双栏布局（左侧会话列表 + 右侧消息输出）
- [ ] 左侧会话列表（基础显示 + 点击切换）
  - [ ] Agent 头像 + 名称
  - [ ] 会话标题
  - [ ] 状态显示（Running / Completed / Failed / Idle）
  - [ ] **实时运行时长**（每秒更新）
- [ ] 右侧消息输出区域（直接复用 `<ChatMessageList>`）
- [ ] 输入区域（复用 `<ChatInput>`）
- [ ] 新建会话按钮
- [ ] 会话状态实时更新（通过 WebSocket）

### P1（核心体验优化）
- [ ] 会话操作
  - [ ] 重命名会话
  - [ ] 删除会话
  - [ ] 停止运行中的任务
- [ ] 未读标记
- [ ] 拖拽调整左侧栏宽度
- [ ] 会话列表空状态提示
- [ ] 错误处理和加载状态

**不在 MVP 范围内**（按优先级排除）：
- ❌ 会话搜索和过滤
- ❌ 会话分组显示
- ❌ 快捷键
- ❌ 主题和字体大小调整
- ❌ 导出会话内容
- ❌ 会话拖拽排序
- ❌ 移动端适配

## 开发计划

### 阶段 1：基础框架（1 周）
- 在左侧导航栏增加"助理"入口
- 创建新路由和页面组件（`packages/views/assistant/`）
- 搭建双栏布局（左 25% 会话列表 + 右 75% 消息区域）
- 复用现有 Chat API 和 Store
- 实现基础的会话列表显示

**交付物**：
- 可以点击左侧导航进入助理页面
- 左侧显示会话列表（静态数据）
- 右侧显示消息（复用 `<ChatMessageList>`）

### 阶段 2：核心交互（1 周）
- 实时运行时长显示组件（每秒更新）
- 会话状态实时更新（Running / Completed / Failed）
- 点击会话切换
- 新建会话
- 发送消息 / 停止任务
- WebSocket 事件处理

**交付物**：
- 完整的会话列表交互
- 实时状态更新和运行时长滚动
- 可以创建新会话、发送消息、停止任务

### 阶段 3：体验优化（1 周）
- 会话操作（重命名、删除、停止）
- 未读标记
- 拖拽调整左侧栏宽度
- 空状态和加载状态
- 错误处理
- UI 细节打磨

**交付物**：
- 完整的助理控制台功能
- 所有 P0 + P1 功能实现
- 可投入生产使用

**总计：约 3 周开发时间**

## 关键技术点

### 1. 实时运行时长的性能优化

**问题**：如果有 10 个正在运行的会话，每秒更新 10 次 DOM 可能影响性能。

**解决方案**：
- 使用 `React.memo` 包裹会话列表项
- 只更新运行中的会话，已完成的会话不重新渲染
- 使用 `requestAnimationFrame` 批量更新

```typescript
function SessionListItem({ session }: { session: ChatSession }) {
  const isRunning = session.status === 'running';
  const [elapsed, setElapsed] = useState(0);
  
  useEffect(() => {
    if (!isRunning) return;
    
    let frameId: number;
    const start = new Date(session.started_at).getTime();
    
    const update = () => {
      setElapsed(Math.floor((Date.now() - start) / 1000));
      frameId = requestAnimationFrame(update);
    };
    
    frameId = requestAnimationFrame(update);
    return () => cancelAnimationFrame(frameId);
  }, [isRunning, session.started_at]);
  
  return (
    <div>
      {/* ... */}
      {isRunning && <span>{formatDuration(elapsed)}</span>}
    </div>
  );
}

export default React.memo(SessionListItem);
```

### 2. 完全复用现有组件

**关键优势**：
- ✅ **零新增 API** - 所有后端接口已就绪
- ✅ **零新增渲染逻辑** - `<ChatMessageList>`、`<Markdown>`、`<CodeBlock>` 已完美支持
- ✅ **零 WebSocket 开发** - 实时更新机制已存在
- ✅ **只需新增布局和导航** - 核心工作量在 UI 组合，不是业务逻辑

**实现要点**：
```tsx
// packages/views/assistant/assistant-page.tsx
export function AssistantPage() {
  const wsId = useWorkspaceId();
  const activeSessionId = useChatStore(s => s.activeSessionId);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: messages = [] } = useQuery(chatMessagesOptions(activeSessionId));
  const { data: pendingTask } = useQuery(pendingChatTaskOptions(activeSessionId));
  
  return (
    <div className="flex h-screen">
      {/* 左侧会话列表 */}
      <SessionList sessions={sessions} />
      
      {/* 右侧消息区域 - 直接复用 */}
      <div className="flex-1 flex flex-col">
        <ChatMessageList
          messages={messages}
          pendingTask={pendingTask}
          availability={availability}
        />
        <ChatInput onSend={handleSend} /* ... */ />
      </div>
    </div>
  );
}
```

## 技术风险和注意事项

1. **性能**：长会话的消息列表可能很长，需要虚拟滚动优化
2. **WebSocket 同步**：确保多个视图（Chat 窗口 + Console 页面）同时打开时数据同步
3. **状态一致性**：避免 Chat 窗口和 Console 视图的状态分裂
4. **移动端适配**：移动端可能不需要这个功能（Mobile 已有独立的 Chat 界面）

## 参考设计

类似产品的终端界面：
- VSCode Integrated Terminal（左侧面板 + 多标签页）
- iTerm2 / Warp（会话列表 + 终端输出）
- GitHub Copilot Chat（但更强调终端视角）

---

**文档版本**：v1.0  
**创建日期**：2026-06-02  
**作者**：Product Team  
**状态**：待评审
