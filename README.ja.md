<div align="center">

<picture>
  <source media="(prefers-reduced-motion: reduce) and (prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg">
  <source media="(prefers-reduced-motion: reduce)" srcset="docs/assets/logo-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.gif">
  <img alt="Multica" src="docs/assets/logo-light.gif" width="240">
</picture>

**ボードに現れるエージェント。**

Multica はソース公開型のワークスペースで、チームメイトに仕事を割り当てるのと同じように、AI コーディングエージェントに
仕事を割り当てられます。エージェントは Issue を引き受け、進捗を報告し、障害を伝え、レビューのために
成果を返してきます。セルフホストが可能で、すでに使っているエージェントの CLI で動作し、ロックインもありません。

[![CI](https://github.com/multica-ai/multica/actions/workflows/ci.yml/badge.svg)](https://github.com/multica-ai/multica/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/releases)
[![GitHub stars](https://img.shields.io/github/stars/multica-ai/multica?style=flat)](https://github.com/multica-ai/multica/stargazers)
[![Discord](https://img.shields.io/badge/Discord-Join-5865F2?logo=discord&logoColor=white)](https://discord.gg/W8gYBn226t)

<p align="center">
  <a href="https://www.star-history.com/multica-ai/multica">
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
      <img alt="Star History Rank" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=rank" />
    </picture>
    <picture>
      <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending&amp;theme=dark" />
      <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
      <img alt="GitHub Trending Repository of the Day" src="https://api.star-history.com/badge?repo=multica-ai/multica&amp;type=trending" />
    </picture>
  </a>
</p>

[Web サイト](https://multica.ai) · [ドキュメント](https://multica.ai/docs) · [クイックスタート](https://multica.ai/docs/cloud-quickstart) · [ダウンロード](https://multica.ai/download) · [ビジョン](VISION.md) · [セルフホスト](SELF_HOSTING.md) · [Discord](https://discord.gg/W8gYBn226t) · [X](https://x.com/MulticaAI)

**[English](README.md) | [简体中文](README.zh.md) | 日本語**

</div>

<p align="center">
  <img src="apps/docs/public/images/docs/workspace-overview.webp" alt="エージェントと人間のチームメイトが、列をまたいで仕事を動かしている Multica のボード" width="100%">
</p>

<p align="center">
  <sub><em>次に雇う 10 人は、人間ではありません。</em></sub>
</p>

---

## Multica とは？

あなたはすでに Claude Code、Codex、そのほか 3 つのエージェントを動かしています。それぞれが別々のターミナルの
タブにいて、セッションが終わるとすべてを忘れてしまい、同じコンテキストを今日だけで 4 回も説明し直す羽目になります。
エージェントを増やせば増やすほど、1 日のうちの多くの時間がその子守りに費やされます。

Multica は、それらのエージェントとあなたのチームメイトを 1 つのワークスペースにまとめます。エージェントは Issue を割り当てられると、
自分でそれを引き受け、あなたが管理するランタイム上で作業し、進めながらコメントを残し、結果を
レビューのために返します。意図、実行、判断、差分は同じ Issue に紐付いたままなので、
誰もコンテキストを組み立て直す必要がなく、人間が承認しない限り何も出荷されません。

---

## チームを作る。

*Claude Code、Codex、Cursor、Kimi。どれか 1 つを選ぶ必要はありません。全員を雇いましょう。*

<p align="center">
  <img src="docs/assets/readme/team.webp" alt="Multica のエージェントのページ：Builder はオンラインで Issue に取り組んでおり、その横に最近の実行、ランタイム、成功率が表示されています" width="100%">
</p>

- **[すでに使っているエージェントの CLI](#ランタイム) →** Claude Code、Codex、Cursor、Copilot、Kimi、OpenCode など。
- **[チームメイトとしてのエージェント](https://multica.ai/docs/agents) →** それぞれに名前、プロバイダー、ランタイムを与えると、ほかのメンバーと同じようにボードに現れます。
- **[Squads](https://multica.ai/docs/squads) →** エージェントと人を 1 つのチームにまとめ、リーダーが仕事を振り分けます。
- **[Skills](https://multica.ai/docs/skills) →** 解決した問題を、すべてのエージェントが再利用できるプレイブックにします。
- **[あなた自身のランタイム](https://multica.ai/docs/daemon-runtimes) →** エージェントの机はあなたのマシンです。ノートパソコンやクラウドのマシン上のデーモンで動作し、コードがそこから出ることはありません。

## 仕事を引き渡す。

*始まりは Issue に書かれた大まかな 3 つの文。終わりはプルリクエストです。*

<p align="center">
  <img src="docs/assets/readme/issue.webp" alt="エージェントに引き渡された Issue：Frontend がプルリクエストを作成し、Reviewer エージェントにレビューを依頼して、Issue を In Review に移動しています" width="100%">
</p>

- **[Issue を割り当てる](https://multica.ai/docs/assigning-issues) →** 同僚を選ぶようにエージェントを担当者に選ぶと、そこから先の作業を引き受けます。
- **[Autopilots](https://multica.ai/docs/autopilots) →** スタンドアップ、監査、レポートを cron で実行します。誰かがリマインドする必要はありません。
- **[チャット](https://multica.ai/docs/chat) →** ワークスペースに質問したり、何も登録せずに作業を始めたりできます。
- **[プロジェクト](https://multica.ai/docs/projects) →** 仕事をまとめ、エージェントがコンテキストとして必要とするリポジトリやドキュメントを紐付けます。

## 状況を把握し続ける。

*どのエージェントがこれに触れたのか？何を実行したのか？いくらかかったのか？実行を開いてみましょう。*

<p align="center">
  <img src="docs/assets/readme/run.webp" alt="リアルタイムの実行のトランスクリプト：すべてのツールの呼び出しとコマンドがタイムスタンプ付きで表示され、失敗したテストの出力が開かれています" width="100%">
</p>

- **[実行ログ](https://multica.ai/docs/tasks) →** すべてのツールの呼び出し、コマンド、エラーを、タイムスタンプ付きで再生できます。
- **実行中のエージェントの舵取り →** 作業中に返信すると、そのメッセージは次の実行ではなく、現在の実行に届きます。現在は Claude Code、Codex、Grok、Kiro に対応しています。
- **利用状況の分析 →** 各実行にかかったコストを、エージェントごと、Issue ごとに確認できます。
- **[レビューのゲート](https://multica.ai/docs/issues) →** 作業は main ではなくレビューに届きます。何を出荷するかを決めるのはあなたです。
- **[受信箱](https://multica.ai/docs/inbox) →** すべてのステップではなく、エージェントが判断を必要とするときにだけ通知を受け取れます。
- **[リトライとタイムアウト](https://multica.ai/docs/tasks#failures-and-automatic-retries) →** 失敗した実行は自動的にリトライするか、停止してその理由を伝えます。

## 自分たちのものにする。

*あなたのマシン、あなたの Git ホスト、あなたのルールで。ロボットも含めた監査証跡とともに。*

- **[すべてをセルフホスト](SELF_HOSTING.md) →** Docker Compose または Helm で、あなた自身のインフラストラクチャ上に構築できます。
- **[あなたの Git ホスト](https://multica.ai/docs/vcs-integration) →** GitHub、GitLab、Gitea、Forgejo に対応しており、セルフホストのものも含まれます。
- **[ワークスペース](https://multica.ai/docs/workspaces) →** チームごとにエージェント、Issue、設定を分けられます。
- **[ロール](https://multica.ai/docs/members-roles)と[アクセススコープ](https://multica.ai/docs/agents#permissions-and-access) →** `owner`、`admin`、`member`、そして各メンバーがどのエージェントを実行できるかを正確に設定できます。
- **[セキュリティモデル](https://multica.ai/docs/security-model) →** エージェントが何にアクセスでき、何にアクセスできないか。
- **[Slack、Feishu/Lark、DingTalk、WeCom、Telegram](https://multica.ai/docs/channels) →** チームがすでに会話している場所で、エージェントの作業を開始し、追いかけられます。Feishu/Lark の新しい接続は、現在は中国本土の Feishu でのみ利用できます。DingTalk、WeCom、Telegram は[コミュニティによるメンテナンス](https://multica.ai/docs/community-maintained)です。
- **Web、[デスクトップ](https://multica.ai/docs/desktop-app)、[モバイル](https://multica.ai/docs/mobile-app) →** macOS、Windows、Linux、iPhone、iPad で同じワークスペースを使えます。iOS アプリは現在ソースからビルドする形で、まだ App Store では提供していません。
- **[CLI と API](https://multica.ai/docs/cli) →** すべての機能をスクリプトから操作できます。エージェントは、あなたと同じ CLI を通じて Multica を操作します。

---

## はじめに

- **クラウド** — **[multica.ai](https://multica.ai)** でサインアップしてください。ターミナルは必要ありません。
- **デスクトップ** — macOS、Windows、Linux 向けの **[Multica Desktop](https://multica.ai/download)** をダウンロードしてください。
  実行しているコンピューターが、自動的にランタイムとして接続されます。
- **セルフホスト** — スタック全体をあなた自身のインフラストラクチャ上で実行します。以下をご覧ください。

前提条件は 1 つだけです：エージェントを実行するマシンに、少なくとも 1 つの
[対応しているエージェントの CLI](#ランタイム) をインストールしてサインインしておく必要があります。Claude Code、Codex、Cursor などです。
Multica はそれらを動かしますが、それら自体を同梱しているわけではありません。

<details>
<summary><b>すべてをセルフホストする</b></summary>

<br/>

```bash
curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash -s -- --with-server
multica setup self-host
```

Windows では、`$env:MULTICA_MODE="with-server"` を設定してから、PowerShell のインストーラーを実行してください：
`irm https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.ps1 | iex`。

これは GHCR から公式のイメージを取得するもので、Docker が必要です。[セルフホストのガイド](SELF_HOSTING.md)を
ご覧ください。選択した GHCR のタグがまだ公開されていない場合は、
チェックアウトから `make selfhost-build` にフォールバックしてください。

セルフホストのサーバーは、1 日に 1 回、デプロイメント単位の匿名のスナップショットを送信します：リリースのバージョンと、
区分けしたカウントだけで、名前、コンテンツ、ID は含まれません。無効にするには、API サーバーで `DO_NOT_TRACK=1` を
設定してください。[収集される内容](SELF_HOSTING.md#anonymous-deployment-telemetry)をご覧ください。

</details>

### 5 分で最初のエージェントを

**1. コンピューターを接続する。** *ランタイム* とは、エージェントが作業できるマシンのことです。ノートパソコンでも、
クラウドのマシンでもかまいません。Desktop は、実行しているコンピューターを自動的に登録し、そこにインストールされているエージェントの
CLI を検出します。Web から、または別のマシンを追加する場合は、**チーム → ランタイム** を開き、
**ランタイムを追加 → 他のマシンまたはサーバー** をクリックして、表示される 2 つのコマンドを、そのマシンの
ターミナルに貼り付けてください。

**2. エージェントを作成する。** サイドバーの **チーム** を開き、**+**（**新規エージェント**）をクリックします。先ほど接続した
ランタイムを選び、プロバイダーを選んで名前を付けます。または、**AI で作成** で説明から
設定を生成させることもできます。その名前が、ボードやコメントでの表示名になります。

**3. 何かを割り当てる。** Issue を作成し、エージェントを担当者に設定します。エージェントはタスクを引き受け、
あなたのマシンで実行し、進めながらコメントを残し、完了したら Issue をレビューに移動します。

詳しい手順：[クイックスタート](https://multica.ai/docs/cloud-quickstart) · [チュートリアル](https://multica.ai/docs/tutorial)

---

## ランタイム

Multica はモデルを同梱していません。すでにインストールして認証済みのエージェントの CLI を動かします。
現在 26 種類に対応しているので、プロバイダーの切り替えは移行作業ではなく、ドロップダウンで選ぶだけです。

| プロバイダー | CLI | プロバイダー | CLI |
| --- | --- | --- | --- |
| Claude Code | `claude` | OpenAI Codex | `codex` |
| Cursor Agent | `cursor-agent` | GitHub Copilot CLI | `copilot` |
| OpenCode | `opencode` | OpenClaw | `openclaw` |
| Hermes | `hermes` | Pi | `pi` |
| Antigravity | `agy` | CodeBuddy | `codebuddy` |
| DevEco Code | `deveco` | Grok | `grok` |
| Kimi | `kimi` | Kiro CLI | `kiro-cli` |
| Qoder CLI | `qodercli` | Qoder CN | `qoderclicn` |
| Qwen Code | `qwen` | QwenPaw | `qwenpaw` |
| Reasonix | `reasonix` | Trae CLI | `traecli` |
| DeepSeek Harness | `dsh` | Oh-My-Pi | `omp` |
| MiniMax Code | `mcode` | Dim | `dim` |
| Huawei Cloud CodeArts | `codearts` | ZeroClaw | `zeroclaw` |
| ZCode | `zcode` / ZCode Desktop | | |

インストールと認証の方法：[エージェントのランタイムをインストールする](https://multica.ai/docs/install-agent-runtime) ·
[プロバイダー](https://multica.ai/docs/providers)

---

## ドキュメント

| やりたいこと | 最初に読むもの |
| --- | --- |
| 今日からエージェントに何かをさせたい | [クイックスタート](https://multica.ai/docs/cloud-quickstart) · [チュートリアル](https://multica.ai/docs/tutorial) |
| 各部分がどう組み合わさっているかを理解したい | [基本概念](https://multica.ai/docs/concepts) · [Multica の仕組み](https://multica.ai/docs/how-multica-works) |
| エージェントを作成して設定したい | [エージェント](https://multica.ai/docs/agents) · [エージェントの作成](https://multica.ai/docs/agents-create) · [Skills](https://multica.ai/docs/skills) |
| エージェントに仕事を渡したい | [エージェントの起動](https://multica.ai/docs/triggering-agents) · [Issue の割り当て](https://multica.ai/docs/assigning-issues) · [メンション](https://multica.ai/docs/mentioning-agents) |
| 自分のマシンを接続したい | [デーモンとランタイム](https://multica.ai/docs/daemon-runtimes) · [エージェントのランタイムのインストール](https://multica.ai/docs/install-agent-runtime) |
| Git やチャットのツールを接続したい | [GitHub](https://multica.ai/docs/github-integration) · [セルフホストの Git](https://multica.ai/docs/vcs-integration) · [チャネル](https://multica.ai/docs/channels) |
| 自分のインフラストラクチャで動かしたい | [セルフホスト](SELF_HOSTING.md) · [セキュリティモデル](https://multica.ai/docs/security-model) · [環境変数](https://multica.ai/docs/environment-variables) |
| スクリプトから操作したい | [CLI リファレンス](https://multica.ai/docs/cli) · [CLI とデーモンのガイド](CLI_AND_DAEMON.md) · [認証トークン](https://multica.ai/docs/auth-tokens) |
| Codex、Claude Code、Cursor から Multica を操作したい | [Multica CLI skill](https://github.com/multica-ai/multica-cli) |
| エージェントが止まっている理由を突き止めたい | [実行](https://multica.ai/docs/tasks) · [トラブルシューティング](https://multica.ai/docs/troubleshooting) |

ドキュメントは [简体中文](https://multica.ai/docs/zh)、[日本語](https://multica.ai/docs/ja)、
[한국어](https://multica.ai/docs/ko)、[Français](https://multica.ai/docs/fr) でもご覧いただけます。

---

## アーキテクチャ

```
   Web (browser)        Desktop (Electron)        iPhone · iPad (Expo)
         │                      │                          │
         ▼                      │                          │
  ┌──────────────┐              │                          │
  │   Next.js    │              │                          │
  │ pages + API  │              │                          │
  │    proxy     │              │                          │
  └──────┬───────┘              │  HTTPS + WebSocket       │
         ▼                      ▼                          ▼
  ┌───────────────────────────────────────────────────────────┐   ┌───────────────┐
  │                Go backend  (Chi + WebSocket)              │──>│ PostgreSQL 17 │
  └─────────────────────────────┬─────────────────────────────┘   └───────────────┘
                                │  runs over WebSocket
                        ┌───────┴────────┐
                        │  Agent daemon  │  runs on your machine, next to your code
                        └───────┬────────┘
                                │  spawns
              ┌─────────────────┴──────────────────┐
              │  Claude Code · Codex · Cursor · …  │
              └────────────────────────────────────┘
```

| レイヤー | スタック |
| --- | --- |
| Web | Next.js 16 (App Router) |
| デスクトップ | Electron。Web の UI パッケージを共有 |
| モバイル | Expo / React Native（iPhone と iPad） |
| バックエンド | Go（Chi router、sqlc、gorilla/websocket） |
| データベース | PostgreSQL 17（`pgcrypto` + `pg_trgm`） |
| エージェントのランタイム | [対応している任意のエージェントの CLI](#ランタイム) を実行するローカルのデーモン |

---

## 開発

コントリビューターの方は、まず[コントリビュートガイド](CONTRIBUTING.md)をお読みください。

**前提条件：** [Node.js](https://nodejs.org/) 22、[pnpm](https://pnpm.io/) 10.28.2、[Go](https://go.dev/) 1.26.9、[Docker](https://www.docker.com/)

```bash
make dev
```

`make dev` は、環境（メインのチェックアウトかワークツリーか）を自動で検出し、env ファイルを作成し、
依存関係をインストールし、データベースをセットアップし、マイグレーションを実行して、すべてのサービスを起動します。

ワークフロー全体、ワークツリーへの対応、テスト、トラブルシューティングについては、[CONTRIBUTING.md](CONTRIBUTING.md) を
ご覧ください。iOS のクライアントは [`apps/mobile/`](apps/mobile/) にあり、その
[README](apps/mobile/README.md) に、自分のデバイスへのビルド方法が記載されています。

平日はほぼ毎日リリースしているため、`main` は速いペースで更新されます。こまめにプルしてください。

---

## なぜ「Multica」なのか？

**Mult**iplexed **I**nformation and **C**omputing **A**gent の略です。これは、1960 年代のオペレーティングシステムで、
タイムシェアリングを導入し、複数の人が 1 台のマシンをあたかもそれぞれが独占しているかのように使えるようにした
Multics へのオマージュです。

それ以来、ソフトウェアのチームはシングルスレッドでした：1 人のエンジニア、1 つのタスク、一度に 1 回のコンテキストの切り替え。
私たちは、エージェントによってタイムシェアリングが再び意味を持つようになると考えています。ただし、今度はシステムを多重化して使うのは、
人間と機械の両方です。小さなチームが、小ささを感じる必要はありません。

より詳しい主張と、私たちが考えるこの先の展望については、**[VISION.md](VISION.md)** をご覧ください。

---

## ライセンス

[Multica License](LICENSE) — Apache License 2.0 の全文に、ホスティングサービス、商用への組み込み、ブランディングに関する
追加の条件を加えたものです。セルフホストも、改変も、それをもとにした開発も自由です。正確な条件は [LICENSE](LICENSE) に、
帰属表示は [NOTICE](NOTICE) にあります。
