# Multica

**Multiplexed Information and Computing Agent**

一個原生桌面客戶端，透過視覺化介面將程式智能體的能力帶給每一個人。

[English](./README.md) | [简体中文](./README.zh-Hans.md) | 繁體中文 | [日本語](./README.ja.md) | [한국어](./README.ko.md)

## 為什麼叫 "Multica"？

這個名字的靈感來自於 [Multics](https://en.wikipedia.org/wiki/Multics)（Multiplexed Information and Computing Service，多工資訊與運算服務），這是一個創建於 1964 年的開創性作業系統。儘管 Multics 最終沒有廣泛普及，但它奠定了現代作業系統的基礎，包括階層式檔案系統等概念。Unix 本身就是從 Multics 衍生而來的（Uniplexed Information and Computing Service -> Unics -> Unix）。

**隱喻：** 正如 Multics 當年是為了解決多使用者分時共享運算資源的問題，Multica 旨在解決多模型/多智能體協作的問題，服務於知識工作者。

## 解決的問題

程式智能體（如 Claude Code、Codex CLI、OpenCode）在 2025 年變得極其強大，其能力已經遠遠超出了單純的程式碼編寫。然而，95% 的知識工作者因為三個核心障礙而無法使用這些能力：

**1. 互動形態的錯配**

- 基於命令列的工具需要理解終端機概念、檔案路徑和環境變數
- 現有工具聚焦於程式碼輸出（差異比對、提交、程式碼檢查），而非業務成果
- 知識工作者關心的是結果（圖表、報告、分析），而不是產生這些結果的腳本

**2. 本機環境的挑戰**

- 基於網頁的智能體無法存取本機檔案、資料夾或原生應用程式
- 設定 Python、Node.js 或其他相依套件是一個巨大的障礙
- 缺少一個「開箱即用」、處理好所有相依套件的沙盒環境

**3. 隱私與信任**

- 敏感的業務資料（財務分析、法律文件、醫療紀錄）不能上傳到第三方伺服器
- 需要一種資料留在本機、智能來自雲端的模式

Multica 透過提供視覺化的原生桌面介面來彌合這一鴻溝，在保持資料本機化的同時，充分利用程式智能體的能力。

## 特性

- 原生 macOS 應用程式，介面簡潔直觀
- 透過 [Agent Client Protocol (ACP)](https://github.com/anthropics/agent-client-protocol) 支援多種 AI 智能體
- 本機優先：資料永遠不會離開你的裝置
- 工作階段管理，支援歷史紀錄和恢復功能
- 內建 CLI，適合進階使用者和測試使用

## 支援的智能體

| 智能體                                                         | 命令              | 安裝方式                                            |
| -------------------------------------------------------------- | ----------------- | --------------------------------------------------- |
| [Claude Code](https://github.com/anthropics/claude-code)       | `claude-code-acp` | `npm install -g @anthropics/claude-code-acp`        |
| [OpenCode](https://github.com/opencode-ai/opencode)            | `opencode acp`    | `go install github.com/opencode-ai/opencode@latest` |
| [Codex CLI (ACP)](https://github.com/zed-industries/codex-acp) | `codex-acp`       | `npm install -g codex-acp`                          |

## 快速開始

```bash
# 安裝相依套件
pnpm install

# 檢查已安裝的智能體
pnpm cli doctor

# 啟動桌面應用程式
pnpm dev
```

## 命令列工具

Multica 包含一個完整的 CLI，用於測試和與智能體互動：

```bash
pnpm cli                          # 互動模式
pnpm cli prompt "訊息"             # 單次提問
pnpm cli sessions                 # 列出工作階段
pnpm cli resume <id>              # 恢復工作階段
pnpm cli agents                   # 列出可用智能體
pnpm cli doctor                   # 檢查智能體安裝狀態
```

### 互動模式

啟動互動式 REPL 工作階段：

```bash
pnpm cli
```

可用命令：

| 命令            | 描述                             |
| --------------- | -------------------------------- |
| `/help`         | 顯示說明                         |
| `/new [cwd]`    | 建立新工作階段（預設：目前目錄） |
| `/sessions`     | 列出所有工作階段                 |
| `/resume <id>`  | 透過 ID 前綴恢復工作階段         |
| `/delete <id>`  | 刪除工作階段                     |
| `/history`      | 顯示目前工作階段的訊息歷史       |
| `/agent <name>` | 切換到其他智能體                 |
| `/agents`       | 列出可用智能體                   |
| `/doctor`       | 檢查智能體安裝狀態               |
| `/status`       | 顯示目前狀態                     |
| `/cancel`       | 取消目前請求                     |
| `/quit`         | 退出 CLI                         |

### 單次提問

傳送單個提示並退出：

```bash
pnpm cli prompt "2+2等於多少？"
pnpm cli prompt "列出檔案" --cwd=/tmp
```

### 選項

| 選項         | 描述                              |
| ------------ | --------------------------------- |
| `--cwd=PATH` | 智能體的工作目錄                  |
| `--log`      | 將工作階段日誌儲存到 `logs/` 目錄 |
| `--log=PATH` | 將工作階段日誌儲存到指定檔案      |

## 開發

```bash
# 以開發模式啟動 Electron 應用程式
pnpm dev

# 型別檢查
pnpm typecheck

# 執行測試
pnpm test
```

## 建置

```bash
pnpm build:mac      # macOS
pnpm build:win      # Windows
pnpm build:linux    # Linux
```

## 架構

```
Multica (Electron)
+-- 渲染程序 (React)
|   +-- UI 元件（聊天、設定等）
|
+-- 主程序
|   +-- Conductor（協調智能體通訊）
|   |   +-- SessionStore（工作階段持久化）
|   |   +-- ClientSideConnection（ACP SDK）
|   |         +-- AgentProcess（子程序管理）
|   |               +-- opencode/codex-acp/gemini (stdio)
|   |
|   +-- IPC 處理器（工作階段、智能體、設定）
|
+-- Preload (contextBridge)
    +-- electronAPI（暴露給渲染程序）
```

### 工作階段管理

Multica 在 ACP 之上維護自己的工作階段層：

```
~/.multica/sessions/
+-- index.json              # 工作階段列表（快速載入）
+-- data/
    +-- {session-id}.json   # 完整工作階段資料 + 更新
```

**關鍵設計決策：**

- **客戶端儲存**：Multica 儲存原始的 `session/update` 資料用於 UI 展示
- **智能體無關**：每個智能體獨立管理自己的內部狀態
- **恢復行為**：建立新的 ACP 工作階段，在 UI 中顯示儲存的歷史紀錄

## 授權條款

Apache-2.0
