# Bundled Platform Agent Runtime Design

## 1. 目标

Multica Desktop 将独立发布的 `platform-agent-cli` 作为内置 Runtime 二进制交付。客户端首次启动后，Multica Daemon 将其注册为独立的 `platform-agent-cli` Provider，安装向导通过现有 Runtime 选择器展示它，用户可直接选择并执行 Agent 任务。

本阶段使用已有 Mock `platform-agent-cli` 验证生产集成路径；CLI 内部的云端 Agent、Skill、Command 和 Extension/Squad 业务不在本设计范围内。

## 2. 非目标

- 不将 Platform Agent CLI 源码放入 Multica 仓库。
- 不将 Platform Agent CLI 二进制提交到 Multica Git 仓库。
- 不为该 Runtime 创建 Runtime Profile，不要求管理员手工配置。
- 不修改 Runtime Profile 数据库表、协议族白名单或管理界面。
- 不将 `platform-agent-cli` 注册成 `codex` Provider。
- 不复用 Codex 的 OpenAI 登录、模型发现、Codex Home、模型缓存、沙箱和自动升级策略。
- 不实现 MCP、普通工具执行或 Extension/Squad 转换。

## 3. 核心决策

### 3.1 交付边界

Platform Agent CLI 由独立项目构建和发布。Multica 只消费版本化的跨平台二进制产物，不依赖其源码。

### 3.2 产品入口

`platform-agent-cli` 是内置 Runtime，不是自定义 Runtime Profile。它由 Desktop 传递内置绝对路径，由 Daemon 通过正常的内置 Provider 探测和注册路径上报。安装向导只展示服务端返回的真实 Runtime，不增加不可执行的静态卡片。

### 3.3 协议边界

`platformAgentBackend` 复用 Codex app-server 的 JSON-RPC 传输和生命周期实现，但 Provider 身份始终是 `platform-agent-cli`。复用的是协议适配，不是 Codex 厂商策略。

## 4. 系统架构

```text
Platform Agent CLI 独立项目
  └─ 发布 darwin/windows/linux × amd64/arm64 二进制
                              ↓
Multica Desktop 构建阶段
  ├─ 选择当前目标平台和架构的唯一产物
  ├─ 校验 SHA256
  └─ 复制到 resources/bin/platform-agent-cli[.exe]
                              ↓
Multica Desktop 启动
  └─ 通过 MULTICA_PLATFORM_AGENT_CLI_PATH 传递内置绝对路径
                              ↓
Multica Daemon
  ├─ 执行 --version
  ├─ 注册 provider=platform-agent-cli
  └─ 将 Runtime 上报到用户工作区
                              ↓
安装向导 Runtime 选择器
  └─ 用户手工选择 Platform Agent CLI
                              ↓
Agent 任务
  └─ platformAgentBackend → app-server → Mock Runtime 结果
```

## 5. 多平台产物契约

### 5.1 产物集

独立 CLI 发布流水线产出：

```text
platform-agent-cli-artifacts/
├── platform-agent-cli_0.1.0_darwin_amd64
├── platform-agent-cli_0.1.0_darwin_arm64
├── platform-agent-cli_0.1.0_linux_amd64
├── platform-agent-cli_0.1.0_linux_arm64
├── platform-agent-cli_0.1.0_windows_amd64.exe
├── platform-agent-cli_0.1.0_windows_arm64.exe
└── checksums.txt
```

`checksums.txt` 使用标准 `sha256sum` 格式：

```text
<64 位小写十六进制 SHA256>  <产物文件名>
```

### 5.2 构建参数

- `PLATFORM_AGENT_CLI_VERSION`：要绑定的 CLI 版本，本阶段为 `0.1.0`。
- `PLATFORM_AGENT_CLI_ARTIFACT_DIR`：包含完整产物集和 `checksums.txt` 的绝对目录，用于 Desktop 打包。
- `PLATFORM_AGENT_CLI_DEV_BINARY`：当前开发系统上的 CLI 绝对路径，仅用于 Desktop 开发和本机验收。

### 5.3 选择规则

Desktop 打包矩阵每次只构建一个目标组合。打包脚本将 Electron 的平台和架构映射到 CLI 产物：

| Desktop 目标 | CLI 产物标识 |
| --- | --- |
| macOS x64 | `darwin_amd64` |
| macOS arm64 | `darwin_arm64` |
| Windows x64 | `windows_amd64` |
| Windows arm64 | `windows_arm64` |
| Linux x64 | `linux_amd64` |
| Linux arm64 | `linux_arm64` |

每个安装包只包含当前目标组合对应的一个 Platform Agent CLI 二进制。

### 5.4 失败策略

- `package.mjs` 构建发布安装包时，版本、产物目录、目标文件或校验值任一缺失即失败。
- SHA256 不匹配时失败，禁止继续打包。
- `pnpm dev:desktop` 未提供 `PLATFORM_AGENT_CLI_DEV_BINARY` 时可继续开发，但必须删除旧的开发复制产物，避免把过期文件误判为当前 Runtime。
- 制品库、GitHub Releases 或对象存储的下载步骤由外部 CLI 发布流水线完成；Multica 的稳定边界是已物化的本地产物目录。

### 5.5 现有打包脚本兼容

`apps/desktop/scripts/bundle-cli.mjs` 当前会删除整个 `resources/bin` 目录。新实现必须将其收窄为只替换 `multica[.exe]` 自身，否则会在同一构建中误删 `platform-agent-cli[.exe]`。Platform Agent CLI 使用独立、可单测的复制脚本。

## 6. Desktop 路径与 Daemon 注册

### 6.1 路径解析

Desktop 主进程使用与内置 `multica` CLI 一致的规则解析 Platform Agent CLI：

- 开发：`<desktop-app-path>/resources/bin/platform-agent-cli[.exe]`。
- 安装：`<app.asar.unpacked>/resources/bin/platform-agent-cli[.exe]`。

Desktop 只在文件存在时向 Daemon 子进程注入 `MULTICA_PLATFORM_AGENT_CLI_PATH`。Desktop 管理的内置路径覆盖 Desktop 进程继承的同名环境变量，避免安装包误启动 PATH 或用户环境中的其他版本。

### 6.2 Daemon 探测

Daemon 新增内置 Provider 探测契约：

```text
env:      MULTICA_PLATFORM_AGENT_CLI_PATH
command:  platform-agent-cli
provider: platform-agent-cli
model:    无 Multica 侧覆盖
```

对于 Desktop 启动的 Daemon，环境变量总是指向内置产物。对于独立运行的 `multica daemon`，运维或开发者可显式设置该环境变量；未设置时仍可按现有探测规则解析同名命令。

### 6.3 Runtime 注册数据

Daemon 通过 `--version` 校验 CLI 后上报：

```json
{
  "name": "Platform Agent CLI",
  "type": "platform-agent-cli",
  "version": "0.1.0",
  "status": "online"
}
```

最低可用版本为 `0.1.0`。显示名称为 `Platform Agent CLI`，启动预览为 `platform-agent-cli app-server`。

## 7. Backend 执行设计

### 7.1 独立类型

`agent.New("platform-agent-cli", cfg)` 返回独立的 `platformAgentBackend`。该 Backend 委托给共享的 app-server 协议实现，而不是要求调用者伪装成 `codex`。

### 7.2 生命周期

执行路径覆盖：

1. 启动 `platform-agent-cli app-server --listen stdio://`。
2. 发送 `initialize`。
3. 创建或恢复 Thread。
4. 发送 Turn 和用户任务文本。
5. 转换流式文本、状态和结果事件。
6. 返回最终状态、输出和 Session ID。
7. 响应超时和上下文取消。

### 7.3 能力隔离

- Multica 对该 Provider 声明模型选择不可用，由 Platform Agent Runtime 的动态配置决定模型。
- Backend 不消费 Codex 的 `ServiceTier`、`ThinkingLevel`、Codex 模型发现或 Codex 专用参数。
- 本阶段不向 Platform Agent CLI 注入 MCP 配置。
- Provider 特有的 OpenAI/Codex 环境准备、恢复存储校验、沙箱和 Shell 策略仍只对 `provider == "codex"` 生效。

## 8. 安装向导与 UI

现有安装向导通过 Runtime API 加载 Daemon 已注册的 Runtime。新 Provider 注册成功后自然进入该列表，无需改造数据流或新增管理页。

UI 只增加与 Provider 身份相关的展示和能力处理：

- `Platform Agent CLI` 显示名称和 Provider Logo。
- Runtime 选择器可选中真实 `runtime_id`。
- 模型选择器显示为由 Runtime 管理，不发送模型覆盖。
- 不自动把该 Runtime 绑定到 Agent；最终选择仍由用户在现有流程中完成。

## 9. 错误处理

| 失败 | 行为 |
| --- | --- |
| 内置文件不存在 | Desktop 不注入路径；Daemon 不注册该 Runtime |
| 文件不可执行 | `--version` 探测失败，健康信息记录原因 |
| 版本低于 `0.1.0` | 拒绝注册并记录最低版本错误 |
| app-server 握手失败 | 任务失败，保留 `platform-agent-cli` Provider 身份和原始错误 |
| 流式事件非法 | Backend 按 app-server 协议错误收束，不伪造成功输出 |
| 任务取消或超时 | 终止 CLI 进程树并返回统一状态 |
| Runtime 启动失败 | 不回退到 `codex` 或其他 Provider |

## 10. 数据与 API 影响

`agent_runtime.provider` 和前端 `RuntimeDevice.provider` 均以字符串承载内置 Provider。新类型通过现有 Daemon Register API 上报，不需要数据库迁移或新 API。Runtime Profile 的 `protocol_family` 白名单保持不变，因为本设计不允许通过 Runtime Profile 创建 Platform Agent Runtime。

## 11. 测试设计

### 11.1 Desktop 打包测试

- 六个平台/架构组合映射到正确产物名。
- 每次只复制当前目标产物。
- 正确 SHA256 通过，缺失或错误 SHA256 失败。
- 旧 Platform Agent CLI 产物不会在未配置的开发启动中残留。
- 现有 `multica` CLI 构建不会删除 Platform Agent CLI。

### 11.2 Desktop 主进程测试

- 开发和安装环境路径解析正确。
- Windows 使用 `.exe`，macOS/Linux 使用无后缀名称。
- 文件存在时 `desktopSpawnEnv` 注入绝对路径。
- 文件缺失时不注入伪路径。
- Desktop 内置路径覆盖继承的同名环境变量。

### 11.3 Go 单元测试

- Daemon 能通过 `MULTICA_PLATFORM_AGENT_CLI_PATH` 探测新 Provider。
- `agent.New("platform-agent-cli", cfg)` 返回可执行 Backend。
- 显示名称、启动预览、最低版本和模型能力与设计一致。
- 默认测试不解析或启动用户机器上的真实 CLI。

### 11.4 真实协议测试

`agentintegration` 测试使用 `MULTICA_PLATFORM_AGENT_CLI_PATH` 和 `MULTICA_RUN_REAL_AGENT_SMOKE=1`，通过 `New("platform-agent-cli", cfg)` 完成 initialize、Thread、Turn、流式文本、最终结果和 Session ID 验证。

### 11.5 UI 与本机验收

- Runtime 选择器能显示并选中 `provider=platform-agent-cli` 的 Runtime。
- 模型选择显示为 Runtime 管理。
- 当前开发系统使用 `PLATFORM_AGENT_CLI_DEV_BINARY` 生成一个本地 Desktop 产物。
- 启动真实 Desktop 和 Daemon，确认 Runtime 注册为在线。
- 选择该 Runtime 后执行一条任务，确认 Mock CLI 返回预期结果。

## 12. 实施边界

本次实施完成当前开发系统的真实 Desktop 打包、Daemon 注册、UI 选择和任务执行验收。其他平台的 Multica 适配和产物选择逻辑需要自动化测试，但不伪造或提交外部 CLI 二进制。对应平台的实际安装包只能在外部 CLI 流水线提供该平台产物后发布。

## 13. 验收标准

1. Multica Git 差异不包含 Platform Agent CLI 源码或二进制。
2. 当前开发系统的 Desktop 资源只包含该系统对应的 Platform Agent CLI。
3. Daemon 上报的 Provider 精确为 `platform-agent-cli`。
4. 安装向导可显示并选择真实 Runtime。
5. 任务通过独立 Backend 完成 app-server 协议生命周期。
6. 失败不回退为 `codex`。
7. 新增单元测试、真实集成测试和相关现有回归测试全部通过。
