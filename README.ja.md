# Multica

**Multiplexed Information and Computing Agent**

ビジュアルインターフェースを通じて、コーディングエージェントの能力をすべての人に届けるネイティブデスクトップクライアント。

[English](./README.md) | [简体中文](./README.zh-CN.md) | [繁體中文](./README.zh-TW.md) | 日本語 | [한국어](./README.ko.md)

## なぜ "Multica" という名前なのか？

この名前は [Multics](https://en.wikipedia.org/wiki/Multics)（Multiplexed Information and Computing Service）に由来しています。Multicsは1964年に作られた先駆的なオペレーティングシステムです。Multicsは広く普及することはありませんでしたが、階層型ファイルシステムなど、現代のオペレーティングシステムの基礎を築きました。Unix自体もMulticsから派生したものです（Uniplexed Information and Computing Service -> Unics -> Unix）。

**メタファー：** Multicsがマルチユーザーのタイムシェアリング問題を解決するために作られたように、Multicaはナレッジワーカーのためのマルチモデル/マルチエージェント協調の問題を解決するために設計されています。

## 解決する課題

コーディングエージェント（Claude Code、Codex、Gemini CLIなど）は2025年に非常に強力になり、単なるコード作成をはるかに超えた複雑なタスクを解決できるようになりました。しかし、95%のナレッジワーカーは3つの主要な障壁によってこれらの能力を利用できません：

**1. インタラクションのミスマッチ**
- CLIベースのツールは、ターミナルの概念、ファイルパス、環境変数の理解を必要とする
- 現在のツールはビジネス成果ではなく、コード出力（差分、コミット、リンティング）に焦点を当てている
- ナレッジワーカーが気にするのは結果（チャート、レポート、分析）であり、それらを生成するスクリプトではない

**2. ローカル環境の課題**
- Webベースのエージェントはローカルファイル、フォルダ、ネイティブアプリケーションにアクセスできない
- Python、Node.js、その他の依存関係のセットアップは大きな障壁となる
- すべての依存関係を処理する「すぐに使える」サンドボックス環境がない

**3. プライバシーと信頼**
- 機密性の高いビジネスデータ（財務分析、法的文書、医療記録）はサードパーティのサーバーにアップロードできない
- データはローカルに保持し、インテリジェンスはクラウドから得るモデルが必要

Multicaは、データをローカルに保持しながらコーディングエージェントの能力を活用する、ビジュアルなネイティブデスクトップインターフェースを提供することでこのギャップを埋めます。

## 機能

- クリーンで直感的なインターフェースを持つネイティブmacOSアプリケーション
- [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol) を通じた複数のAIエージェントのサポート
- ローカルファースト：データはあなたのマシンから離れることはありません
- 履歴と再開機能を備えたセッション管理
- パワーユーザーとテスト用の組み込みCLI

## サポートされているエージェント

| エージェント | コマンド | インストール |
|-------|---------|---------|
| [OpenCode](https://github.com/opencode-ai/opencode) | `opencode acp` | `go install github.com/opencode-ai/opencode@latest` |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp` | `npm install -g codex-acp` |
| [Gemini CLI](https://github.com/google-gemini/gemini-cli) | `gemini acp` | `npm install -g @google/gemini-cli` |

## クイックスタート

```bash
# 依存関係をインストール
pnpm install

# インストールされているエージェントを確認
pnpm cli doctor

# デスクトップアプリを起動
pnpm dev
```

## CLI

Multicaには、エージェントのテストと対話のための包括的なCLIが含まれています：

```bash
pnpm cli                          # インタラクティブモード
pnpm cli prompt "メッセージ"        # ワンショットプロンプト
pnpm cli sessions                 # セッション一覧
pnpm cli resume <id>              # セッションを再開
pnpm cli agents                   # 利用可能なエージェント一覧
pnpm cli doctor                   # エージェントのインストール状況を確認
```

### インタラクティブモード

インタラクティブなREPLセッションを開始：

```bash
pnpm cli
```

利用可能なコマンド：

| コマンド | 説明 |
|---------|-------------|
| `/help` | ヘルプを表示 |
| `/new [cwd]` | 新しいセッションを作成（デフォルト：カレントディレクトリ） |
| `/sessions` | すべてのセッションを一覧表示 |
| `/resume <id>` | IDプレフィックスでセッションを再開 |
| `/delete <id>` | セッションを削除 |
| `/history` | 現在のセッションのメッセージ履歴を表示 |
| `/agent <name>` | 別のエージェントに切り替え |
| `/agents` | 利用可能なエージェントを一覧表示 |
| `/doctor` | エージェントのインストール状況を確認 |
| `/status` | 現在のステータスを表示 |
| `/cancel` | 現在のリクエストをキャンセル |
| `/quit` | CLIを終了 |

### ワンショットプロンプト

単一のプロンプトを送信して終了：

```bash
pnpm cli prompt "2+2は何ですか？"
pnpm cli prompt "ファイルを一覧表示" --cwd=/tmp
```

### オプション

| オプション | 説明 |
|--------|-------------|
| `--cwd=PATH` | エージェントの作業ディレクトリ |
| `--log` | セッションログを `logs/` ディレクトリに保存 |
| `--log=PATH` | セッションログを指定したファイルに保存 |

## 開発

```bash
# 開発モードでElectronアプリを起動
pnpm dev

# 型チェック
pnpm typecheck

# テストを実行
pnpm test
```

## ビルド

```bash
pnpm build:mac      # macOS
pnpm build:win      # Windows
pnpm build:linux    # Linux
```

## アーキテクチャ

```
Multica (Electron)
+-- レンダラープロセス (React)
|   +-- UIコンポーネント（チャット、設定など）
|
+-- メインプロセス
|   +-- Conductor（エージェント通信の調整）
|   |   +-- SessionStore（セッションの永続化）
|   |   +-- ClientSideConnection（ACP SDK）
|   |         +-- AgentProcess（サブプロセス管理）
|   |               +-- opencode/codex-acp/gemini (stdio)
|   |
|   +-- IPCハンドラー（セッション、エージェント、設定）
|
+-- Preload (contextBridge)
    +-- electronAPI（レンダラーに公開）
```

### セッション管理

MulticaはACPの上に独自のセッションレイヤーを維持します：

```
~/.multica/sessions/
+-- index.json              # セッションリスト（高速ロード）
+-- data/
    +-- {session-id}.json   # 完全なセッションデータ + 更新
```

**主要な設計上の決定：**
- **クライアントサイドストレージ**：MulticaはUI表示用に生の `session/update` データを保存
- **エージェント非依存**：各エージェントは独自の内部状態を個別に管理
- **再開動作**：新しいACPセッションを作成し、保存された履歴をUIに表示

## ライセンス

MIT
