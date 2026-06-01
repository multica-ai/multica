/**
 * Skip path, issue 1/2: "Connect a runtime to start using agents".
 *
 * Written to a new issue (assigned to the user themselves) by the welcome
 * hook when the user took the Skip exit on Step 3. Content is the
 * install-runtime tutorial; the Chinese version uses Kimi CLI as the
 * recommended quickest path, the English version uses Codex.
 *
 * Title is stable — kept identical to the v2 server-side
 * `NoRuntimeIssueTitle` so any existing dedupe code elsewhere keeps
 * matching by title.
 */

/**
 * Step 1 of the skip-path bundle. Bilingual so a Chinese user sees a
 * Chinese title on the board.
 *
 * Note: server's deprecation shim (`onboarding_shim.go:noRuntimeIssueTitle`)
 * still uses the bare English string for its title-based dedupe — that
 * codepath only runs for pre-v3 desktop builds and never overlaps with
 * the v3 frontend population, so the two title-spaces drifting is fine.
 */
export const INSTALL_RUNTIME_ISSUE_TITLE = {
  en: "Step 1 — Connect a runtime to start using agents",
  zh: "第 1 步 —— 连接运行时,开始使用 agent",
} as const;

const en = `Welcome to Multica.

Agents need a runtime before they can execute work. You can still use Multica as a lightweight project-management workspace while you install one.

## Try Multica first

Before the runtime is ready, you can:

1. Create a project for your current work.
2. Create a few issues and move them across backlog, todo, in_progress, and done.
3. Add priorities, labels, comments, and subscriptions.
4. Use Inbox to track assignments and mentions.

That gives you the project-management layer first. Once a runtime is connected, agents can start working from the same issues.

## Install your first agent runtime

Full guide: https://multica.ai/docs/install-agent-runtime

For English users, the fastest first path is Codex:

1. Make sure Node.js is installed.
2. Install Codex:
   npm i -g @openai/codex
3. Sign in:
   codex
4. Confirm your terminal can find it:
   which codex
   codex --version
5. Restart the Multica daemon:
   multica daemon restart
   If you use the desktop app, restarting the app is enough.
6. Return to Runtimes and refresh. You should see a Codex runtime online.
7. Create your first agent from that runtime, then assign an issue to the agent and set status to todo.

Codex reference: https://developers.openai.com/codex/cli

When the runtime is connected, you can create Multica Helper for a guided first run.`;

const zh = `欢迎来到 Multica。

智能体需要先连上运行时才能执行工作。运行时还没准备好时,你也可以先把 Multica 当作轻量项目管理工具体验起来。

## 先体验项目管理功能

运行时安装前,你可以先做这些事:

1. 为当前工作创建一个项目。
2. 新建几个 issue,并在 backlog、todo、in_progress、done 之间流转。
3. 给 issue 加优先级、标签、评论和订阅。
4. 用收件箱追踪分配给你的事项和 @mention。

这样你先熟悉项目管理层。连上运行时后,智能体会直接在这些 issue 上开始工作。

## 安装第一个 Agent 运行时

完整文档:https://multica.ai/docs/install-agent-runtime

中文用户建议先装 Kimi CLI:

1. 在 macOS / Linux 终端安装 Kimi CLI:
   curl -LsSf https://code.kimi.com/install.sh | bash
   Windows PowerShell:
   Invoke-RestMethod https://code.kimi.com/install.ps1 | Invoke-Expression
2. 确认终端能找到 Kimi:
   kimi --version
3. 在你想让 Kimi 工作的项目目录里启动一次:
   kimi
4. 首次启动后输入 /login,按提示完成 Kimi Code 或 API key 配置。
5. 重启 Multica 守护进程:
   multica daemon restart
   如果你用桌面端,重启 app 即可。
6. 回到 Runtimes 页面刷新。你应该能看到一个在线的 Kimi 运行时。
7. 用这个运行时创建第一个智能体,再把一个 issue 分配给它,并把状态切到 todo。

Kimi CLI 官方文档:https://moonshotai.github.io/kimi-cli/zh/guides/getting-started.html

运行时连上后,你就可以创建 Multica Helper,开始一次有智能体参与的上手引导。`;

export const INSTALL_RUNTIME_ISSUE_BODY = { en, zh } as const;

/**
 * Prefix sentence for the follow-up comment posted on this issue (the one
 * that links to the create-agent-guide issue via a mention chip). Kept
 * here as a TS const rather than an i18n JSON key because anything that
 * gets persisted to the DB must be available at write time without
 * depending on an i18n bundle having loaded the new key — otherwise a
 * cold dev server / stale build writes the raw key string into
 * `comment.content` and the comment is permanently broken.
 */
export const FOLLOWUP_COMMENT_PREFIX = {
  en: "Your next step:",
  zh: "完成后的下一步：",
} as const;
