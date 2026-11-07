# 本机会话的 Computer Use

本地 Codex 和 Claude 会话通过 Multica 内置 MCP `multica_desktop_context` 使用持久 JavaScript REPL；Multica 普通智能体的任务运行在同一台 Mac 上时，默认注入 `multica_computer` MCP 使用同一套能力。原生操作由执行设备的桌面主进程授权，再交给内嵌开源 [cua-driver](https://github.com/trycua/cua)（MIT）的原生子进程执行。

当前支持 macOS 13 及以上。动作在后台投递给目标应用，不切换用户的前台应用、键盘焦点和指针。Windows、Linux 和锁屏继续不在此版本范围内。系统权限必须由用户授予；本实现不依赖 Codex/ChatGPT 安装包或额外模型服务。cua-driver 依赖 macOS 私有接口，升级 macOS 或 cua-driver 时必须重新人工验收。

Desktop 与 daemon 生命周期解耦：Computer Use 的 SDK、提示词、REPL 运行时和授权都由 Desktop 在连接时上报，daemon 按上报内容提供能力。通道协议、能力版本和账号认证见 [Computer Host 协议](computer-host-protocol.md)。

## 开发和使用

在仓库根目录安装依赖，然后使用仓库的开发环境命令启动所需服务。桌面启动脚本会暂存 cua-driver SDK：

```sh
pnpm install --frozen-lockfile
pnpm --filter @multica/desktop dev
```

在本地会话中明确要求 Computer Use，并指定目标应用。模型先调用 `computer_status`，再调用 `computer_execute`。首次执行会请求用户设置屏幕录制和辅助功能权限；在系统设置中为 macOS 显示的 **Multica** 授权；开发构建显示为开发应用的名称。授予权限后再发起一次执行。每轮首次访问应用都会显示原生授权框，显示应用名称、标识和会话 ID。

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
| REPL 进程管理 | `server/internal/localconversation/computer/` | 每个会话独立进程，按 Desktop 上报的运行时启动，能力版本不兼容时重建；超时、终止、原生动作记录 |
| JavaScript 运行时、SDK 与提示词 | `packages/computer-use/` | Node REPL、顶层 await、变量和应用对象；不持有宿主授权凭据。`prompts.mjs` 是面向模型的说明，`contract/` 是 Computer 能力契约与版本锁 |
| 主进程通道 | `server/internal/daemon/localhost/computer_host.go` 与 Desktop `computer-use/channel.ts` | Computer Host 协议（JSON-RPC 2.0）：Desktop 以 `initialize` 上报能力、提示词、REPL 运行时和账号；执行设备上的主进程直连 daemon，不经渲染进程或远端查看端 |
| 桌面 Broker | `apps/desktop/src/main/computer-use/` | 每轮应用授权、设备输入互斥、权限请求、停止和原生进程生命周期 |
| 原生执行 | `apps/desktop/src/computer-native/` | 以 `ELECTRON_RUN_AS_NODE=1` 运行的主进程子进程，内嵌 cua-driver SDK；把 Broker 动作转换为 cua-driver 工具：窗口截图、辅助功能结构、后台控件动作与键鼠投递 |

模型经 MCP 把代码交给 Go。Go 使用 stdin/stdout 向独立 Node REPL 发送 `execute`。SDK 发出结构化 `action`；Go 补齐宿主身份，经 `/local/v2/computer-host` 交给 Electron Broker。原生结果沿相同链路返回，最新截图作为 MCP image 返回模型。

`/computer-host` 仅接受本机主进程 token，不接受渲染进程凭据、WebSocket 票据或设备互联调用。每个 daemon 仅连接一个 Computer Host；连接断开后不自动重放动作，正在执行的 JavaScript 会终止。远端查看会话不会把控制请求转给查看端的电脑。

## 普通智能体的任务运行

以下条件同时满足时，`daemon/computer_mcp.go` 为任务绑定一个 `multica_computer` MCP，与插件 Hook MCP、远程 MCP 代理合并进任务的 MCP 配置，并在任务提示词末尾追加 Desktop 提供的 `taskBrief`：daemon 运行在 Multica Desktop 中；Desktop 已完成 Computer Host 握手并上报兼容的 Computer 能力；Desktop 登录的账号等于 daemon 账号；任务有 Agent，且 provider 支持任务本地 HTTP MCP（codex、claude、hermes、qoder、mcode）。任一条件不满足时不注入；绑定失败只记日志，不使任务失败。

- 路由复用 `contextmcp` 本地服务与本地会话的三个工具，令牌放在 URL 路径中，与 daemon 其他任务本地 MCP 一致；任务结束时解绑路由、清空 REPL 并释放桌面控制。
- 控制身份为 `conversationId = task:<任务 ID>`、`turnId = <任务 ID>`，信封带 `origin: "task"`、工作区和 daemon 账号。daemon 账号在启动时由 `/api/me` 获取，未知时任务不注入。Broker 对 AI 会话和任务一样要求信封账号等于 Desktop 当前账号；账号会话代次变化时撤销控制。
- 任务运行的应用访问**自动授权**，不弹窗；设备互斥、`CU` 菜单与快捷键停止、禁用应用和宿主进程排除规则与本地会话相同。任务内容（issue、评论、网页）属于未可信数据，自动授权意味着没有逐应用的人工关卡。
- 工具说明以 MCP `instructions` 下发，内容是 Desktop 上报的 `prompts.task`；AI 会话的系统提示词使用 `prompts.conversation`。两者只在授权方式一句上不同。

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

未选择窗口时，使用该应用在当前桌面空间中最前的可见窗口。其他桌面空间中的窗口无法解析辅助功能结构，观察结果为空且动作被拒绝。

坐标使用窗口内逻辑点，原生进程按观察时的截图比例换算为 cua-driver 的截图像素。截图可能缩小，不能把截图像素直接当作坐标。控件引用仅属于该应用的最近一次观察；窗口变化、动作执行、原生进程重启或不同应用的引用都不能直接复用。

动作只走后台投递。后台无法投递的动作，例如 Electron 窗口内滚动、在后台打开网页 `<select>`，返回 `background_unavailable` 等拒绝码，不会自动切到前台。成功结果带有 cua-driver 的 `effect`：`confirmed` 表示有读回证据，`unverifiable` 表示已投递但无法确认，仍需重新观察。

## 权限与生命周期

- REPL 进程由 Go 管理，使用安装包的 Electron Node 模式，不运行在 Electron 主进程。macOS 使用 `sandbox-exec` 限制文件读取、写入、网络与子进程；启动失败即拒绝执行，没有放宽权限的回退。
- 只允许读取系统库和本安装的运行时代码。系统加载器需要根目录及文件元信息；不因此开放用户文件内容。环境变量经过清空，不继承账号凭据。V8 heap 上限为 128 MiB，这不等同于总 RSS 上限。
- Node REPL 自身不是安全沙箱。它使用打包 Node 的 REPL 行为，并对同步异常和顶层 await 做回归测试；升级 Electron 时必须运行实际构建产物的沙箱测试。
- 一个设备同时只允许一个会话轮次或任务运行控制原生输入。本地会话的应用授权由真实桌面弹窗产生，模型不能传入 `approved`；任务运行自动授权，见上文。授权不跨轮次继承。
- 同一运行器进程中的 REPL 可跨轮次保留变量；下一轮仍需重新 `getApp` 获取授权。重置、取消、超时、断连、运行器关闭和 daemon 关闭会使变量丢失。分叉不共享 REPL。
- 停止菜单／快捷键同时撤销原生许可，并通知 Go 终止当前 REPL。该轮不能自动重新获取控制。账号切换与桌面退出也会释放控制。
- 已发送的系统输入不能撤销。原生调用或连接中断后可能返回 `outcome_unknown`，应检查界面与动作记录，不能直接重复整段代码。
- 托管本 Broker 的 Multica 进程（原生进程的父进程）的窗口不会列出、观察或操作，因此模型无法点击该进程的授权弹窗；其他 Multica 实例（如验证环境、另一份安装）可以操作。已列出的终端、系统设置和认证应用被执行端拒绝。cua-driver 不提供控件的 subrole，执行端无法识别安全文本控件：macOS 本身不通过辅助功能返回密码框的值，但向密码框设置值或输入文字不会被拦截。密码等敏感输入必须由用户接管。应用内容仍是未可信数据；此限制不代表通用内容识别或完整的应用风险分类。
- 原生进程以 embedded 模式加载 cua-driver，不连接用户自行安装的 CuaDriver.app，也不显示 cua-driver 的代理光标；操作状态由菜单栏 `CU` 提示。
- 网页操作优先推荐 Side 浏览器与 `side_browser_*`；也可按用户要求或任务需要使用外部浏览器、浏览器自动化或 Computer Use。各工具仍遵守自身的权限与确认规则。

执行默认最多 60 秒，可设置为 1–120 秒；每次代码执行最多发出 60 个 SDK 动作。原生调用最多 15 秒。观察最多保留 300 个控件，控件 JSON 不超过 120 KiB；JPEG 不超过 500 KiB，因此符合现有 1 MiB WebSocket 消息预算。只有最新截图返回模型，操作目标及成功／失败记录由 Go 生成，避免回显输入内容。工具的 `value` 是有界摘要；需要更多控件时，可在下一次调用中读取 `state.elements.slice(...)` 或使用筛选。截图像素尺寸在 `screenshotSize` 中返回。

## 构建和验证

`scripts/stage-cua-driver.mjs` 把目标架构的 cua-driver SDK 及其原生库复制到忽略的 `apps/desktop/resources/native/cua-driver/`；打包时作为 extraResources 放到 `Contents/Resources/native/cua-driver/`，不进入 app.asar，因为 SDK 按真实路径加载原生库。开发、构建和每个发行目标都会调用它。pnpm 只安装本机架构的原生包，其他架构用 `npm pack` 下载，并按 `pnpm-lock.yaml` 的 integrity 校验。非 macOS 打包目标移除该目录。原生库随应用一起执行现有 Developer ID 签名和公证流程。

`computer-host.cjs` 是独立构建入口；打包后由 `resources/local-conversation-host/computer-host.cjs` 加载 app.asar 内的运行时。`computer-native.js` 由桌面主进程直接启动，并传入暂存 SDK 的路径。两者都不导入 Electron 或桌面主进程。

常规检查不运行模型或操作真实桌面：

```sh
pnpm --filter @multica/computer-use test
pnpm --filter @multica/desktop test src/main/computer-use
pnpm --filter @multica/desktop test src/computer-native src/main/computer-use/broker.test.ts src/main/local-v2-host.test.ts
pnpm --filter @multica/desktop typecheck
node apps/desktop/scripts/stage-cua-driver.mjs --target-arch x64
node apps/desktop/scripts/stage-cua-driver.mjs --target-arch arm64
(cd server && go test -race ./internal/localconversation/computer ./internal/localconversation/service ./internal/daemon/localhost)
git diff --check
```

在 `apps/desktop` 执行 `pnpm exec electron-vite build` 后，用构建所用的 Electron 和绝对入口路径运行 `TestBuiltRuntimeSandbox`：设置 `MULTICA_TEST_COMPUTER_EXECUTABLE` 和 `MULTICA_TEST_COMPUTER_ENTRY`，然后在 `server` 执行 `go test ./internal/localconversation/computer -run TestBuiltRuntimeSandbox -count=1 -v`。该测试运行真实沙箱，验证持久变量、文件内容读取、写入、网络和子进程限制，不调用模型或桌面操作。

人工验收需在独立桌面测试环境中完成系统授权、窗口截图、中文输入、弹窗与原生文件选择框、Retina／多显示器坐标、前台应用与指针不变和停止操作，并验证签名安装包中的 TCC 身份。暂存与假的原生服务测试不替代这些验收。
