export type Lang = "en" | "zh";

/** What the simulated speaker says, as the recognizer would chunk it. */
export interface VoiceScript {
  /** Phrases are committed as final text one at a time, like a recognizer's results. */
  phrases: string[];
  /** The cleaned-up text a polish pass would return. */
  polished: string;
}

export const CHAT_SCRIPT: Record<Lang, VoiceScript> = {
  en: {
    phrases: [
      "Um, can you look at why the inbox unread count",
      " doesn't refresh after switching workspaces?",
      " I think the query key is missing the workspace ID.",
      " Please fix it, open a PR, and add a test.",
    ],
    polished:
      "Can you look at why the inbox unread count doesn't refresh after switching workspaces? I think the query key is missing the workspace ID. Please fix it, open a PR, and add a test.",
  },
  zh: {
    phrases: [
      "嗯，帮我看一下切换工作区之后",
      "收件箱未读数不刷新的问题，",
      "我怀疑是 query key 里没带 workspace ID，",
      "修一下然后开个 PR，顺便补个测试。",
    ],
    polished:
      "帮我看一下切换工作区后收件箱未读数不刷新的问题。我怀疑是 query key 里没带 workspace ID，请修复后开个 PR，并补一个测试。",
  },
};

export const ISSUE_SCRIPT: Record<Lang, VoiceScript> = {
  en: {
    phrases: [
      "New issue: on narrow screens",
      " the board view columns overflow and you can't scroll to the last one.",
      " It's high priority,",
      " assign it to Lambda, and put it in the Web project.",
    ],
    polished:
      "On narrow screens the board view columns overflow and you can't scroll to the last one.",
  },
  zh: {
    phrases: [
      "新建一个 issue：窄屏下",
      "看板视图的列会溢出，最后一列滚动不到。",
      "优先级高，",
      "指派给 Lambda，放到 Web 项目里。",
    ],
    polished: "窄屏下看板视图的列会溢出，最后一列无法滚动到。",
  },
};

export const ISSUE_FIELDS: Record<Lang, { title: string; priority: string; assignee: string; project: string }> = {
  en: { title: "Board columns overflow on narrow screens", priority: "High", assignee: "Lambda", project: "Web" },
  zh: { title: "窄屏下看板列溢出", priority: "高", assignee: "Lambda", project: "Web" },
};

export const COPY = {
  en: {
    agentName: "Lambda",
    sessionTitle: "Inbox badge after workspace switch",
    placeholder: "Message Lambda…",
    commentPlaceholder: "Leave a comment…",
    dictate: "Dictate",
    holdToTalk: "Hold to talk",
    listening: "Listening",
    speakNow: "Speak now…",
    paused: "Paused",
    stopped: "Stopped",
    transcribing: "Transcribing",
    polishing: "Polishing",
    cancel: "Cancel",
    done: "Done",
    insert: "Insert",
    send: "Send",
    stop: "Stop",
    pause: "Pause",
    resume: "Resume",
    retry: "Try again",
    releaseToInsert: "Release to insert",
    releaseToSend: "Release to send",
    slideToCancel: "Slide left to cancel",
    lockHint: "Slide up to lock",
    locked: "Locked",
    escToCancel: "to cancel",
    enterToSend: "to send",
    polish: "Polish",
    original: "Original",
    requestingTitle: "Allow microphone access",
    requestingBody: "Your browser will ask for permission. Audio is only used to transcribe what you say.",
    deniedTitle: "Microphone is blocked.",
    deniedBody: "Allow microphone access in your browser's site settings, then try again.",
    noSpeech: "Didn't catch that. Try speaking closer to the microphone.",
    network: "Couldn't transcribe. Your recording is kept.",
    limitWarning: "Recording stops in 0:30",
    newIssue: "New issue",
    createIssue: "Create issue",
    issueTitle: "Title",
    issueDescription: "Description",
    understood: "From your voice note",
    priority: "Priority",
    assignee: "Assignee",
    project: "Project",
    status: "Todo",
    you: "You",
    userMessage: "The inbox badge still shows 3 after I switched to the Design workspace.",
    agentReply:
      "I can reproduce it. The unread count query is cached per user, not per workspace, so the old value sticks until a refetch. Want me to fix it?",
    microphone: "MacBook Pro Microphone",
    language: "Language",
  },
  zh: {
    agentName: "Lambda",
    sessionTitle: "切换工作区后收件箱角标不更新",
    placeholder: "给 Lambda 发消息…",
    commentPlaceholder: "写评论…",
    dictate: "语音输入",
    holdToTalk: "按住说话",
    listening: "正在听",
    speakNow: "请说话…",
    paused: "已暂停",
    stopped: "已停止",
    transcribing: "正在转写",
    polishing: "正在润色",
    cancel: "取消",
    done: "完成",
    insert: "插入",
    send: "发送",
    stop: "停止",
    pause: "暂停",
    resume: "继续",
    retry: "重试",
    releaseToInsert: "松开插入",
    releaseToSend: "松开发送",
    slideToCancel: "左滑取消",
    lockHint: "上滑锁定",
    locked: "已锁定",
    escToCancel: "取消",
    enterToSend: "发送",
    polish: "润色",
    original: "原文",
    requestingTitle: "允许使用麦克风",
    requestingBody: "浏览器会请求权限。音频只用于把你说的话转成文字。",
    deniedTitle: "麦克风已被禁用。",
    deniedBody: "请在浏览器的网站设置中允许麦克风，然后重试。",
    noSpeech: "没有听清，请靠近麦克风再说一次。",
    network: "转写失败，录音已保留。",
    limitWarning: "30 秒后停止录音",
    newIssue: "新建 Issue",
    createIssue: "创建 Issue",
    issueTitle: "标题",
    issueDescription: "描述",
    understood: "从语音中识别",
    priority: "优先级",
    assignee: "负责人",
    project: "项目",
    status: "待办",
    you: "你",
    userMessage: "我切到 Design 工作区之后，收件箱角标还显示 3。",
    agentReply:
      "已复现。未读数的查询按用户缓存而不是按工作区缓存，所以在重新拉取之前旧值会一直保留。需要我修复吗？",
    microphone: "MacBook Pro 麦克风",
    language: "语言",
  },
} satisfies Record<Lang, Record<string, string>>;

export type Copy = (typeof COPY)["en"];
