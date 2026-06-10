# Multica Agent Runtime 配置机制全景图

## 总体发现

multica 项目中 agent 运行时的配置机制分为三层：
1. 前端 UI 配置 - 用户在 agent detail 页面配置
2. 数据库存储 - 配置持久化到 PostgreSQL  
3. Daemon 执行 - Daemon 在运行 task 时读取并应用配置

---

## 1. Agent 端的 Runtime 配置覆盖

### 1.1 前端 UI 可配置项

路径：packages/views/agents/components/tabs/

| Tab | 字段 | 类型 | 说明 |
|-----|------|------|------|
| custom-args-tab.tsx | custom_args | string[] | CLI 参数（以空格分割展开为数组） |
| env-tab.tsx | custom_env | Record<string,string> | 环境变量（仅所有者/管理员可见） |
| mcp-config-tab.tsx | mcp_config | json 对象 | MCP 服务器配置 |
| model-picker.tsx | model | string | 模型 ID |
| thinking-picker.tsx | thinking_level | string | 推理等级 |

### 1.2 前端类型定义

路径：packages/core/types/agent.ts

Agent 接口（行 210-282）：
- custom_args: string[] (行 220)
- has_custom_env?: boolean (行 232)
- custom_env_key_count?: number (行 237)
- mcp_config?: unknown | null (行 252)
- mcp_config_redacted?: boolean (行 260)
- model: string (行 264)
- thinking_level?: string (行 275)

UpdateAgentRequest 接口（行 386-425）：
- custom_args?: string[] (行 402)
- mcp_config?: unknown | null (行 411)
- model?: string (行 415)
- thinking_level?: string (行 424)

### 1.3 API 端点

路径：packages/core/api/client.ts

| 端点 | 方法 | 用途 |
|------|------|------|
| PUT /api/agents/{id} | updateAgent() | 更新 agent（行 802） |
| GET /api/agents/{id}/env | getAgentEnv() | 获取 custom_env 值（行 819） |
| PUT /api/agents/{id}/env | updateAgentEnv() | 更新 custom_env（行 831） |

---

## 2. 数据库存储

### 2.1 Agent 表结构

表：agent

| 列 | 类型 | 说明 |
|---|------|------|
| custom_args | JSONB | JSON 数组 |
| custom_env | JSONB | 环境变量 k-v 对 |
| mcp_config | JSONB | MCP 配置 |
| model | TEXT | 模型 ID（nullable） |
| thinking_level | TEXT | 推理等级（nullable） |

### 2.2 服务端数据流

路径：server/internal/handler/agent.go

- 行 44-45：Agent 响应包含 custom_args 和 mcp_config
- 行 96-103：从 DB unmarshal custom_args
- 行 110-135：构建 AgentResponse 转换所有字段

---

## 3. Daemon 执行时的配置应用

### 3.1 Task 数据结构

路径：server/internal/daemon/types.go 行 196-206

AgentData 结构：
- CustomEnv: map[string]string (行 201)
- CustomArgs: []string (行 202)
- McpConfig: json.RawMessage (行 203)
- Model: string (行 204)
- ThinkingLevel: string (行 205)

### 3.2 Daemon 运行流程

路径：server/internal/daemon/daemon.go

第 1 步（行 2713）：runTask() 进入点
第 2 步（行 2740-2823）：读取 Agent 配置及 McpConfig
第 3 步（行 3022-3076）：
  - 读取 task.Agent.CustomArgs 和 McpConfig（行 3022-3027）
  - 解析 task.Agent.Model（行 3039-3043）
  - 验证 thinking_level（行 3045-3076）
  - 调用 agent.ValidateThinkingLevel() 验证三元组
第 4 步（行 3077-3088）：构建 agent.ExecOptions
第 5 步（行 3114+）：调用 backend.Execute()

### 3.3 自定义参数处理

路径：server/pkg/agent/openclaw.go

customArgs := filterCustomArgs(opts.CustomArgs, blockedArgs, logger)
args = append(args, customArgs...)

阻止列表来自 runtime.json 的 command.blocked_args

---

## 4. 模型列表动态获取

### 4.1 当前实现

路径：server/internal/daemon/daemon.go 行 1489-1568

func handleModelList():
- 外部 runtime：使用 manifest 中的静态列表（行 1504-1506）
- 内置 runtime：通过 CLI 动态发现（行 1509）

### 4.2 内置 Runtime 发现机制

路径：server/pkg/agent/models.go

Claude/Codex（行 96-103）：
- 返回静态目录 + CLI thinking-level 发现

Pi/OpenCode（行 134-139）：
- 运行 CLI 命令（`opencode models --verbose`、`pi --list-models`）
- 15 秒超时，缓存 60 秒

ACP 提供商（行 688-757）：
- Hermes、Kimi、Kiro、Copilot
- 启动 ACP 进程 → initialize + session/new
- 解析 models.availableModels

本地（行 995-1067）：
- Antigravity：agy models
- Cursor：cursor-agent --list-models

### 4.3 外部 Runtime 局限

问题：runtime.json 中 models 是静态数组

缺失设计：外部 runtime 需要 CLI 模型发现的声明机制

---

## 5. 定价获取

### 5.1 前端分层架构

路径：packages/core/runtimes/

第 1 层：内置 MODEL_PRICING（硬编码）
第 2 层：CustomPricingStore（Zustand 用户覆盖）
第 3 层：ManifestPricingStore（runtime.json pricing）

### 5.2 Manifest 定价结构

路径：packages/core/types/agent.ts 行 80-85

RuntimeModelPricing：
- input?: number (USD per million tokens)
- output?: number
- cacheRead?: number
- cacheWrite?: number

RuntimeDevice.pricing: Record<string, RuntimeModelPricing> (行 45)

### 5.3 定价数据流

1. Daemon 注册时读取 runtime.json 的 pricing
2. Server 存储在 runtime_device 或独立表
3. GET /api/runtimes 返回 pricing 对象
4. 前端 useManifestPricingStore 镜像到 Zustand

### 5.4 缺失部分

- 无动态定价发现（CLI 查询）
- 无 API 端点同时返回模型 + 定价
- 成本计算完全在客户端

---

## 关键文件路径

| 用途 | 文件路径 | 行号 |
|------|---------|------|
| Agent 类型 | packages/core/types/agent.ts | 210-282, 386-425 |
| Custom Args UI | packages/views/agents/components/tabs/custom-args-tab.tsx | 30-154 |
| 环境变量 UI | packages/views/agents/components/tabs/env-tab.tsx | 55-306 |
| MCP 配置 UI | packages/views/agents/components/tabs/mcp-config-tab.tsx | 21-187 |
| API 客户端 | packages/core/api/client.ts | 802-836 |
| 后端响应 | server/internal/handler/agent.go | 34-136 |
| Task 数据 | server/internal/daemon/types.go | 113-206 |
| Daemon | server/internal/daemon/daemon.go | 2713-3120 |
| 模型发现 | server/pkg/agent/models.go | 81-1468 |
| 模型处理 | server/internal/daemon/daemon.go | 1489-1616 |
| 定价存储 | packages/core/runtimes/manifest-pricing-store.ts | 1-58 |

---

## 建议后续行动

短期：为外部 runtime 添加 CLI 模型发现配置
中期：在 runtime.json 声明定价发现方式
长期：设计 /api/daemon/runtime/{id}/models 统一端点

