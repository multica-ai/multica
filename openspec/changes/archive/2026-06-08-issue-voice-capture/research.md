# Issue Voice Capture — Research

## Research Goal

确认当前 issue 创建、附件上传、AI 设置和 markdown 渲染现状，并把录音功能的供应商和流式决策建立在可验证证据上。

## Current Code Evidence

### Create issue flow

- Evidence: `apps/workspace/src/features/modals/create-issue.tsx#CreateIssueModal`
- Finding: `CreateIssueModal` 是创建 issue 的主承载面，持有 title、description、status、priority、assignee、project、parent、date 等状态。
- Evidence: `apps/workspace/src/features/modals/create-issue.tsx#handleSubmit`
- Finding: `handleSubmit` 调用 `createIssue` 创建 issue，并从 `descEditorRef.current?.getMarkdown()` 读取 description。
- Evidence: `apps/workspace/src/features/modals/create-issue.tsx#FileUploadButton`
- Finding: 现有 footer 已有文件上传入口，但没有麦克风、录音或转录入口。

### Frontend upload flow

- Evidence: `apps/workspace/src/shared/hooks/use-file-upload.ts#useFileUpload`
- Finding: `useFileUpload` 封装上传状态和 toast，支持传入 `issueId` / `commentId`。
- Evidence: `apps/workspace/src/shared/api/client.ts#uploadFile`
- Finding: `uploadFile` 使用 multipart form，并在 `opts.issueId` 存在时追加 `issue_id`。

### Backend upload flow

- Evidence: `server/internal/handler/file.go#UploadFile`
- Finding: `UploadFile` 读取 multipart `file`，检测 content type，上传 storage，并在 workspace 上下文存在时创建 attachment。
- Evidence: `server/internal/handler/file.go#UploadFile`
- Finding: `UploadFile` 从 form 读取 `issue_id` 和 `comment_id`，因此 issue 创建成功后补传音频附件是现有链路支持的。
- Evidence: `server/internal/storage/s3.go#isPreviewable`
- Finding: `isPreviewable` 对 `image/`、`video/`、`audio/` 返回 true，音频类型不会被存储层视为不可预览内容。

### Text AI settings are not transcription settings

- Evidence: `server/internal/handler/ai.go#AISettings`
- Finding: `AISettings` 包含 provider、API key、base URL、model、label rules，语义是 workspace 文本 AI 配置。
- Evidence: `server/internal/handler/ai.go#buildLLMClient`
- Finding: `buildLLMClient` 默认 DeepSeek chat completion，并构造 OpenAI-compatible 文本 LLM client。
- Conclusion: 不能把现有文本 AI settings 当作音频转录配置复用。

### Markdown rendering

- Evidence: `apps/workspace/src/components/markdown/Markdown.tsx#createComponents`
- Finding: markdown renderer 对 `img` 和普通 `a` 有定制渲染，但没有 `audio` renderer。
- Conclusion: 第一版不应把“保留录音”定义成 description 内联播放器，应使用现有 attachment 链路。

## Provider Research

### Cloudflare

- Evidence: Cloudflare Workers AI pricing documents list Whisper large-v3-turbo audio pricing.
- Finding: Cloudflare Whisper 非流式转录价格低，适合早期“录完后转文本”的使用量。
- Evidence: Cloudflare Workers AI Whisper model documentation.
- Finding: 官方示例和模型接口更贴近提交完整音频后返回 transcript 的非流式形态。
- Product implication: Cloudflare 是 Phase 1 非流式 provider 的合适选择。

### Doubao / Volcengine

- Evidence: Volcengine speech recognition documents include big model streaming speech recognition over WebSocket.
- Finding: 豆包/火山引擎更适合未来“边说边出字”的流式体验。
- Evidence: Volcengine also provides file-based recognition APIs.
- Finding: 豆包也可作为未来非流式 provider 备选，但第一版没有必要同时实现多个 provider。
- Product implication: 豆包流式应作为 Phase 2 或 fallback decision，不进入 Phase 1 实现。

## Data Flow Baseline

Current create issue flow:

```text
CreateIssueModal
  |
  v
createIssue()
  |
  v
ApiClient.createIssue
  |
  v
POST /api/issues
```

Current optional attachment flow:

```text
FileUploadButton / ContentEditor
  |
  v
useFileUpload.uploadWithToast
  |
  v
ApiClient.uploadFile
  |
  v
POST /api/upload-file
  |
  v
UploadFile
  |
  v
CreateAttachment(issue_id or comment_id)
```

Target Phase 1 voice flow:

```text
CreateIssueModal
  |
  v
MediaRecorder
  |
  v
POST /api/transcriptions
  |
  v
TranscriptionProvider(cloudflare)
  |
  v
transcript
  |
  v
CreateIssueModal review
```

## Boundary Conditions

- Browser recording support varies; feature must degrade without breaking manual creation.
- Non-streaming transcription means transcript appears only after recording stops.
- Transcription request size should be smaller and more controlled than the generic 100 MB file upload limit.
- Provider errors must not leak credentials or raw upstream responses.
- Raw recording should not be persisted unless user chooses to preserve it after issue creation.

## Open Questions

1. Exact max recording duration and max transcription upload size.
2. Whether "keep original recording" defaults on or off.
3. Whether Phase 1 provider config remains global env-only or needs workspace override soon.
4. Which provider should be first for Phase 2 streaming if non-streaming UX is insufficient.
