import { zhCNAgentGuiAddContent } from "./zh-CN.agentGuiAddContent.ts";
import { zhCNAgentGuiCollaboration } from "./zh-CN.agentGuiCollaboration.ts";
import { zhCNAgentGuiComposer } from "./zh-CN.agentGuiComposer.ts";
import { zhCNAgentGuiHomeSuggestions } from "./zh-CN.agentGuiHomeSuggestions.ts";
import { zhCNAgentGuiModelPlans } from "./zh-CN.agentGuiModelPlans.ts";
import { zhCNAgentGuiProjectLaunch } from "./zh-CN.agentGuiProjectLaunch.ts";
import { zhCNAgentGuiProviderIdentity } from "./zh-CN.agentGuiProviderIdentity.ts";
import { zhCNAgentGuiQuickPrompts } from "./zh-CN.agentGuiQuickPrompts.ts";
import { zhCNAgentGuiReferencePicker } from "./zh-CN.agentGuiReferencePicker.ts";
import { zhCNAgentGuiRuntimeNotices } from "./zh-CN.agentGuiRuntimeNotices.ts";
import { zhCNAgentGuiSessionActions } from "./zh-CN.agentGuiSessionActions.ts";
import { zhCNAgentGuiSide } from "./zh-CN.agentGuiSide.ts";
import { zhCNAgentGuiSlashPalette } from "./zh-CN.agentGuiSlashPalette.ts";
export const zhCNAgentGui = {
  imageDownloaded: "图片已下载",
  imageLoadFailed: "图片加载失败",
  imageTemporarilyUnavailable: "图片暂时无法查看",
  retryImage: "重试",
  codexSaverModeLabel: "Codex 省额度模式",
  codexSaverModeDescription:
    "主模型保持不变；合适的独立子任务改用 Luna Max，按当前额度口径约为 Sol High 的 1/10。实际效果与速度因任务而异。",
  rtkSaverModeLabel: "RTK 省钱模式",
  rtkSaverModeDescription:
    "保持当前模型，并为新会话注入 RTK.md 与会话私有 rtk 指令，用更精简的工具输出节省 token",
  initialPlaceholder: "输入 @ 引用会话、任务和智能体",
  followupPlaceholder: "要求 {{provider}} 继续后续变更",
  installRequiredPlaceholder: "请先连接 {{provider}}，然后再发送消息",
  installRequiredAction: "安装",
  providerGateCheckingTitle: "正在检查 Agent",
  providerGateCheckingDescription:
    "稍等一下，我们正在确认 {{provider}} 是否已经可用。",
  providerGateCheckingAgentsDescription:
    "稍等一下，我们正在确认 agents 是否已经可用。",
  providerGateInstallTitle: "先连接 {{provider}}",
  providerGateInstallDescription:
    "需要先连接 {{provider}}，才能在这里开始新的对话。",
  providerGateInstallAction: "安装",
  providerGateLoginTitle: "登录 {{provider}}",
  providerGateLoginDescription: "使用账号登录后即可开始使用 {{provider}} 对话",
  providerGateLoginAction: "登录",
  providerGateModelPlanAction: "使用自己的模型",
  providerGateComingSoonTitle: "{{provider}} 即将上线",
  providerGateComingSoonDescription:
    "{{provider}} 暂未开放。准备好后即可在这里使用这个 Agent。",
  providerGateComingSoonAction: "coming soon",
  providerGateUnavailableTitle: "{{provider}} 暂时还不可用",
  providerGateUnavailableDescription:
    "我们还不能确认 {{provider}} 已准备好，可以再检测一次。",
  providerGateRetryAction: "重新检测",
  providerGateRuntimeSelectionTitle: "选择要使用的 {{provider}}",
  providerGateRuntimeSelectionDescription:
    "检测到多个 {{provider}}，选择其中一个即可继续使用。",
  providerGateRuntimeSelectionAction: "去选择",
  providerGatePendingInstall: "正在连接…",
  providerGatePendingLogin: "正在打开登录…",
  providerGatePendingRefresh: "正在检测…",
  targetSetupTitle: "设置 {{provider}}",
  targetSetupDescription:
    "已经安装了 {{provider}}？Tutti 可以直接使用，也可以为你安装",
  targetSetupAuthRequired: "运行时已安装并通过 ACP 检测，但仍需完成登录",
  targetSetupReady: "已检测到运行时，可重新检测或重新登录",
  targetSetupOpen: "打开设置",
  targetSetupRemaining: "完成以下步骤后即可使用 {{provider}}",
  targetSetupComplete: "{{provider}} 已就绪",
  targetSetupLoggedInAccount: "已登录账号",
  targetSetupStage: {
    detect: "检测运行时",
    install: "安装运行时",
    login: "登录"
  },
  targetSetupChecking: "正在检测本地和 Tutti 托管运行时…",
  targetSetupInstall: "安装运行时",
  targetSetupReinstall: "重新安装运行时",
  targetSetupStarting: "正在启动安装…",
  targetSetupAuthMethod: "登录方式",
  targetSetupAuthenticate: "继续登录",
  targetSetupReauthenticate: "重新登录",
  targetSetupAuthStarting: "正在打开登录…",
  targetSetupAuthFailed: "登录未完成",
  targetSetupNoAuthMethods: "运行时未返回支持的登录方式，请重新检测",
  targetSetupTerminalAuthHint:
    "该登录方式需要在终端中完成。在终端运行以下命令，按提示完成登录后，回到这里点击「重新检测」",
  targetSetupCopyCommand: "复制命令",
  targetSetupCommandCopied: "已复制",
  targetSetupTerminalLoginStart: "开始登录",
  targetSetupTerminalLoginWaiting:
    "已在工作台中打开终端，请在终端中按提示完成登录，这里会自动检测登录状态",
  targetSetupTerminalLoginCancel: "取消",
  targetSetupTerminalLoginTimedOut: "等待登录完成超时，请重试",
  targetSetupTerminalLoginUnavailable:
    "无法在此窗口打开终端，请复制命令后在自己的终端中执行",
  targetSetupRetry: "重新检测",
  targetSetupFailed: "运行时设置失败",
  targetSetupPhase: {
    preparing: "正在准备安装…",
    installing: "正在安装固定版本…",
    verifying: "正在验证运行时版本…",
    probing: "正在检测 ACP 兼容性…",
    activating: "正在启用托管运行时…",
    authenticating: "正在等待登录完成…",
    complete: "安装完成"
  },
  collaboratorSessionReadOnlyPlaceholder: "非当前用户会话，不可直接对话",
  send: "发送",
  modelLabel: "模型",
  modelSelectionLabel: "模型选择",
  defaultModel: "默认模型",
  ...zhCNAgentGuiComposer,
  reasoningLabel: "推理强度",
  reasoningDegreeLabel: "推理程度",
  reasoningOptionDefault: "默认",
  reasoningOptionMinimal: "最低",
  reasoningOptionLow: "低",
  reasoningOptionMedium: "中",
  reasoningOptionHigh: "高",
  reasoningOptionXHigh: "超高",
  reasoningOptionMax: "最高",
  reasoningOptionUltra: "极致",
  speedLabel: "速度",
  speedSelectionLabel: "速度",
  speedOptionStandard: "标准",
  speedOptionStandardDescription: "标准速度",
  speedOptionFast: "快速",
  speedOptionFastDescription: "1.5 倍速度，用量增加",
  permissionModeReadOnly: "请求批准",
  permissionModeAuto: "替我审批",
  permissionModeFullAccess: "完全访问权限",
  permissionModeChangeUnavailableDuringTurn: "无法在运行过程中切换权限",
  fullAccessWarning: {
    title: "确定启用完全访问权限吗？",
    description: "完全访问权限允许 Codex 无需你的批准即可操作这台电脑。",
    filesTitle: "文件和文件夹",
    filesDescription: "读取、创建、修改或删除这台电脑上任意位置的文件。",
    commandsTitle: "终端命令",
    commandsDescription: "运行命令并更改系统设置。",
    internetTitle: "互联网访问",
    internetDescription: "访问网站并通过互联网发送数据。",
    riskDescription:
      "近期版本的 Codex，尤其在使用 GPT-5.6 系列模型时，可能会超出你的意图，误删或覆盖文件。仅在你理解并接受此风险时继续。",
    learnMore: "了解详情",
    cancel: "取消",
    confirm: "启用完全访问权限"
  },
  fullAccessRestoredWarning: {
    title: "完全访问权限已开启",
    description:
      "Codex 可以在未经你同意的情况下运行命令、使用互联网，以及在这台电脑的任意位置创建、修改、上传或删除文件。这可能导致数据丢失，也会带来提示词注入风险。",
    dontShowAgain: "不再显示",
    dismissLabel: "关闭完全访问权限警告"
  },
  permissionSemantics: {
    "ask-before-write": {
      label: "请求批准",
      description: "编辑外部文件和使用互联网时始终询问"
    },
    "accept-edits": {
      label: "接受编辑",
      description: "允许直接编辑文件，但更高风险操作仍会先询问你"
    },
    "locked-down": {
      label: "不再询问",
      description: "不会弹出确认；未获允许的操作会被直接拒绝"
    },
    auto: {
      label: "替我审批",
      description: "仅对检测到的风险操作请求批准"
    },
    "full-access": {
      label: "完全访问权限",
      description: "可不受限制地访问互联网和您电脑上的任何文件"
    },
    unconfigurable: {
      label: "固定模式",
      description: "这个 provider 当前不支持在这里调整权限模式"
    }
  },
  permissionModes: {
    codex: {
      "read-only": {
        label: "请求批准",
        description: "编辑外部文件和使用互联网时始终询问"
      },
      auto: {
        label: "替我审批",
        description: "仅对检测到的风险操作请求批准"
      },
      "full-access": {
        label: "完全访问权限",
        description: "可不受限制地访问互联网和您电脑上的任何文件"
      }
    },
    cursor: {
      "read-only": {
        label: "只读",
        description: "Cursor 只读取和规划，提出修改建议但不做任何更改。"
      },
      agent: {
        label: "请求批准",
        description: "完整工具权限；Cursor 在运行命令等高风险操作前会先询问你。"
      },
      "full-access": {
        label: "完全访问",
        description:
          "无需询问直接运行命令，除非被你的 Cursor 权限规则明确拒绝。"
      }
    },
    opencode: {
      "read-only": {
        label: "只读",
        description:
          "允许读取和搜索本地项目，拒绝修改、命令、网络及其他需授权操作"
      },
      ask: {
        label: "询问",
        description: "读取和搜索直接执行，修改、命令、网络及其他操作会先询问你"
      },
      "full-access": {
        label: "完全访问",
        description:
          "自动允许需授权操作，但不会改变 OpenCode 独立的 Plan 模式限制"
      }
    },
    nexight: {
      "read-only": {
        label: "请求批准",
        description: "编辑外部文件或使用互联网前始终询问你"
      },
      auto: {
        label: "替我审批",
        description: "仅在检测到可能不安全的操作时询问你"
      },
      "full-access": {
        label: "完全访问",
        description: "可不受限制地访问互联网和你电脑上的任何文件"
      }
    },
    "claude-code": {
      default: {
        label: "默认权限",
        description: "默认较保守；需要执行修改或高风险操作时会先询问你。"
      },
      acceptEdits: {
        label: "接受编辑",
        description: "允许直接修改文件；遇到更高风险操作时仍会先询问你。"
      },
      dontAsk: {
        label: "不再询问",
        description: "不会弹出确认；未预先允许的操作会被直接拒绝。"
      },
      bypassPermissions: {
        label: "绕过权限",
        description: "尽量不做权限拦截，适合需要连续执行且你完全信任的任务。"
      }
    },
    hermes: {
      yolo: {
        label: "固定模式",
        description: "当前 provider 不支持在这里调整权限模式。"
      }
    }
  },
  modelContextWindowSuffix: "上下文窗口",
  modelTooltipVersionLabel: "版本",
  modelDescriptions: {
    frontierComplexCoding: "适合复杂编码、研究和真实工作场景的前沿模型",
    everydayCoding: "适合日常编码的强力模型",
    smallFastCostEfficient: "小型、快速且成本高效，适合较简单的编码任务",
    codingOptimized: "面向编码优化的模型",
    ultraFastCoding: "超快编码模型",
    professionalLongRunning: "针对专业工作和长时间运行的 Agent 优化"
  },
  permissionLabel: "运行权限",
  planModeLabel: "计划模式",
  normalModeLabel: "普通",
  normalModeDescription: "直接执行请求",
  planModeDescription: "先生成计划，再实施或拆解为 Issue",
  planModeOnLabel: "开启",
  planModeOffLabel: "关闭",
  planUnavailable: "规划模式不可用",
  queuedLabel: "排队中",
  queuePausedByUserLabel: "队列已暂停",
  sendQueuedPromptNext: "直接发送",
  editQueuedPrompt: "编辑",
  deleteQueuedPrompt: "删除",
  queuedPromptMoreActions: "更多排队操作",
  stop: "停止回复",
  stopping: "正在停止...",
  slashStatusTitle: "状态",
  slashStatusSession: "会话",
  slashStatusBaseUrl: "Base URL",
  slashStatusContext: "上下文",
  slashStatusLimits: "限制",
  slashStatusAccount: "账户",
  slashStatusProviderAccount: "{{provider}} 账户",
  slashStatusClose: "关闭",
  slashStatusFiveHourLimit: "5h limit",
  slashStatusWeeklyLimit: "7d limit",
  slashStatusLimitPercentLeft: "{{percent}}% left",
  slashStatusLimitReset: "resets {{reset}}",
  slashStatusContextValue:
    "{{percentLeft}}% 剩余（已用 {{usedTokens}} / {{totalTokens}}）",
  slashStatusContextUnavailable: "—",
  slashStatusLimitsUnavailable: "账户额度暂不可用",
  slashStatusEmptyValue: "—",
  slashStatusUsageJustUpdated: "刚刚更新",
  slashStatusUsageMinutesAgo: "{{count}} 分钟前更新",
  slashStatusUsageHoursAgo: "{{count}} 小时前更新",
  slashStatusUsageUpdating: "更新中…",
  slashStatusUsageRefreshFailed: "刷新失败",
  slashStatusUsageRefreshAria: "刷新额度用量",
  slashStatusUsageAuthRequired: "需要配置 API Key 或登录后才能继续",
  slashStatusUsageSessionExpired: "登录已过期，请重新登录",
  slashStatusUsageSubscriptionRequired: "需要开通 Coding Plan 或订阅",
  slashStatusUsageQuotaExhausted: "账户余额或使用额度已用尽",
  slashStatusUsageParseFailed: "无法解析账户状态",
  slashStatusUsageError: "无法加载账户状态",
  usageChipLabel: "上下文 {{percent}}%",
  usageTooltipLabel: "上下文用量",
  usagePopoverTitle: "上下文用量",
  usageContextWindowLabel: "上下文窗口",
  usageTokensLabel: "Token 用量",
  usageLimitsLabel: "限额",
  usageCompactAction: "压缩",
  planCardTitle: "计划",
  planCardCopy: "复制计划",
  copyCode: "复制代码",
  mermaidLoading: "正在渲染 Mermaid 图表",
  mermaidRenderFailed: "无法渲染 Mermaid 图表",
  mermaidExpand: "放大 Mermaid 图表",
  mermaidViewer: "Mermaid 图表查看器",
  mermaidPanHint: "按住空格并拖拽",
  mermaidZoomIn: "放大",
  mermaidZoomOut: "缩小",
  mermaidResetZoom: "重置视图",
  mermaidZoomPercent: "图表缩放 {{percent}}%",
  mermaidCloseViewer: "关闭图表查看器",
  planCardExpand: "展开方案",
  planCardCollapse: "收起方案",
  planImplementationLead: "是否实作此方案？",
  planImplementationConfirm: "是的，实作此方案",
  planImplementationFeedbackPlaceholder: "否，请告知应如何调整执行方式",
  planImplementationSend: "送出",
  planImplementationSkip: "留在计划模式",
  noRunningResponse: "当前没有正在运行的回复。",
  composerTextMenu: "输入框文本操作",
  pastedTextFilesHeader: "引用的粘贴文本文件：",
  pastedTextFileLine:
    '- 粘贴的文本文件 "{{preview}}"：{{path}}，继续前请先阅读此文件。',
  pastedTextAttachmentTitle: "粘贴的文本",
  pastedTextAttachmentFailed: "无法保存粘贴的文本",
  pastedTextRestoreToComposer: "在文本框中显示",
  copyMessage: "复制消息",
  selectedTextFragment: "1 个已选文本片段",
  selectedTextFragments: "{{count}} 个已选文本片段",
  forkThroughTurn: "从此轮分叉",
  forkThroughTurnPending: "正在分叉…",
  continuedFromTask: "接续自任务",
  sourceConversationNotFound: "原会话无法找到",
  copyImage: "复制图片",
  editRetryEditMessage: "编辑消息",
  editRetryCancel: "取消",
  editRetrySubmit: "保存并重试",
  editRetryProcessing: "正在更新会话历史并重试…",
  editRetryNeedsAction: "会话历史已更新，但编辑后的消息仍需恢复",
  editRetryReconcile: "核对状态",
  editRetryRetryReplacement: "重试消息",
  messageCopied: "已复制",
  promptTipsPrefix: "Tips：",
  reviewPicker: {
    title: "代码审查",
    targetLabel: "审查范围",
    searchPlaceholder: "搜索",
    noResults: "无匹配结果",
    uncommitted: "未提交的更改",
    baseBranch: "与分支比较",
    commit: "指定提交",
    custom: "自定义说明",
    branchLabel: "基准分支",
    branchPlaceholder: "选择分支",
    branchLoading: "正在加载分支…",
    branchEmpty: "未找到分支",
    commitPlaceholder: "提交 SHA",
    customPlaceholder: "描述要审查的内容",
    submit: "开始审查",
    cancel: "取消"
  },
  promptTips: {
    setWorkspace: {
      label: "指定工作区",
      prompt: "让 Agent 知道在哪里读文件、运行命令和理解代码"
    },
    useIssue: {
      label: "善用任务",
      prompt: "把需求、约束和验收标准写进任务，Agent 更容易按目标推进"
    },
    mapCurrentState: {
      label: "先梳理现状",
      prompt: "不确定怎么下手时，让 Agent 先总结当前状态、风险和下一步"
    },
    continueRecentSession: {
      label: "接力最近会话",
      prompt: "延续工作时让 Agent 先回顾最近进展、未完成任务和阻塞点"
    },
    referenceOtherAgents: {
      label: "引用其他 Agent 对话历史",
      prompt: "让上下文接力更完整，减少关键信息丢失"
    },
    controlPermissions: {
      label: "控制执行权限",
      prompt: "需要稳妥时使用「请求批准」，确认可改文件后再切到更高权限"
    }
  },
  empty: "需要 {{provider}} 帮你做些什么？",
  ...zhCNAgentGuiHomeSuggestions,
  conversations: "会话",
  newConversation: "新建会话",
  agentConfig: "检测与设置",
  agentSettingsMenu: "设置",
  agentEnvSetup: "环境检测",
  agentsEmpty: "暂无可用 Agent",
  conversationFilterAll: "全部",
  ...zhCNAgentGuiProviderIdentity,
  providerSwitchLabel: "切换 Provider",
  sharedAgentOwnerSeparator: " 的 ",
  handoffConversation: "Handoff",
  handoffConversationTooltip: "交接给其他 Agent",
  handoffConversationMenu: "选择要交接的 Agent",
  handoffTargetDeviceSource: "来自 {{device}}",
  handoffTargetSelf: "我的 Agent",
  handoffTargetShared: "共享 Agent",
  startConversation: "开始会话",
  selectConversation: "选择一个会话",
  loadingConversation: "正在加载会话...",
  scrollToBottom: "滚动至底部",
  conversationUnavailable: "会话不可用。",
  contextPickerBrowseHint: "根据你输入的内容搜索工作区文件",
  contextPickerBrowseFileHint:
    "暂无已打开或 Agent 生成的文件，继续输入文件名可搜索本机文件",
  contextPickerBrowseAgentHint: "输入内容以搜索 Agent",
  contextPickerBrowseAppHint: "输入内容以搜索应用",
  contextPickerBrowseSessionHint: "输入内容以搜索我发起的 Agent 会话",
  contextPickerBrowseCollabHint: "输入内容以搜索其他人和 Agent 的会话",
  contextPickerBrowseIssueHint: "输入内容以搜索当前房间内的任务",
  workspaceAppFactoryMentionFallback: "创建应用",
  contextPickerExpandMore: "展开更多 {{count}} 条",
  contextPickerLoadMoreLoading: "正在加载",
  contextPickerLoadMoreRetry: "加载失败，重试",
  contextPickerCategoryFileDescription: "搜索工作区文件和目录",
  contextPickerCategorySessionDescription: "查找我发起的 Agent 会话",
  contextPickerCategoryCollabDescription: "查看其他人和 Agent 的会话",
  contextPickerCategoryTaskDescription: "查找当前房间内的任务",
  searchPlaceholder: "搜索会话",
  sectionToday: "今天",
  sectionYesterday: "昨天",
  sectionEarlier: "更早",
  projectSectionEdit: "新建会话",
  projectSectionMoreActions: "项目操作",
  projectSectionViewFiles: "打开文件夹",
  pinProject: "置顶项目",
  unpinProject: "取消置顶项目",
  pinnedProjectAccessibleName: "已置顶项目：{{project}}",
  projectRailCreateProject: "新建项目",
  projectRailLinkExistingProject: "关联已有项目文件",
  removeProject: "移除",
  removeProjectConfirmDescription:
    "将会从列表中移除「{{project}}」及项目下全部会话，不会删除本地文件，是否确认移除？",
  removeProjectConfirmTitle: "移除项目？",
  batchDeleteProjectSessions: "批量删除会话",
  batchDeleteProjectSessionsTitle: "删除项目会话？",
  batchDeleteProjectSessionsBody:
    "将删除「{{project}}」下的 {{count}} 个会话。删除后无法恢复。",
  batchDeleteProjectSessionsConfirm: "删除会话",
  conversationsSectionMoreActions: "对话操作",
  batchDeleteConversations: "批量删除对话",
  batchDeleteConversationsTitle: "删除对话？",
  batchDeleteConversationsBody: "将删除 {{count}} 个对话。删除后无法恢复。",
  batchDeleteConversationsConfirm: "删除对话",
  runtimeSessionOnly: "这里只显示 runtime 会话。",
  approvalRequired: "{{provider}} 请求你的授权。",
  fileChangeApprovalRequired: "{{provider}} 请求编辑文件的授权",
  approvalUnavailable: "没有可用选项。",
  approvalOptions: {
    allowOnce: "允许执行",
    allowForSession: "本次会话允许",
    allowAlways: "允许，并且不再询问",
    allowAlwaysForCommandPrefix:
      "允许，并且不再询问以 `{{command}}` 开头的命令",
    allowAlwaysForCommandPrefixLead: "允许，并且不再询问以下列内容开头的命令",
    allowAlwaysForScope: "允许，并且不再询问：{{scope}}",
    alwaysAllowScope: "始终允许 {{scope}}",
    bypassPermissions: "允许，并绕过权限",
    autoMode: "允许，并使用自动模式",
    acceptEdits: "允许，并自动接受编辑",
    manualApproval: "允许，并手动确认编辑",
    rejectOnce: "拒绝执行",
    rejectAlways: "拒绝，并且不再询问",
    rejectAndStopTurn: "拒绝并停止当前回复",
    rejectWithFollowUp: "拒绝，然后发送新的指令"
  },
  authRequired: "需要认证",
  authLogin: "登录",
  activatingSession: "正在连接会话...",
  cancellingSession: "正在取消中...",
  retryActivation: "重试",
  continueInNewConversation: "去新会话",
  processing: "正在规划下一步",
  turnProcessedSeconds: "已处理 {{seconds}} 秒",
  turnProcessedMinutes: "已处理 {{minutes}} 分钟",
  turnProcessedMinutesSeconds: "已处理 {{minutes}} 分 {{seconds}} 秒",
  turnPeerDeviceOfflinePendingSync: "对方设备离线 · 进度待同步",
  turnPeerDeviceProgressSynchronizing: "正在同步对方设备进度…",
  turnTotalSeconds: "总用时 {{seconds}} 秒",
  turnTotalMinutes: "总用时 {{minutes}} 分钟",
  turnTotalMinutesSeconds: "总用时 {{minutes}} 分 {{seconds}} 秒",
  expandTurnWork: "展开任务详情",
  collapseTurnWork: "收起任务详情",
  agentTargetRequired: "请先选择可用的 Agent 目标。",
  sessionActivationFailed: "Agent 会话启动失败。",
  sessionNoLongerAvailable: "之前的 Agent 会话已不可用",
  promptImagesUnsupported: "当前模型不支持图片输入。",
  ...zhCNAgentGuiRuntimeNotices,
  tuttiModeCheckpointWakeTaskSettled: "某任务已完成，待审查",
  tuttiModeCheckpointWakeTaskFailed: "某任务失败，待审查",
  tuttiModeCheckpointWakeTaskCanceled: "某任务已取消，待审查",
  tuttiModeCheckpointWakeGoalReview: "待进行最终目标审查",
  tuttiModeCheckpointWakeInitialSchedule: "可以调度下一批任务了",
  tuttiModeCheckpointWakeDefault: "执行检查点待你审查",
  tuttiModeCheckpointWakeIssue: "Issue {{issue}}",
  tuttiModeCheckpointWakeExpand: "查看完整提示",
  tuttiModeCheckpointWakeCollapse: "收起完整提示",
  tuttiModePlanIssueLinkCreated: "已根据该计划创建 Issue",
  ...zhCNAgentGuiModelPlans,
  turnSummary: "已变更文件",
  userMessageLocator: "用户消息",
  turnSummaryFilesChanged: "变更了 {{count}} 个文件",
  turnSummaryModified: "{{count}} 个修改",
  turnSummaryCreated: "{{count}} 个新增",
  turnSummaryModifiedTag: "修改",
  turnSummaryCreatedTag: "新增",
  turnSummaryViaTool: "通过 {{tool}}",
  turnSummaryBefore: "变更前",
  turnSummaryAfter: "变更后",
  codeBlockEmptyContent: "(empty)",
  turnSummaryOpenFile: "打开",
  turnSummaryUndo: "撤销",
  turnSummaryReapply: "重新应用",
  turnSummaryCheckingGit: "正在检查 Git 仓库...",
  turnSummaryGitRequired: "当前目录不是 Git 仓库，无法撤销变更",
  turnSummaryPatchUnavailable: "当前变更缺少可撤销的补丁数据",
  turnSummaryInvalidPatch: "变更记录中的补丁格式无效，无法安全撤销",
  turnSummaryPatchDoesNotApply: "文件在此次变更后已发生变化，无法安全撤销",
  turnSummaryUndoFailed: "撤销变更失败",
  turnSummaryReapplyFailed: "重新应用变更失败",
  turnSummaryShowMoreFiles: "再显示 {{count}} 个文件",
  turnSummaryShowFewerFiles: "收起多余文件",
  planLead: "结束规划并开始实施。你希望权限如何工作？",
  planModes: {
    acceptEdits: {
      label: "接受编辑",
      description: "自动批准文件编辑"
    },
    askFirst: {
      label: "逐次确认",
      description: "每次工具调用前都询问"
    },
    allowAll: {
      label: "全部允许",
      description: "不再弹出工具确认"
    },
    auto: {
      label: "自动",
      description: "由智能体自行决定何时询问"
    }
  },
  stayInPlan: "继续规划",
  sendFeedback: "发送反馈并继续规划",
  feedbackPlaceholder: "补充反馈，继续完善计划...",
  previousQuestion: "上一步",
  nextQuestion: "下一步",
  submitAnswers: "提交回答",
  answerPlaceholder: "补充更多细节...",
  waitingForAnswer: "等待你的回答...",
  shortcutEnter: "Enter",
  shortcutCmdEnter: "Cmd + Enter",
  shortcutCtrEnter: "Ctr + Enter",
  openConversationWindow: "在新窗口打开会话",
  showMoreConversations: "显示更多",
  showLessConversations: "收起",
  deleteSession: "删除会话",
  pinSession: "置顶会话",
  ...zhCNAgentGuiSessionActions,
  renameSession: "重命名会话",
  renameSessionTitle: "重命名对话",
  renameSessionDescription: "保持简短且易于识别。",
  renameSessionPlaceholder: "对话标题",
  renameSessionSave: "保存",
  unpinSession: "取消置顶",
  deleteSessionTitle: "删除会话？",
  deleteSessionBody:
    "删除后无法恢复。该会话将不再出现在会话列表、会话时间线、房间时间线或房间状态中。",
  deleteSessionConfirm: "删除会话",
  collapseConversationRail: "隐藏侧边栏",
  expandConversationRail: "显示侧边栏",
  relativeTimeJustNow: "刚刚",
  relativeTimeMinutes: "{{count}} 分钟",
  relativeTimeHours: "{{count}} 小时",
  relativeTimeDays: "{{count}} 天",
  relativeTimeMonths: "{{count}} 个月",
  relativeTimeYears: "{{count}} 年",
  ...zhCNAgentGuiSlashPalette,
  slashCommandCompactLabel: "压缩",
  slashCommandContextLabel: "上下文",
  slashCommandFastLabel: "快速",

  slashCommandInitLabel: "初始化",
  slashCommandPlanLabel: "计划",
  slashCommandReviewLabel: "审查",
  slashCommandStatusLabel: "状态",
  slashCommandUsageLabel: "用量",
  slashCommandCompactDescription: "压缩当前对话上下文。",
  slashCommandContextDescription: "查看当前上下文快照。",
  slashCommandFastDescription: "切换快速响应模式。",

  slashCommandInitDescription: "初始化仓库说明文件。",
  slashCommandPlanDescription: "切换计划模式。",
  slashCommandReviewDescription: "发起代码审查。",
  slashCommandStatusDescription: "查看会话状态和上下文用量。",
  slashCommandUsageDescription: "查看上下文和额度用量。",
  browserUseCapabilityLabel: "浏览器",
  browserUseCapabilityDescription: "让 Agent 使用浏览器。",
  browserUseCapabilityDescriptionAutoConnect: "当前配置：复用已登录的 Chrome。",
  browserUseCapabilityDescriptionIsolated: "当前配置：使用独立浏览器。",
  browserUseCapabilitySettingsLabel: "浏览器设置",
  browserUseCapabilitySettingsDescription: "配置 Agent 使用的浏览器。",
  capabilityInlineSettingsLabel: "设置",
  computerUseCapabilityLabel: "电脑控制",
  computerUseCapabilityDescription: "让 Agent 控制你的电脑桌面",
  computerUseCapabilitySetupRequiredDescription: "未安装。按 Enter 打开设置。",
  computerUseCapabilityAuthorizationRequiredDescription:
    "需要授权。按 Enter 打开设置。",
  computerUseCapabilityAuthorizationUnknownDescription:
    "无法确认授权状态。按 Enter 打开设置。",
  computerUseCapabilitySettingsLabel: "电脑控制设置",
  computerUseCapabilitySettingsDescription: "安装、移除或授权电脑控制。",
  fileMentionPalette: "引用或调用",
  fileMentionLoading: "正在搜索工作区...",
  fileMentionEmpty: "根据你输入的内容搜索工作区文件",
  fileMentionError: "无法搜索工作区文件。",
  fileMentionTabHint: "Tab / ←→ 切换分类 ｜ ↑↓ 切换选中",
  fileDropHint: "拖放文件以添加到会话",
  composerFileFolderUnsupported: "暂不支持在这里附加文件夹",
  composerFileTooLarge: "文件过大",
  composerFilePreparationFailed: "文件处理失败",
  mentionPalette: "引用或调用",
  addReference: "添加引用",
  addContent: "添加文件等内容",
  ...zhCNAgentGuiAddContent,
  quickPrompts: zhCNAgentGuiQuickPrompts,
  referenceWorkspaceFiles: "引用空间文件",
  ...zhCNAgentGuiReferencePicker,
  ...zhCNAgentGuiProjectLaunch,
  fileMentionEnterFolder: "进入文件夹",
  fileMentionSwitchCategory: "切换分类",
  fileMentionNavigateHierarchy: "进入/返回文件夹",
  fileMentionSwitchSelection: "切换选中",
  mentionFilterFile: "文件",
  mentionFilterApp: "应用",
  mentionFilterAgent: "智能体",
  provenanceFilterAllAgents: "全部智能体",
  provenanceFilterAllMembers: "全部成员",
  provenanceFilterAllSources: "全部来源",
  provenanceFilterAgents: "智能体",
  provenanceFilterFilteredSources: "已筛选来源",
  provenanceFilterMembers: "成员",
  mentionFilterSession: "会话",
  mentionFilterIssue: "任务",
  mentionKindAgent: "智能体",
  mentionKindApp: "应用",
  mentionKindAppFactory: "应用工厂",
  mentionKindFile: "文件",
  mentionKindIssue: "任务",
  mentionKindReference: "引用",
  mentionKindSession: "会话",
  mentionGroupFiles: "文件",
  mentionGroupOpenedFiles: "我打开的文件",
  mentionGroupAgentGeneratedFiles: "近期 Agent 生成的文件",
  mentionGroupApps: "应用",
  mentionGroupAgents: "智能体",
  mentionGroupMySessions: "我的会话",
  mentionGroupIssues: "任务",
  mentionEmptyMySessions: "暂无会话",
  mentionEmptyApps: "暂无应用",
  mentionEmptyAgents: "暂无可用智能体",
  mentionEmptyIssues: "暂无任务",
  mentionEmptyDockFiles: "Dock 栏暂无已打开文件，输入关键词可搜索工作区文件",
  mentionEmptyAgentGeneratedFiles: "暂无 Agent 生成的文件",
  mentionFolderBack: "返回",
  mentionFolderChildCount: "包含 {{count}} 个子项",
  mentionAgentTargetAvailable: "可用",
  mentionAgentTargetUnavailable: "不可用",
  mentionNoMatchingFiles: "没有匹配到文件",
  mentionOpenReferences: "查看产物",
  issueRunPrompt: {
    currentWorkingDirectoryLabel: "当前工作目录",
    executionRequirementsLabel: "执行要求",
    intro: "你正在处理一个任务。",
    issueContentLabel: "任务内容",
    issueTitleLabel: "任务标题",
    missingContent: "（无补充内容）",
    noReferences: "- （无引用资料）",
    referencesLabel: "引用资料",
    requirementNoOtherOutputDir: "3. 不要把最终产物写到其他目录。",
    requirementStayInWorkspace:
      "1. 在 {{workspaceRoot}} 下工作，不要切换到其他无关目录。",
    requirementSummaryOutput:
      "2. 如果用户没有另行指定位置，至少输出 docs/tutti/task_summary_{{issueId}}.md，说明处理结果、改动与结论。",
    taskContentLabel: "任务内容",
    taskTitleLabel: "任务标题"
  },
  syncPending: "已保存到本地，正在同步到云端",
  syncSynced: "已同步到云端",
  syncFailed: "云端同步失败",
  ...zhCNAgentGuiSide,
  ...zhCNAgentGuiCollaboration
} as const;
