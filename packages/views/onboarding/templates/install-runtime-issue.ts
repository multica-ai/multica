/**
 * Skip path: "Connect an AI coding tool and create your agent".
 *
 * Written to a new issue (assigned to the user themselves) by OnboardingFlow
 * when the user takes the Skip exit on Step 3, which then opens it. Desktop
 * installs the tool itself, so the guide leads with that and keeps the
 * terminal route as the fallback.
 */

/**
 * Localized so users see the title in their current supported locale on the
 * board. Once a tool is connected, the Runtimes page offers to create the
 * member's agent, so this guide ends there.
 *
 * Note: server's deprecation shim (`onboarding_shim.go:noRuntimeIssueTitle`)
 * still uses the bare English string for its title-based dedupe — that
 * codepath only runs for pre-v3 desktop builds and never overlaps with
 * the v3 frontend population, so the two title-spaces drifting is fine.
 */
export const INSTALL_RUNTIME_ISSUE_TITLE = {
  en: "Connect an AI coding tool and create your agent",
  zh: "连接 AI 编程工具，创建你的智能体",
  ko: "AI 코딩 도구를 연결하고 나만의 에이전트 만들기",
  ja: "AI コーディングツールを接続して、自分のエージェントを作る",
} as const;

const en = `Welcome to Multica.

Your agent works through an AI coding tool on your computer — Claude Code, Codex, or Cursor CLI. Connect any one of them and you can create your personal agent.

## The quickest way: Multica Desktop

1. Open Multica Desktop (on the web, Multica offers the download).
2. Pick a tool. Multica installs it and signs you in — no terminal needed.
3. Confirm the name, create your agent, and start chatting.

Download: https://multica.ai/download

## Prefer the terminal?

Install one tool, sign in, then connect this computer:

- Claude Code: \`curl -fsSL https://claude.ai/install.sh | bash\`, then \`claude\`
- Codex: \`npm i -g @openai/codex\` (or \`brew install codex\`), then \`codex login\`
- Cursor CLI: \`curl https://cursor.com/install -fsS | bash\`, then \`cursor-agent login\`

Then open Runtimes → Connect a computer and run the commands it shows. A running daemon picks up newly installed tools within a few minutes; \`multica daemon restart\` applies it right away.

Full guide: https://multica.ai/docs/install-agent-runtime

## When it's connected

Open Runtimes. The page offers **Create your agent** — it makes a private agent only you can use and opens a chat with it.

In the meantime you can already use Multica to track work: create a project, add a few issues, and move them across the board.`;

const zh = `欢迎来到 Multica。

智能体要用你电脑上的 AI 编程工具干活：Claude Code、Codex 或 Cursor CLI。装好任意一个，就能创建你的个人智能体。

## 最快的方式：Multica 桌面端

1. 打开 Multica 桌面端（网页端会提示你下载）。
2. 选一个工具，Multica 帮你装好并登录，不用打开终端。
3. 确认名字，创建智能体，开始对话。

下载：https://multica.ai/download

## 想用终端？

装好一个工具并登录，再把这台电脑连上来：

- Claude Code：\`curl -fsSL https://claude.ai/install.sh | bash\`，然后运行 \`claude\`
- Codex：\`npm i -g @openai/codex\`（或 \`brew install codex\`），然后运行 \`codex login\`
- Cursor CLI：\`curl https://cursor.com/install -fsS | bash\`，然后运行 \`cursor-agent login\`

然后打开「运行时」→「连接电脑」，按页面上的命令执行。运行中的守护进程几分钟内会识别新装的工具；想立刻生效可以运行 \`multica daemon restart\`。

完整文档：https://multica.ai/docs/install-agent-runtime

## 连上之后

打开「运行时」页面，点 **创建你的智能体**：它会建一个只有你能用的私有智能体，并打开和它的对话。

在这之前，你也可以先用 Multica 管理工作：建一个项目，加几个任务，在看板上流转。`;

const ko = `Multica에 오신 것을 환영합니다.

에이전트는 내 컴퓨터의 AI 코딩 도구(Claude Code, Codex, Cursor CLI)로 일합니다. 이 중 하나만 연결하면 나만의 에이전트를 만들 수 있습니다.

## 가장 빠른 방법: Multica 데스크톱

1. Multica 데스크톱을 엽니다(웹에서는 다운로드를 안내합니다).
2. 도구를 하나 고릅니다. Multica가 설치하고 로그인까지 해 줍니다. 터미널은 필요 없습니다.
3. 이름을 확인하고 에이전트를 만든 뒤 대화를 시작합니다.

다운로드: https://multica.ai/download

## 터미널이 편하다면

도구 하나를 설치하고 로그인한 다음 이 컴퓨터를 연결합니다:

- Claude Code: \`curl -fsSL https://claude.ai/install.sh | bash\` 후 \`claude\`
- Codex: \`npm i -g @openai/codex\`(또는 \`brew install codex\`) 후 \`codex login\`
- Cursor CLI: \`curl https://cursor.com/install -fsS | bash\` 후 \`cursor-agent login\`

그다음 Runtimes → 컴퓨터 연결을 열고 안내된 명령을 실행합니다. 실행 중인 daemon은 몇 분 안에 새로 설치된 도구를 인식합니다. 바로 적용하려면 \`multica daemon restart\`를 실행하세요.

전체 가이드: https://multica.ai/docs/install-agent-runtime

## 연결된 다음에는

Runtimes를 엽니다. **나만의 에이전트 만들기**를 누르면 나만 쓸 수 있는 비공개 에이전트가 만들어지고 대화가 열립니다.

그동안에도 Multica로 일을 관리할 수 있습니다. 프로젝트를 만들고 태스크 몇 개를 추가해 보드에서 옮겨 보세요.`;

const ja = `Multica へようこそ。

エージェントは、あなたのコンピューター上の AI コーディングツール(Claude Code、Codex、Cursor CLI)で作業します。どれか 1 つを接続すれば、自分専用のエージェントを作れます。

## いちばん早い方法: Multica デスクトップ

1. Multica デスクトップを開きます(Web ではダウンロードを案内します)。
2. ツールを 1 つ選びます。インストールとサインインは Multica が行うので、ターミナルは不要です。
3. 名前を確認してエージェントを作成し、チャットを始めます。

ダウンロード: https://multica.ai/download

## ターミナルを使う場合

ツールを 1 つインストールしてサインインし、このコンピューターを接続します:

- Claude Code: \`curl -fsSL https://claude.ai/install.sh | bash\` の後に \`claude\`
- Codex: \`npm i -g @openai/codex\`(または \`brew install codex\`)の後に \`codex login\`
- Cursor CLI: \`curl https://cursor.com/install -fsS | bash\` の後に \`cursor-agent login\`

次に Runtimes → コンピューターを接続 を開き、表示されたコマンドを実行します。動作中の daemon は数分以内に新しいツールを認識します。すぐに反映するには \`multica daemon restart\` を実行してください。

詳しいガイド: https://multica.ai/docs/install-agent-runtime

## 接続できたら

Runtimes を開き、**自分のエージェントを作る** を選びます。自分だけが使える非公開のエージェントが作成され、チャットが開きます。

それまでの間も、Multica で仕事を管理できます。プロジェクトを作り、タスクをいくつか追加してボード上で動かしてみてください。`;

export const INSTALL_RUNTIME_ISSUE_BODY = { en, zh, ko, ja } as const;
