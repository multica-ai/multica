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

export const COMMENT_SCRIPT: Record<Lang, VoiceScript> = {
  en: {
    phrases: [
      "Um, I just reproduced it in the Design workspace,",
      " switching back still shows 3,",
      " a page refresh fixes it,",
      " so it's the cache key.",
    ],
    polished:
      "I just reproduced it in the Design workspace: switching back still shows 3 and a page refresh fixes it, so it's the cache key.",
  },
  zh: {
    phrases: ["嗯，我刚在 Design 工作区复现了，", "切回来以后角标还是 3，", "刷新一下页面就好了，", "应该就是缓存 key 的问题。"],
    polished: "我刚在 Design 工作区复现了：切回来以后角标还是 3，刷新页面后恢复正常，应该就是缓存 key 的问题。",
  },
};

export const REPLY_SCRIPT: Record<Lang, VoiceScript> = {
  en: {
    phrases: ["Yes, it's the same one,", " have Lambda fix both."],
    polished: "Yes, it's the same one. Have Lambda fix both.",
  },
  zh: {
    phrases: ["对，是同一个，", "让 Lambda 顺便把那边也改了。"],
    polished: "对，是同一个。让 Lambda 顺便把那边也改了。",
  },
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
    you: "You",
    userMessage: "The inbox badge still shows 3 after I switched to the Design workspace.",
    agentReply:
      "I can reproduce it. The unread count query is cached per user, not per workspace, so the old value sticks until a refetch. Want me to fix it?",
    microphone: "MacBook Pro Microphone",
    language: "Language",
    voiceInput: "Voice input",
    micTooltip: "Click to dictate, hold to talk",
    replyPlaceholder: "Reply…",
    activity: "Activity",
    issueKey: "MUL-1342",
    issueBody: "After switching from Multica to Design, the inbox badge keeps showing 3 until the page is refreshed.",
    humanName: "Jiayuan",
    humanComment: "Is this the same cache bug we fixed for issue counts?",
    minutesAgo: "2m ago",
    justNow: "just now",
    willStart: "Starts after sending",
    attach: "Attach file",
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
    you: "你",
    userMessage: "我切到 Design 工作区之后，收件箱角标还显示 3。",
    agentReply:
      "已复现。未读数的查询按用户缓存而不是按工作区缓存，所以在重新拉取之前旧值会一直保留。需要我修复吗？",
    microphone: "MacBook Pro 麦克风",
    language: "语言",
    voiceInput: "语音输入",
    micTooltip: "点按听写，按住说话",
    replyPlaceholder: "回复…",
    activity: "动态",
    issueKey: "MUL-1342",
    issueBody: "从 Multica 切到 Design 工作区后，收件箱角标一直显示 3，刷新页面才恢复。",
    humanName: "Jiayuan",
    humanComment: "这和之前 issue 计数那个缓存问题是同一个吗？",
    minutesAgo: "2 分钟前",
    justNow: "刚刚",
    willStart: "发送后开始",
    attach: "添加附件",
  },
} satisfies Record<Lang, Record<string, string>>;

export type Copy = (typeof COPY)["en"];
