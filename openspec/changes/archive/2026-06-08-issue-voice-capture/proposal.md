# Issue Voice Capture — Proposal

## Problem

Multica 的 issue 创建流程已经支持标题、富文本描述、属性设置和文件上传，但记录方式仍然以手动输入为主。用户在临时想到一个任务、会议中捕获结论、移动中记录待办时，需要先把语音内容转成文字，再回到 issue 创建流程，成本偏高。

当前现状有明确代码证据：

- `apps/workspace/src/features/modals/create-issue.tsx#CreateIssueModal` 是当前 issue 创建主入口，已有 title、description、属性工具栏、文件上传和提交逻辑，但没有录音、转录或语音 provider 概念。
- `apps/workspace/src/shared/hooks/use-file-upload.ts#useFileUpload` 和 `apps/workspace/src/shared/api/client.ts#uploadFile` 已经支持带 `issueId` 的附件上传。
- `server/internal/handler/file.go#UploadFile` 已经支持从 multipart form 读取 `issue_id` 并创建 attachment 记录。
- `server/internal/storage/s3.go#isPreviewable` 已经把 `audio/*` 视为可预览内容类型。
- `server/internal/handler/ai.go#buildLLMClient` 当前只构造文本 chat-completion LLM client，不代表语音转录能力已经存在。
- `apps/workspace/src/components/markdown/Markdown.tsx#createComponents` 只为图片和链接定义渲染分支，没有音频播放器分支。

因此，第一版录音功能应解决“创建 issue 时更快把语音变成草稿文字”，而不是引入完整语音产品、会议转写或流式语音助手。

## Proposed Solution

为 create issue 流程增加录音输入能力，并采用 **Cloudflare 非流式转录先行、后端 transcription provider 抽象、前端 provider 无感知** 的方案。

第一版能力：

1. 用户在 `CreateIssueModal` 中点击麦克风开始录音。
2. 浏览器使用 `MediaRecorder` 采集音频。
3. 用户停止录音后，前端把音频提交到后端非流式转录接口。
4. 后端通过 `TranscriptionProvider` 抽象调用 Cloudflare Workers AI Whisper。
5. 前端拿到 transcript 后进入 review 状态，用户确认后写入 title / description 草稿。
6. 用户可选择是否保留原始录音；若保留，则 issue 创建成功后把录音作为 attachment 上传。

后续增强：

1. 如果 Cloudflare 非流式效果或等待时间不满足使用体验，可以新增豆包非流式 provider。
2. 如果需要“边说边出字”，再新增豆包流式 provider 与 WebSocket transcription session。
3. 前端录音体验保持统一，不把 Cloudflare、豆包等供应商细节泄露到 create issue UI。

## Scope

**In scope for Phase 1:**

- 在 create issue 弹窗中增加录音入口、录音状态和权限错误提示。
- 新增后端非流式转录接口 `POST /api/transcriptions`。
- 新增后端 `TranscriptionProvider` interface，并实现 Cloudflare provider。
- 使用独立环境变量配置转录能力，不复用现有文本 AI settings。
- 将 transcript 按确定性规则写入 title / description 草稿。
- 可选保留原始录音为 issue attachment。
- 补充前端、后端和 OpenSpec 文档。

**Out of scope for Phase 1:**

- 豆包 provider 的实际实现。
- WebSocket 流式转录。
- comment、reply、bulk import、issue detail description editor 的语音输入。
- 自动摘要、标签、排期建议等 AI 结构化处理。
- 新增 voice note 表、转录历史表或 issue schema 字段。
- 音频播放器富媒体渲染。
- workspace 设置页里的 transcription 密钥管理。

## Data Flow

```text
CreateIssueModal
  |
  | start recording
  v
MediaRecorder
  |
  | audio blob after stop
  v
POST /api/transcriptions
  |
  v
TranscriptionProvider
  |
  | provider=cloudflare
  v
Cloudflare Workers AI Whisper
  |
  | transcript text
  v
CreateIssueModal review
  |
  | user confirms insert
  v
title / description draft
  |
  | create issue
  v
POST /api/issues
  |
  | optional keep recording
  v
POST /api/upload-file with issue_id
```

## Why Now

这个需求直接服务于当前产品方向：

1. 降低任务捕获成本，让“先记下来”更快发生。
2. 强化个人执行闭环，把临时想法和会议结论收敛到 issue。
3. 为未来语音 provider 和流式体验留出扩展点，但第一版保持实现面可控。

## Decisions

- 第一版做非流式转录，不做 WebSocket 流式。
- 第一版 provider 使用 Cloudflare Workers AI Whisper。
- 转录 provider 独立于当前文本 AI settings。
- 前端只感知录音、转录、review 状态，不感知具体 provider。
- 原始录音默认不自动保存，只有用户确认保留并创建 issue 后才作为 attachment 上传。

## Open Questions

1. 原始录音默认是否勾选“保留”，还是默认不保留。
2. Phase 1 的最大录音时长和文件大小限制最终取值。
3. Cloudflare provider 是只用全局环境变量，还是同时支持未来 workspace-level 配置读取。
4. 未来流式能力是否优先豆包，还是先做 Cloudflare/Deepgram 等其他实时 provider 对比。
