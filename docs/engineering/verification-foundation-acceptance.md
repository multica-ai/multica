# 自动化验证基础能力：原方案对齐与验收

本清单对应[原方案](automation-runtime-foundation.md)和[接口契约](verification-foundation-contracts.md)。代码、局部测试和干净 Mac 整机验收分别认定；没有原生执行证据时不宣布完整交付通过。

## 实现对应

| 原方案要求 | 实现位置 | 已形成的约束 |
| --- | --- | --- |
| 底层统一接口，MCP 薄接入 | `packages/verification/src/host`、`mcp` | 公用 schema、收据、任务和资源引用；20 个工具，无第二套业务状态 |
| 设备自检与准备 | `setup/inspect.ts`、`device-inspect.ts`、`prerequisites.ts` | 工具、磁盘、图形会话、端口/数据库目标、构建前提、现存 Desktop；人工系统前提及正常登录为 awaitingUser |
| 固定版本与构建复用 | `setup/source-identity.ts`、`build-cache.ts`、`environments.ts` | SHA/工作变更、锁文件/工具/构建环境缓存键、同键互斥、原子工件清单与校验；启动/报告前拒绝源码漂移 |
| 设备互斥与中断恢复 | `setup/device-lease.ts`、`environments.ts`、`read-only-web.ts` | 跨项目设备锁、环境 interrupted、实际分来源状态、保留的认证关闭身份、清理未确认时保留占用 |
| 真实数据与补充模板 | `data/service.ts`、`workspace.ts`、`manifest.ts`，Go `verificationworkspace`、`handler/verification_dev.go` | 选定会话/项目内容导出、关系和附件清单、事务导入、ID 映射、命名空间收据、按类型的创建清单和重置；跨出清单的新增关联拒绝清理；两个版本化模板 |
| 状态、事件和引用 | `sources/service.ts`、`event-page.ts` | 必需/可选来源、游标/generation/过滤绑定、开始/结束水位；恢复版本使旧游标和页面引用失效 |
| 条件目录和确定性检查 | `contracts/resources.ts`、`checks/service.ts` | 按环境给出独立条件 schema、来源和支持的时间语义；未知或缺失来源不作为成功事实 |
| 验证计划与 passed 门槛 | `contracts/index.ts`、`evidence/requirements.ts`、`store.ts` | 场景/范围、同一输入快照/provider、当前恢复版本、目标/边界检查、同一步骤截图；纯 UI 浅/深色要求 |
| 页面及外部 CU 协作 | `page/service.ts`、`read-only.ts`、`host/operations.ts` | Playwright 观察/有限动作/截图、动作意图与结果证据；只读页面网络约束；外部 CU 文件或内存图归档 |
| 证据与独立报告 | `evidence/store.ts` | 图片字节和 SHA、归属、固定报告 revision；环境/数据/步骤/任务索引，HTML 图片内嵌，bundle 包含快照和附件 |
| dev/生产边界 | Desktop 构建 alias、Go build tags、`scripts/verification/check-production.mjs` | 生产排除 main/preload/renderer 与 Go 验证实现；环境变量不能重新打开新增路由 |
| 原生贯穿验收 | `packages/verification/src/desktop.acceptance.ts` | 真实 Electron/bundled daemon、正常登录、页面发送/队列/导航边界、截图、导出与停止；记录耗时和有范围说明的资源采样 |

## 数据与运行边界

- `liveReadOnly` 页面要求 renderer 模式和 backend 来源。正常 Electron 认证/IPC 验证使用隔离副本。真实来源的 daemon/SQLite 仍可单独用于会话快照，不能把它伪装成 Web 页面来源。
- workspace v1 恢复状态、工作流、项目、事项、父子关系、标签、评论和附件；源账号凭据、人员执行身份及自动工作流执行不随内容克隆。执行 source context、自定义属性定义和项目执行资源明确报告不支持，不能用当前格式声称全账号复制。
- 完整 Desktop 使用设备级互斥；不接管或停止日常应用。外部 Computer Use 的配置及系统授权由其真实状态接口/系统流程认定，宿主不能推断或自授。
- 原生验收的资源结果注明采样间隔、已发现所属进程、状态目录磁盘及未计入范围；不把宿主自身内存当作整台设备内存。

## 实际执行证据

| 检查 | 结果 |
| --- | --- |
| 验证包 TypeScript、lint、构建 | 通过 |
| Desktop 类型、运行配置及生产构建 | 通过；18 项运行配置测试通过 |
| 基础测试 | 27 项通过，含缓存并发/篡改、设备互斥、源码身份、等待取消、游标作用域、报告要求与所属 Web 子进程关闭 |
| 协议/页面集成 | 9 项通过，使用真实 Go service/SQLite/Local API/WS、受控 provider 和 Chromium；不等同完整 Electron |
| 后端真实数据库导入 | 通过：请求复用/冲突、工作流不自动执行、父子/标签/评论映射、附件字节、受控重置；图关系和损坏附件在写入前拒绝 |
| 会话关闭并发 | Codex 迟到 turn/start 不修改已关闭 turn；新增回归与队列设置测试各 100 次 race 通过，service 全套 race 通过 |
| 生产新路由排除 | 生产构建扫描 622 个 JS 工件、4 组 Go 检查通过；开启验证环境变量时新增路由仍返回 404 |
| `test:desktop` 实际执行 | 返回 instanceBusy，保存 inconclusive；本机存在日常 Desktop，未竞争启动 |

数据库验证使用仓库 Compose 配置的独立测试项目和空闲端口，经托管迁移流程初始化。宿主 5432 属于另一套项目；没有替换或停止该服务，也未读取其业务数据。

## 正式交付仍需的实测

在专用干净 Mac 运行[准备流程](../../packages/verification/README.md#准备一台-mac)，再执行：

```sh
pnpm --filter @multica/verification test:desktop
```

完成测试应用的正常登录与工作区选择。以该次 `desktop-acceptance.json`、不可变报告及证据包判定实际状态；失败、缺来源、设备占用或缺必需截图不能生成通过。还需记录冷启动安装与构建、原生权限、完整 GUI 链路，以及开启/关闭诊断采集的资源和行为对照。当前设备的模块结果不能替代这些实测。
