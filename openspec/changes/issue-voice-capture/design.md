## Context

这次变更要解决的是“在创建 issue 时，通过语音更快完成捕获”的问题，而不是引入一个新的语音产品面。

当前实现里的关键事实：

- `apps/workspace/src/features/modals/create-issue.tsx#CreateIssueModal` 是当前创建 issue 的唯一主弹窗，适合作为语音入口的主承载面。
- `apps/workspace/src/shared/hooks/use-file-upload.ts#useFileUpload` 已经抽象了上传能力，因此录音文件不需要新建存储体系。
- `server/internal/handler/file.go#UploadFile` 已经支持带 `issue_id` 的附件上传，说明“创建成功后补传语音附件”是可行路径。
- `server/internal/handler/ai.go#buildLLMClient` 当前只支持 chat completion，不支持音频转录，因此不能把“已有 AI 设置”直接当成语音能力存在的证据。
- `apps/workspace/src/components/markdown/Markdown.tsx#createComponents` 当前不会对音频链接做播放器渲染，因此第一版不能假设 description 中出现录音链接就拥有完整播放体验。

## Goals / Non-Goals

**Goals:**

- 让用户在创建 issue 时可以直接用语音输入，减少手动输入成本
- 在不改 issue 数据模型的前提下，把转录文本自然接入 title / description 草稿
- 让用户可选保留原始录音，且录音和 issue 保持可追溯关系
- 用清晰的能力边界区分 Phase 1 浏览器原生方案与 Phase 2 服务端转录方案
- 保持创建流程的人在回路中，语音只是更快的输入方式，不自动替代用户确认

**Non-Goals:**

- 让语音成为一个新的独立导航模块或 inbox 模型
- 在第一版引入新的 issue schema、voice note 表、转录历史表
- 在第一版支持 comment、reply、project、search 等跨模块语音输入
- 在第一版把语音内容直接做成摘要、标签、排期建议等更高阶 AI 流程
- 在第一版承诺跨所有浏览器获得一致的实时转录质量

## Decisions

### D1: 语音输入挂在现有 CreateIssueModal，而不是新建“语音收件箱”

原因：当前 issue 已经是产品的 canonical work item。用户需求是“更快创建 issue”，不是“先创建一种语音草稿对象，再转 issue”。

结果：

- 语音入口直接出现在 `CreateIssueModal`
- 转录结果写入现有 title / description 状态
- 不引入新的 capture 对象模型

### D2: 第一版不改 issue schema，转录文本仍然写入现有 title / description

原因：`CreateIssueRequest` 目前只包含 title、description 和 issue 属性字段；让语音输入落到现有字段能最小化后端改动和跨模块扩散。

结果：

- `title` 继续是必填事实字段
- `description` 继续承接细节内容
- 不新增 `voice_transcript` 字段
- 不新增 `voice_note_id` 字段

### D3: 标题映射规则采用“标题为空则首句进标题，否则只追加到描述”

原因：用户创建 issue 时最常见的意图有两类：

1. 完全通过语音快速创建一个新 issue
2. 已经写了标题，再通过语音补充上下文

因此第一版采用确定性规则，而不是一开始引入 AI 结构化抽取：

- 若 `title` 为空：取转录文本的第一句或第一行作为标题候选
- 完整转录文本写入 description 草稿
- 若 `title` 已有值：不覆盖标题，只把完整转录追加到 description 草稿

这样既能减少用户改动，也避免把标题生成依赖到额外 AI 能力。

### D4: 原始录音采用“issue 创建成功后作为附件上传”的方式保存

原因：现有上传链路已经支持 `issue_id` 上下文，issue 创建成功后可以直接把录音文件挂到 issue 上；这比在创建前把链接嵌入 description 更干净，也更符合“记录语音”的预期。

结果：

- 录音 blob 在 create modal 内先保存在本地草稿状态
- 用户提交创建后，先调用 `createIssue`
- 若 issue 创建成功且用户选择保留原始录音，则再调用上传接口，把音频以 `issue_id` 形式关联到新 issue
- 如果录音上传失败，issue 创建仍然成功，但用户收到明确提示“文本已保存，录音未保存”

### D5: Phase 1 采用浏览器原生采集 + 浏览器原生转录的能力适配器

原因：当前仓库没有现成转录后端，直接进入服务端转录会把 feature 复杂度扩大到 provider、权限、计费、接口、重试、文件生命周期等多个面。

因此第一版采用能力适配：

- 录音：`MediaRecorder`（或等价浏览器媒体采集能力）
- 转录：`SpeechRecognition` / `webkitSpeechRecognition` 等浏览器原生能力

前端应显式探测：

- 是否支持录音
- 是否支持原生转录

并根据结果决定：

- 完整体验：录音 + 实时转录
- 弱降级：仅录音，可后续转录
- 不支持：按钮禁用并说明原因

### D6: Phase 2 的服务端转录采用独立 provider 配置，不直接复用现有 chat-completion 配置

原因：`server/internal/handler/ai.go` 当前面向的是文本 chat completion，默认 provider/模型链路并不能证明支持音频转录。直接复用会把“文本 LLM 是否可用”和“音频转录是否可用”错误绑定。

结果：

- 未来若增加服务端转录，需要在 workspace 设置中新增独立 transcription 配置，或明确的 provider capability 描述
- API 形态可以是 `POST /api/workspaces/:id/ai/transcriptions` 或同级能力接口
- 前端能力优先级为：浏览器原生转录 > 服务端转录 > 仅录音

### D7: 第一版不承诺音频播放器，默认以 issue attachment 方式保留录音

原因：当前 markdown 渲染不会自动提供播放器。若第一版把“保留录音”定义成 description 内嵌播放，会把 scope 扩展到消息渲染、播放器 UI、下载策略和跨端体验。

结果：

- 原始录音作为 attachment 保留
- issue 详情可通过现有附件入口访问录音
- 音频内联播放作为后续增强项，而不是当前基础需求的阻塞项

## User Experience Design

### Primary Flow

1. 用户打开 `CreateIssueModal`
2. 用户点击新增的麦克风按钮
3. 系统请求麦克风权限并进入 `listening` 状态
4. 若浏览器支持转录，界面实时展示 transcript preview
5. 用户停止录音
6. 系统进入 `review` 状态，显示：
   - 转录文本预览
   - 是否保留原始录音
   - 即将如何写入 title / description 的说明
7. 用户确认插入
8. 系统把文本写回当前草稿
9. 用户继续编辑或直接创建 issue
10. 若用户选择保留录音，issue 创建成功后上传语音附件

### UI States

- `idle`: 未开始
- `requesting-permission`: 正在请求麦克风权限
- `listening`: 正在录音
- `transcribing`: 停止录音后等待最终 transcript
- `review`: 展示 transcript 和保留录音选项
- `uploading-audio`: issue 创建成功后正在上传录音附件
- `unsupported`: 当前浏览器不支持录音或转录
- `error`: 权限拒绝、录音失败、转录失败、上传失败

### Field Mapping Rules

### Rule 1: Title empty

- 输入：title 为空，description 为空或有内容
- 处理：
  - 从 transcript 中提取第一句或第一行作为 title candidate
  - 把完整 transcript 追加到 description
- 用户可在提交前继续手动编辑 title 和 description

### Rule 2: Title already exists

- 输入：title 已有内容
- 处理：
  - 不覆盖 title
  - transcript 全量追加到 description

### Rule 3: Transcript too short

- 若 transcript 短到不足以形成有效 issue 标题，则仅写入 description，并提示用户手动完善标题

## Technical Design

### Frontend

新增一个专用的语音采集 hook，例如：

- `apps/workspace/src/features/issues/hooks/use-voice-issue-capture.ts`

职责：

- 封装浏览器能力探测
- 封装录音状态机
- 暴露 transcript、blob、error、capabilities
- 不直接写业务字段，只把结果返回给 `CreateIssueModal`

`CreateIssueModal` 负责：

- 渲染麦克风按钮和 review UI
- 根据 title 当前状态决定映射规则
- 在 `handleSubmit` 成功后决定是否上传语音附件

### Backend

Phase 1 不需要新增后端转录接口。

Phase 1 只依赖现有：

- `POST /api/issues`
- `POST /api/upload-file`

Phase 2 预留：

- 新增服务端音频转录接口
- 新增 transcription provider 配置读取逻辑
- 新增上传音频到转录服务的安全与大小限制策略

### Data Model

Phase 1：

- 不改数据库 schema
- 不改 `CreateIssueRequest`
- 不改 issue 读取模型
- 原始录音仅在创建成功后作为 attachment 保存

Phase 2（候选）：

- 仍优先避免新增 voice 专属表
- 如果未来需要审计 transcript 来源、转录状态或 provider 诊断，再评估独立 metadata 表

## Error Handling

### Permission denied

- 明确提示用户浏览器拒绝了麦克风权限
- 不影响已有手动创建 issue 流程

### Recording unsupported

- 若浏览器不支持录音，则隐藏或禁用语音入口，并给出说明

### Transcription unsupported

- 若浏览器支持录音但不支持转录，则允许录音，但提示“当前浏览器无法实时转录”
- 若未来服务端转录可用，可引导转为上传后转录

### Attachment upload failure

- issue 创建成功但录音上传失败时，toast 明确区分这两个结果
- 不回滚已创建的 issue

## Privacy / Security

- 麦克风权限必须由显式用户操作触发
- Phase 1 音频默认不离开浏览器，除非用户勾选“保留原始录音”并提交 issue
- Phase 2 若引入服务端转录，必须明确说明音频将被发送到哪类 provider
- 不允许在用户未确认 issue 创建前自动把录音上传到服务器

## Testing Strategy

- 单元测试：标题映射规则、状态机转换、能力探测分支
- 组件测试：CreateIssueModal 中的语音 UI 状态和提交分支
- 集成测试：issue 创建成功后附带上传录音附件的顺序
- E2E：浏览器能力可 mock 时验证主要 happy path；对不稳定浏览器 API 使用 stub 而非真实麦克风

## Risks / Trade-offs

- 浏览器原生转录兼容性不一致，Phase 1 体验会受浏览器差异影响
- 第一版没有音频播放器，保留录音更偏向审计和回看，而不是即时播放体验
- 若标题切分规则过于机械，部分 transcript 仍需用户手动修正
- 若过早把服务端转录绑定到现有 AI settings，后续 provider 能力会变得难以解释，因此本设计明确拆开

## Open Questions

1. “保留原始录音”是否默认开启？
2. 是否需要在 create modal 内展示最近一次录音的时长和大小？
3. Phase 2 的服务端转录要不要和现有 AI tab 放在一起，还是拆成独立的 transcription tab？
4. 后续是否需要把相同能力复用到 comment composer 和 daily review capture 中？