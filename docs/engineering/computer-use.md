# 本机会话的 Computer Use

本地 Codex 和 Claude 会话通过 Multica 内置 MCP 使用持久 JavaScript REPL，原生操作由执行设备的桌面主进程授权，再交给 macOS Helper 执行。

当前支持 macOS 13 及以上的前台操作。Windows、Linux、锁屏继续及后台输入不在此版本范围内。系统权限必须由用户授予；本实现不依赖 Codex/ChatGPT 安装包、私有 CUA SDK 或额外模型服务。

## 开发和使用

在仓库根目录安装依赖，然后使用仓库的开发环境命令启动所需服务。桌面启动脚本会编译原生 Helper：

```sh
pnpm install --frozen-lockfile
pnpm --filter @multica/desktop dev
```

在本地会话中明确要求 Computer Use，并指定目标应用。模型先调用 `computer_status`，再调用 `computer_execute`。首次执行会请求用户设置屏幕录制和辅助功能权限；在系统设置中为 macOS 显示的 **Multica** 或 **Multica Computer Use** 项目授权。授予权限后再发起一次执行。每轮首次访问应用都会显示原生授权框，显示应用名称、标识和会话 ID。

操作期间菜单栏显示 `CU`，菜单包含当前应用、会话及停止操作。快捷键注册成功时，也可按 `Control+Option+Escape` 停止。原生权限弹窗与授权按钮不能由模型自行确认。

以下代码运行在 `computer_execute` 中，按不同调用执行，不能作为普通终端脚本直接运行：

```javascript
let app = await computer.getApp("com.apple.calculator");
let state = await app.observe();
```

检查返回的截图和 `state.elements`，选择真实控件引用后，再调用 `app.click(ref)`。每个动作都会消费当前观察，下一动作之前必须重新 `await app.observe()`。

## 分层与通信

| 层 | 实现 | 职责 |
| --- | --- | --- |
| 会话工具 | `server/internal/localconversation/service/computer_tools.go` | `computer_status`、`computer_execute`、`computer_reset`；绑定账号、工作区、会话与轮次 |
| REPL 进程管理 | `server/internal/localconversation/computer/` | 每个会话独立进程、超时、终止、原生动作记录 |
| JavaScript 运行时和 SDK | `packages/computer-use/` | Node REPL、顶层 await、变量和应用对象；不持有宿主授权凭据 |
| 主进程通道 | `server/internal/daemon/localhost/computer_host.go` 与 Desktop `computer-use/channel.ts` | 执行设备上的主进程直连 daemon，不经渲染进程或远端查看端 |
| 桌面 Broker | `apps/desktop/src/main/computer-use/` | 每轮应用授权、设备输入互斥、权限请求、停止和原生 Helper 生命周期 |
| 原生执行 | `apps/desktop/native/computer-use/ComputerUse.swift` | ScreenCaptureKit 窗口截图、辅助功能结构、语义控件动作、前台键鼠操作 |

模型经 MCP 把代码交给 Go。Go 使用 stdin/stdout 向独立 Node REPL 发送 `execute`。SDK 发出结构化 `action`；Go 补齐宿主身份，经 `/local/v2/computer-host` 交给 Electron Broker。原生结果沿相同链路返回，最新截图作为 MCP image 返回模型。

`/computer-host` 仅接受本机主进程 token，不接受渲染进程凭据、WebSocket 票据或设备互联调用。每个 daemon 仅连接一个 Computer Host；连接断开后不自动重放动作，正在执行的 JavaScript 会终止。远端查看会话不会把控制请求转给查看端的电脑。

## 模型接口

| 工具 | 参数 | 返回 |
| --- | --- | --- |
| `computer_status` | 无 | 平台、原生能力及系统权限状态；不读取屏幕、不发起权限请求 |
| `computer_execute` | `code`、`request_id`，可选 `timeout_seconds` | 完成／失败、文本输出、值摘要、动作数量、动作记录和最新截图 |
| `computer_reset` | `request_id` | 清空此会话 REPL 并等待原生控制释放；不撤销已执行动作 |

`request_id` 在当前运行器进程内用于相同请求去重。同一 ID 携带不同参数会拒绝。新操作使用新 ID；进程重启后不保证跨进程去重，也不会自动恢复或重跑代码。

SDK 提供：

| 接口 | 用法 |
| --- | --- |
| `computer.getApp(nameOrBundleId)` | 获取、授权并在必要时启动已安装应用 |
| `app.windows()` / `app.selectWindow(id)` | 列出已授权应用的窗口，使用实际返回的 ID 选择目标；选择后重新观察 |
| `app.observe()` | 返回窗口、带引用的控件、观察 ID，同时输出截图 |
| `app.click(ref)` / `app.click({x,y})` | 控件引用点击或窗口内坐标点击 |
| `app.setValue(ref,value)` | 设置支持该辅助功能动作的控件值 |
| `app.typeText(text)` / `app.pressKey(key)` | 当前目标窗口中的 Unicode 输入、快捷键 |
| `app.scroll({x,y,dy})` | 垂直滚动 |
| `app.drag({x,y,toX,toY})` | 窗口内拖拽 |

坐标使用窗口内逻辑点。截图可能缩小，不能把截图像素直接当作坐标。控件引用仅属于该应用的最近一次观察；窗口变化、动作执行、Helper 重启或不同应用的引用都不能直接复用。

## 权限与生命周期

- REPL 进程由 Go 管理，使用安装包的 Electron Node 模式，不运行在 Electron 主进程。macOS 使用 `sandbox-exec` 限制文件读取、写入、网络与子进程；启动失败即拒绝执行，没有放宽权限的回退。
- 只允许读取系统库和本安装的运行时代码。系统加载器需要根目录及文件元信息；不因此开放用户文件内容。环境变量经过清空，不继承账号凭据。V8 heap 上限为 128 MiB，这不等同于总 RSS 上限。
- Node REPL 自身不是安全沙箱。它使用打包 Node 的 REPL 行为，并对同步异常和顶层 await 做回归测试；升级 Electron 时必须运行实际构建产物的沙箱测试。
- 一个设备同时只允许一个会话轮次控制原生输入。应用授权由真实桌面弹窗产生，模型不能传入 `approved`。授权不跨轮次继承。
- 同一运行器进程中的 REPL 可跨轮次保留变量；下一轮仍需重新 `getApp` 获取授权。重置、取消、超时、断连、运行器关闭和 daemon 关闭会使变量丢失。分叉不共享 REPL。
- 停止菜单／快捷键同时撤销原生许可，并通知 Go 终止当前 REPL。该轮不能自动重新获取控制。账号切换与桌面退出也会释放控制。
- 已发送的系统输入不能撤销。原生调用或连接中断后可能返回 `outcome_unknown`，应检查界面与动作记录，不能直接重复整段代码。
- Multica、自身 Helper、已列出的终端、系统权限和认证应用被执行端拒绝。安全文本控件不返回值，也不接受对应控件操作或焦点键盘输入。应用内容仍是未可信数据，敏感操作需人工接管；此限制不代表通用内容识别或完整的应用风险分类。
- 网页操作优先推荐 Side 浏览器与 `side_browser_*`；也可按用户要求或任务需要使用外部浏览器、浏览器自动化或 Computer Use。各工具仍遵守自身的权限与确认规则。

执行默认最多 60 秒，可设置为 1–120 秒；每次代码执行最多发出 60 个 SDK 动作。原生调用最多 15 秒。观察最多保留 300 个控件，控件 JSON 不超过 120 KiB；JPEG 不超过 500 KiB，因此符合现有 1 MiB WebSocket 消息预算。只有最新截图返回模型，操作目标及成功／失败记录由 Go 生成，避免回显输入内容。工具的 `value` 是有界摘要；需要更多控件时，可在下一次调用中读取 `state.elements.slice(...)` 或使用筛选。截图像素尺寸在 `screenshotSize` 中返回。

## 构建和验证

`scripts/build-computer-use.mjs` 编译和签名生成的 Helper app，产物位于忽略的 `apps/desktop/resources/native/`。开发、构建和每个发行目标都会调用它。非 macOS 打包目标移除该生成产物。macOS arm64 和 x64 分别编译，发行仍需执行现有 Developer ID 签名和公证流程。

`computer-host.cjs` 是独立构建入口；打包后由 `resources/local-conversation-host/computer-host.cjs` 加载 app.asar 内的运行时。它不导入 Electron 或桌面主进程。

常规检查不运行模型或操作真实桌面：

```sh
pnpm --filter @multica/computer-use test
pnpm --filter @multica/desktop test src/main/computer-use/broker.test.ts src/main/local-v2-host.test.ts
pnpm --filter @multica/desktop typecheck
node apps/desktop/scripts/build-computer-use.mjs --target-arch arm64
node apps/desktop/scripts/build-computer-use.mjs --target-arch x64
(cd server && go test -race ./internal/localconversation/computer ./internal/localconversation/service ./internal/daemon/localhost)
git diff --check
```

在 `apps/desktop` 执行 `pnpm exec electron-vite build` 后，用构建所用的 Electron 和绝对入口路径运行 `TestBuiltRuntimeSandbox`：设置 `MULTICA_TEST_COMPUTER_EXECUTABLE` 和 `MULTICA_TEST_COMPUTER_ENTRY`，然后在 `server` 执行 `go test ./internal/localconversation/computer -run TestBuiltRuntimeSandbox -count=1 -v`。该测试运行真实沙箱，验证持久变量、文件内容读取、写入、网络和子进程限制，不调用模型或桌面操作。

人工验收需在独立桌面测试环境中完成系统授权、窗口截图、中文输入、弹窗、Retina／多显示器坐标和停止操作，并验证签名安装包中的 TCC 身份。编译与假的原生服务测试不替代这些验收。
