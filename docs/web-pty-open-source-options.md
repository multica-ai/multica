# Web PTY 开源方案选型

日期：2026-08-09  
范围：只比较可嵌入 Multica Agent Cockpit 的终端渲染与交互能力。资料只来自项目官方仓库、官方 API 文档和官方发布记录。

## 结论

**保留 Multica 现有 `@xterm/xterm 6.0.0`，补齐官方 addon 和终端交互层；不要整体接入 ttyd 或 WeTTY，也暂不切换到 wterm。**

原因很直接：Multica 已经拥有安全边界完整的数据面——浏览器鉴权 WebSocket、服务端网关、单控制者租约、限流、重放、任务代次校验，以及守护进程持有的 PTY。当前 [`agent-terminal.tsx`](../packages/views/issues/components/agent-terminal.tsx) 只用了 xterm.js 核心和 `addon-fit`，视觉与交互能力尚未充分利用。xterm.js 官方本身支持 curses/TUI、鼠标、CJK、emoji、IME、屏幕阅读器和可选 GPU 渲染，并有搜索、WebGL、Unicode、剪贴板等官方 addon；它也是 ttyd 和 WeTTY 的实际渲染核心。[xterm.js README](https://github.com/xtermjs/xterm.js#features) [官方 addon 列表](https://github.com/xtermjs/xterm.js#addons)

需要保持不变的链路：

```text
Browser UI + xterm.js
  -> Multica TerminalClient
  -> authenticated Browser WebSocket
  -> Multica Server gateway / lease / replay / rate limits
  -> authenticated Daemon WebSocket
  -> Daemon-owned PTY and Codex process
```

不得用 `@xterm/addon-attach`、ttyd 的 `/ws`、WeTTY 的 Socket.IO 或 wterm 自带的 `WebSocketTransport` 替换 `TerminalClient`。渲染器只能消费 `TerminalClient.onOutput`，输入和 resize 也必须继续走 Multica 的控制租约与协议。

## 候选比较

| 候选 | 前端能力 | 与 Multica 架构的关系 | 许可证与维护信号 | 结论 |
| --- | --- | --- | --- | --- |
| **xterm.js 6** | 默认 DOM 渲染，可选 WebGL2；`onData`/`onBinary`/`onResize`；官方 Fit、Search、WebGL、Unicode 11、Clipboard、Web Links 等 addon；CJK、emoji、IME、屏幕阅读器和最小对比度选项。[功能](https://github.com/xtermjs/xterm.js#features) [API](https://xtermjs.org/docs/api/terminal/classes/terminal/) | 与现有 `TerminalClient` 的 `write`/`onData` 模式完全匹配，不需要改变服务端协议。 | MIT；Multica 已使用最新稳定版 `6.0.0`；该版加入 synchronized output，并明确移除了 Canvas addon，推荐 DOM 或 WebGL。[6.0.0 发布记录](https://github.com/xtermjs/xterm.js/releases/tag/6.0.0) | **采用并增强。** |
| **ttyd** | 自带成熟的 xterm.js 客户端：WebGL/Canvas/DOM 回退、Fit、Unicode 11、Clipboard、Web Links、Sixel/image、ZMODEM/trzsz、浏览器端流控；官方说明支持 CJK 与 IME。[README](https://github.com/tsl0922/ttyd#features) [客户端实现](https://github.com/tsl0922/ttyd/blob/main/html/src/components/terminal/xterm/index.ts) | 它是完整 C/libwebsockets PTY 服务，自己生成进程、令牌和 WebSocket 协议。嵌入服务或 iframe 会复制/绕过 Multica 的鉴权、任务所有权、租约、重放和 Stop 语义。 | MIT；稳定版 `1.7.7`，主分支客户端仍基于 xterm.js `5.5.0`。[发布记录](https://github.com/tsl0922/ttyd/releases/tag/1.7.7) [依赖](https://github.com/tsl0922/ttyd/blob/main/html/package.json) | **不接入整体；只借鉴 WebGL 回退、resize overlay 和渲染流控模式。** |
| **WeTTY 3** | 基于 xterm.js 6；桌面加载 WebGL，移动端回退 DOM；集成 Fit、Web Links、Image，并有 Ctrl、Esc、Tab、方向键等触屏按键。[终端实现](https://github.com/butlerx/wetty/blob/main/src/client/wetty/term.ts) [移动端输入](https://github.com/butlerx/wetty/blob/main/src/client/wetty/mobile.ts) | 它是 Node.js + `node-pty` + Socket.IO + SSH/login 的完整应用。其主用途是登录本机或 SSH 主机，不理解 Multica 的任务、代次、观察者或控制租约。[README](https://github.com/butlerx/wetty#usage) | MIT；`v3.2.0` 于 2026-07 发布，主分支使用 xterm.js 6 和官方 addon。[发布记录](https://github.com/butlerx/wetty/releases/tag/v3.2.0) [依赖](https://github.com/butlerx/wetty/blob/main/package.json) | **不接入整体；只借鉴移动端快捷键栏和 WebGL 回退。** |
| **Vercel Labs wterm** | Zig/WASM 终端核心 + DOM 渲染；React 包；原生选择、复制、浏览器查找与屏幕阅读器；alternate screen、scrollback、24-bit 色、CJK/emoji 宽字符、ResizeObserver。[README](https://github.com/vercel-labs/wterm#features) [React API](https://github.com/vercel-labs/wterm/blob/main/packages/%40wterm/react/README.md) | `write`、`onData`、`onResize` 可以适配 `TerminalClient`，不必使用其 WebSocket transport；但替换后要重新验证全部 ANSI/DEC、鼠标和 Codex TUI 行为。 | Apache-2.0；项目很新，当前仅 `v0.3.2`。该版本仍集中修复宽字符、scrollback、alternate-screen 样式和 WASM 加载问题。[v0.3.2 发布记录](https://github.com/vercel-labs/wterm/releases/tag/v0.3.2) | **观察和做隔离 benchmark，不作为当前替换项。** |

## 建议拼接的能力

### 1. WebGL 渲染，DOM 自动回退

动态加载 `@xterm/addon-webgl`，终端 `open()` 后再加载；构造或加载失败时继续使用 xterm.js 6 默认 DOM 渲染。监听 `onContextLoss`，释放 WebGL addon 后回退 DOM。官方明确说明 WebGL context 可能因 OOM 或系统休眠丢失，并给出了释放 addon 的处理方式。[WebGL addon](https://github.com/xtermjs/xterm.js/tree/master/addons/addon-webgl#handling-context-loss)

不要照搬 ttyd 的 Canvas 回退：xterm.js 6 已删除 Canvas addon，只推荐 DOM 或 WebGL。[xterm.js 6.0.0](https://github.com/xtermjs/xterm.js/releases/tag/6.0.0)

### 2. 原生终端搜索

加载 `@xterm/addon-search`，在终端区域提供 `Cmd/Ctrl+F` 搜索条、上一个/下一个、匹配数量和关闭操作。搜索只访问浏览器内 xterm buffer，不发往服务器，也不持久化终端内容。官方 addon 提供 `findNext`/`findPrevious` 和结果/装饰选项。[Search addon](https://github.com/xtermjs/xterm.js/tree/master/addons/addon-search) [Search API](https://github.com/xtermjs/xterm.js/blob/master/addons/addon-search/typings/addon-search.d.ts)

### 3. 中文、emoji 与终端宽度

加载 `@xterm/addon-unicode11` 并激活版本 `11`，同时确保守护进程 PTY 环境使用 UTF-8 locale。xterm.js 的 `Uint8Array` 输入按流式 UTF-8 解码，`onData` 输出 Unicode 字符串；官方也明确说明非 UTF-8 locale 会导致非 ASCII 程序行为错误。[Unicode 11 addon](https://github.com/xtermjs/xterm.js/tree/master/addons/addon-unicode11) [编码指南](https://xtermjs.org/docs/guides/encoding/)

WebGL 模式可以评估 `rescaleOverlappingGlyphs: true`，官方说明该选项用于减少单元格重叠并帮助 GB18030 合规；它不适用于 DOM renderer。[终端选项](https://xtermjs.org/docs/api/terminal/interfaces/iterminaloptions/#optional-rescaleoverlappingglyphs)

`addon-unicode-graphemes` 仍被官方标为 experimental，先不放入默认路径。[官方说明](https://github.com/xtermjs/xterm.js/tree/master/addons/addon-unicode-graphemes)

### 4. 可访问性

至少提供：

- `minimumContrastRatio: 4.5`，对应官方列出的 WCAG AA 参考值；
- 可开启的 `screenReaderMode`，让 DOM 暴露 NVDA 和 macOS VoiceOver 所需元素；
- 给终端输入区、搜索框、控制权按钮与移动快捷键添加本地化 label；
- controller 与 observer 不只靠颜色区分，继续保留文字状态。

这些能力由 xterm.js 6 的稳定选项直接支持。[ITerminalOptions](https://xtermjs.org/docs/api/terminal/interfaces/iterminaloptions/)

### 5. 移动端控制栏

WeTTY 的可复用思路是触屏快捷键栏，不是它的 Socket.IO/PTY 服务：提供 `Ctrl`、`Esc`、`Tab`、上下左右、隐藏键盘和粘贴。按键最终调用 xterm 的 `input()`，再由现有 `terminal.onData` 进入 `TerminalClient.sendInput`；没有 Multica controller lease 时必须禁用。WeTTY 的实现也证明 WebGL 可在移动端跳过，让 DOM renderer 承担回退。[WeTTY 终端与移动按键](https://github.com/butlerx/wetty/blob/main/src/client/wetty/term.ts)

当前“移动端只读”限制应在 iOS Safari 与 Android Chrome 实机通过中文 IME、组合输入、方向键、粘贴、横竖屏 resize、控制租约抢占和 Ctrl+C 测试后再解除，不能只凭桌面响应式测试开放写入。

### 6. 链接、剪贴板和图像默认收紧

- Web Links 只有在自定义 handler 严格允许 `http://` 与 `https://`，并在打开前向用户展示目标时才启用。xterm.js 官方明确警告终端可输出 `javascript:` 等恶意链接。[linkHandler 安全说明](https://xtermjs.org/docs/api/terminal/interfaces/iterminaloptions/#optional-linkhandler)
- Clipboard/OSC 52 不默认授予远端输出读取或改写系统剪贴板的能力；粘贴必须是用户动作，且只有 controller 可以发送。
- Image/Sixel、ZMODEM/trzsz 与文件下载不是 Codex TUI 的当前需求。不要因为 ttyd/WeTTY 支持就一起引入；它们会扩大内存、内容处理和文件传输攻击面。

xterm.js 官方安全指南强调：网页中的任何 JavaScript 都可能读到终端按键，终端集成必须尽量减少第三方代码和权限。因此优先采用 xterm.js 同仓库官方 addon，并继续执行 Multica 当前的鉴权和最小权限模型。[xterm.js 安全指南](https://xtermjs.org/docs/guides/security/)

## 实施顺序

1. **第一批：** WebGL→DOM 回退、Search、Unicode 11、AA 对比度、终端 focus/resize 修正。
2. **第二批：** 搜索与连接状态 overlay、复制/粘贴按钮、移动快捷键栏；移动写入仍由 feature flag 控制。
3. **验收：** 使用现有 fake TUI 和真实 Codex TUI，覆盖 alternate screen、同步输出、中文/emoji/IME、快速 burst、WebGL context loss、浏览器缩放、横竖屏、搜索、VoiceOver、双标签 observer/controller、刷新重放和 Stop。
4. **观察项：** 单独做 xterm.js 6 与 wterm `v0.3.x` 的渲染正确性、CPU、内存、输入延迟和可访问性 benchmark。只有 wterm 对真实 Codex TUI 明显更好且协议覆盖稳定后，才讨论 renderer adapter；即使切换 renderer，Multica 的 WebSocket、安全和控制协议也不变。

## 最终选择

短期最有效的“开源拼接”不是换掉现有终端，而是把 **xterm.js 官方 WebGL + Search + Unicode 11 + accessibility** 接到现有 `AgentTerminal`，再借用 **WeTTY 的移动快捷键交互** 和 **ttyd 的 WebGL 回退/resize/流控经验**。这样能显著改善观感、滚动、查找、中文宽度、输入和移动操作，同时保留 Multica 已经完成并验证过的安全数据面。
