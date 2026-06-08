# Issue Voice Capture — Design

## Goal

在创建 issue 时提供录音输入能力，让用户可以把语音快速转成 title / description 草稿。第一版采用 Cloudflare 非流式转录，并通过后端 provider 抽象为后续豆包非流式和豆包流式能力预留扩展点。

## Non-Goals

- 不新增 issue schema 字段。
- 不新增 voice note、transcription job 或 transcript history 表。
- 不做 WebSocket 流式转录。
- 不做 comment、reply、issue detail、bulk import 的语音输入。
- 不把语音转录接入现有文本 AI settings。
- 不做音频播放器或 markdown 内联音频渲染。
- 不自动把 transcript 变成摘要、标签、排期或结构化 issue。

## Current Architecture Baseline

当前实现基线：

- `apps/workspace/src/features/modals/create-issue.tsx#CreateIssueModal` 持有创建 issue 的 title、description、status、priority、assignee、project、parent、date 等草稿状态，并在 `handleSubmit` 中调用 `createIssue`。
- `apps/workspace/src/shared/api/client.ts#createIssue` 通过 `POST /api/issues` 创建 issue。
- `apps/workspace/src/shared/hooks/use-file-upload.ts#useFileUpload` 封装了前端文件上传，并支持传入 `issueId`。
- `apps/workspace/src/shared/api/client.ts#uploadFile` 使用 multipart form 上传文件，并在存在 `issueId` 时追加 `issue_id` 字段。
- `server/internal/handler/file.go#UploadFile` 读取 multipart 文件，支持 `issue_id` / `comment_id`，并创建 attachment 记录。
- `server/internal/storage/s3.go#isPreviewable` 允许 `audio/*` 作为可预览类型。
- `server/internal/handler/ai.go#AISettings` 与 `buildLLMClient` 面向文本 LLM，不包含音频转录 provider、模型、音频格式或流式能力。
- `apps/workspace/src/components/markdown/Markdown.tsx#createComponents` 不包含 `audio` renderer。

## Gap Definition

当前缺口：

1. 前端缺少录音状态机和麦克风权限处理。
2. 前端缺少把 transcript 映射进 issue 草稿的 review 流程。
3. 后端缺少音频转录接口。
4. 后端缺少与文本 AI settings 独立的 transcription provider 抽象。
5. 现有 OpenSpec 旧版本偏向浏览器原生转录，需要反向同步为 Cloudflare 非流式第一版。

## Recommended Architecture

第一版采用非流式 HTTP 转录接口。

```text
+------------------+       +------------------------+
| CreateIssueModal |       | useIssueVoiceRecorder  |
+------------------+       +------------------------+
          |                              |
          | start / stop recording       |
          v                              v
+------------------+       +------------------------+
|  MediaRecorder   | ----> | audio Blob / File      |
+------------------+       +------------------------+
          |
          | POST multipart file
          v
+-------------------------+
| POST /api/transcriptions|
+-------------------------+
          |
          v
+-------------------------+
| TranscriptionService    |
+-------------------------+
          |
          v
+-------------------------+
| TranscriptionProvider   |
+-------------------------+
          |
          | provider=cloudflare
          v
+-------------------------+
| Cloudflare Workers AI   |
| Whisper model           |
+-------------------------+
          |
          | transcript text
          v
+-------------------------+
| CreateIssueModal review |
+-------------------------+
```

创建 issue 和可选附件保存保持现有链路：

```text
review confirmed
  |
  v
title / description draft
  |
  v
POST /api/issues
  |
  +-- keep recording = false --> done
  |
  +-- keep recording = true
        |
        v
      POST /api/upload-file
        form: file + issue_id
```

## Provider Model

后端新增 provider interface，第一版只实现 Cloudflare。

```text
TranscriptionProvider
  |
  +-- CloudflareTranscriptionProvider  (Phase 1)
  |
  +-- DoubaoFileTranscriptionProvider  (future)
  |
  +-- DoubaoStreamTranscriptionProvider (future, WebSocket/session)
```

推荐 Go interface：

```go
type TranscriptionProvider interface {
    Transcribe(ctx context.Context, input TranscriptionInput) (TranscriptionResult, error)
}
```

推荐数据结构：

```go
type TranscriptionInput struct {
    Filename    string
    ContentType string
    Data        []byte
}

type TranscriptionResult struct {
    Text            string
    Provider        string
    Model           string
    DurationSeconds *float64
}
```

## API Contract

### POST /api/transcriptions

受保护路由，要求登录和 workspace 上下文。

Request:

```text
Content-Type: multipart/form-data

file: audio/webm | audio/mp4 | audio/mpeg | audio/wav
```

Response:

```json
{
  "text": "transcribed text",
  "provider": "cloudflare",
  "model": "@cf/openai/whisper-large-v3-turbo",
  "duration_seconds": 12.3
}
```

Error cases:

- `400`: missing file, unsupported content type, file too large, transcript empty.
- `401`: unauthenticated.
- `403`: no workspace access.
- `413`: audio file exceeds transcription size limit.
- `424`: transcription provider not configured.
- `502`: provider request failed.

## Configuration

Phase 1 使用环境变量，避免扩大 workspace 设置页和密钥管理范围。

```text
TRANSCRIPTION_PROVIDER=cloudflare
CLOUDFLARE_ACCOUNT_ID=...
CLOUDFLARE_API_TOKEN=...
CLOUDFLARE_TRANSCRIPTION_MODEL=@cf/openai/whisper-large-v3-turbo
TRANSCRIPTION_MAX_BYTES=26214400
```

配置规则：

- `TRANSCRIPTION_PROVIDER` 为空时禁用服务端转录。
- `TRANSCRIPTION_PROVIDER=cloudflare` 时必须存在 Cloudflare account id 和 API token。
- `CLOUDFLARE_TRANSCRIPTION_MODEL` 为空时使用 `@cf/openai/whisper-large-v3-turbo`。
- `TRANSCRIPTION_MAX_BYTES` 为空时默认 25 MB。

不复用：

```text
AISettings.provider
AISettings.api_key
AISettings.base_url
AISettings.model
DEEPSEEK_API_KEY
```

## Frontend Design

新增两个 hook，分离录音和转录。

### useIssueVoiceRecorder

职责：

- 探测 `navigator.mediaDevices.getUserMedia` 和 `MediaRecorder`。
- 选择支持的 mime type，优先 `audio/webm`。
- 管理麦克风权限和录音生命周期。
- 暴露 audio blob、duration、error。
- 不调用后端，不关心 provider。

### useIssueTranscription

职责：

- 接收 audio `File` 或 `Blob`。
- 调用 `api.transcribeAudio`。
- 暴露 `text`、`status`、`error`。
- 不关心 title / description 映射。

### CreateIssueModal

职责：

- 渲染麦克风按钮、录音中状态、转录中状态、review UI。
- 在 review 中展示 transcript。
- 执行 transcript 到 issue 草稿的映射。
- 创建 issue 成功后，按用户选择上传原始录音。

## UI State Machine

```text
              +-------------+
              | unsupported |
              +-------------+
                     ^
                     |
capability missing   |
                     |
+------+   start   +-----------------------+
| idle | --------> | requesting-permission |
+------+           +-----------------------+
   ^                         |
   |                         | permission granted
   |                         v
   |                  +-----------+
   |                  | recording |
   |                  +-----------+
   |                         |
   |                         | stop
   |                         v
   |                 +--------------+
   |                 | transcribing |
   |                 +--------------+
   |                   |          |
   |       success     |          | failure
   |                   v          v
   |                +--------+  +-------+
   |                | review |  | error |
   |                +--------+  +-------+
   |                   |          |
   | insert / discard  |          | retry / dismiss
   +-------------------+----------+
```

Issue submit with optional audio:

```text
+----------------+
| ready to submit|
+----------------+
        |
        v
+----------------+
| create issue   |
+----------------+
   |          |
   | fail     | success
   v          v
+------+   +---------------------+
|error |   | keep recording ?    |
+------+   +---------------------+
              |             |
              | no          | yes
              v             v
            +------+   +----------------+
            | done |   | upload audio   |
            +------+   +----------------+
                         |           |
                         | success   | fail
                         v           v
                       +------+   +----------------------+
                       | done |   | issue done, audio err |
                       +------+   +----------------------+
```

## Field Mapping Rules

### Rule 1: Title is empty

- Extract the first sentence or first line as title candidate.
- Append the full transcript to description.
- If the title candidate is too short, leave title empty and show a manual title hint.

### Rule 2: Title already exists

- Do not overwrite title.
- Append the full transcript to description.

### Rule 3: Description already exists

- Append transcript after a blank line.
- Do not remove existing description content.

### Rule 4: User discards review

- Do not mutate title or description.
- Keep manual issue creation available.

## Security And Privacy

- Microphone access must be triggered by explicit user action.
- `POST /api/transcriptions` must be authenticated and workspace-scoped.
- Do not upload raw audio before the user stops recording.
- Do not save raw audio as attachment unless the user chooses to keep it and creates the issue.
- Do not log audio bytes or transcript text.
- Limit request body size for transcription separately from generic file upload.
- Return provider errors without exposing provider API keys or raw upstream responses.

## Error Handling

- Permission denied: show a microphone permission error and return to idle/error state.
- Recording unsupported: disable or hide the voice control with a clear tooltip.
- Transcription disabled: show provider-not-configured error and keep manual creation usable.
- Provider failure: keep the recorded audio in review/error state so user can retry or discard.
- Empty transcript: tell user no speech was detected and allow retry.
- Attachment upload failure: issue remains created; show a toast that text was saved but recording was not preserved.

## Future Streaming Design

Streaming is intentionally outside Phase 1. The future stream path should add a separate session API instead of overloading `POST /api/transcriptions`.

```text
CreateIssueModal
  |
  v
useIssueVoiceRecorder
  |
  v
useIssueStreamingTranscription
  |
  v
WS /api/transcriptions/stream
  |
  v
DoubaoStreamTranscriptionProvider
  |
  v
partial transcript events
  |
  v
same CreateIssueModal review model
```

This keeps the frontend user experience compatible: recording, transcript preview, review, insert, and optional attachment stay the same.

## Testing Strategy

- Unit tests for transcript-to-draft mapping.
- Hook tests for recorder state transitions with mocked `MediaRecorder`.
- Hook tests for non-streaming transcription success/failure.
- Handler tests for missing file, unsupported content type, provider disabled, provider success, provider failure.
- Component tests for create issue modal voice states and optional attachment upload path.
- E2E can use mocked browser APIs and mocked transcription endpoint; no real microphone dependency.

## Acceptance Checks

- AC1: A logged-in workspace member can open create issue, start recording, stop recording, and see a non-streaming transcription progress state.
- AC2: A successful transcription returns transcript text to review before mutating title or description.
- AC3: Empty-title insertion uses the first sentence or first line as the title candidate and appends the full transcript to description.
- AC4: Existing-title insertion preserves the title and appends transcript to description.
- AC5: Existing-description insertion appends transcript after a blank line without deleting existing content.
- AC6: Discarding review leaves the current issue draft unchanged.
- AC7: Unsupported recording, denied permission, disabled provider, and provider failure do not block manual issue creation.
- AC8: `POST /api/transcriptions` is authenticated, workspace-scoped, multipart, and non-streaming.
- AC9: Backend transcription uses `TranscriptionProvider` and does not reuse text AI settings or `DEEPSEEK_API_KEY`.
- AC10: Cloudflare provider returns a normalized transcript response when configured.
- AC11: Missing transcription config returns a provider-not-configured error.
- AC12: Raw recording is uploaded as attachment only after issue creation succeeds and only when the user opts in.
- AC13: Attachment upload failure after issue creation does not roll back the issue and shows a distinct warning.
- AC14: Phase 1 does not implement WebSocket streaming or partial transcript chunk semantics.
- AC15: Create issue UI remains provider-agnostic.
- AC16: The design contains clear ASCII data flow and state machine diagrams.
