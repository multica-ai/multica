# Issue Voice Capture — Proposal

## Problem

Multica 的 issue 创建流程目前已经支持标题、富文本描述、属性设置和文件上传，但记录方式仍然以手动输入为主，导致“先记下来再说”的效率不够高。

当前现状有几个明确证据：

- `apps/workspace/src/features/modals/create-issue.tsx` 中的 `CreateIssueModal` 负责当前 issue 创建主流程，只提供标题输入、描述编辑器、属性工具栏和文件上传入口，没有语音采集或语音转录能力。
- `apps/workspace/src/shared/hooks/use-file-upload.ts` 中的 `useFileUpload` 已经支持通用文件上传。
- `server/internal/handler/file.go` 中的 `UploadFile` 已经支持通用文件上传，且底层存储路径允许 `audio/*` 类型文件。
- `server/internal/handler/ai.go` 中的 `buildLLMClient` 只覆盖文本类 chat completion，没有现成的音频转录接口。
- `apps/workspace/src/components/markdown/Markdown.tsx` 中的 `createComponents` 会把普通链接渲染为链接，不会自动把音频链接升级为播放器。

这意味着用户现在要快速记录一个想法、会议结论、临时待办或移动中产生的任务时，必须：

1. 打开创建 issue 弹窗
2. 手动输入标题
3. 手动输入描述
4. 如有语音素材，另行保存，不会自然回到 issue 创建流程

这和当前产品正在强化的“任务优先 + 更低记录成本 + 个人执行闭环”方向不一致。

## Proposed Solution

为 issue 创建流程增加 **语音录入能力**，让用户可以在创建 issue 时：

1. 直接通过麦克风录音
2. 在支持的浏览器中实时或准实时获得转录文本
3. 把转录文本直接写入标题和描述草稿
4. 在需要时保留原始录音，作为 issue 的附件一并保存

为了控制复杂度，这个功能按两个阶段落地：

### Phase 1: 浏览器原生语音录入

- 在 `CreateIssueModal` 中增加语音输入入口
- 使用浏览器原生录音与语音识别能力进行采集和转录
- 如果标题为空，则把转录结果的第一句或第一行作为标题候选，把完整转录写入描述草稿
- 如果标题已有内容，则把完整转录追加到描述草稿
- 用户可以选择是否保留原始录音；若保留，则在 issue 创建成功后把录音文件作为 issue attachment 上传

### Phase 2: 后端转录增强

- 在浏览器不支持原生语音识别，或产品需要更稳定转录质量时，引入后端音频转录能力
- 后端音频转录不直接复用当前 chat-completion 配置，而是引入独立的 transcription provider 配置模型
- 前端根据能力探测优先选择浏览器原生转录；不可用时可退化到“录音上传 + 服务端转录”

## Scope

**In scope:**

- 在 issue 创建弹窗中增加语音录入入口
- 语音转文本后写入 title / description 草稿
- 可选保留原始录音为 issue 附件
- 浏览器能力探测、权限提示、失败提示和降级路径
- 为后续服务端转录预留产品和接口设计
- PRD、OpenSpec proposal / design / spec / tasks 工件

**Out of scope for initial implementation:**

- issue 详情页、comment composer、bulk import、移动端 App 的语音录入
- 通用聊天式语音助手
- 自动把录音内容摘要成结构化 issue，而不经过用户确认
- 音频播放器富媒体渲染
- 把所有语音文件自动嵌入 description markdown
- 直接把当前 workspace 的文本 AI 配置当作语音转录配置使用

## Why Now

这个需求直接服务于当前产品目标里的三个方向：

1. **降低任务记录成本**：让“先把事情记下来”更快发生
2. **增强个人执行闭环**：把临时想法、阻塞、待办快速收拢进 issue，而不是漂在外部语音工具里
3. **保留 agent / AI 增强层的正确位置**：第一版先解决 capture，不把问题过早泛化成聊天或知识库

## Open Questions

1. 语音转录后的标题切分规则是否需要支持多语言自定义，还是先采用固定的“第一句 / 第一行”规则？
2. 原始录音是否默认保留，还是默认不保留、由用户显式开启？
3. 服务端转录未来是绑定 workspace 级配置，还是允许用户个人级配置？
4. 浏览器原生语音识别不稳定时，是否允许“仅录音不转录”的弱降级模式先发布？