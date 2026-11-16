import type { ConceptInfo } from "../stage";

export const INLINE_WAVEFORM: ConceptInfo = {
  letter: "A",
  name: { en: "Inline waveform", zh: "工具栏波形" },
  pitch: {
    en: "The toolbar turns into a recording strip. Text arrives when you stop, so you review it before it goes anywhere.",
    zh: "点麦克风后，输入框底部的工具栏变成录音条。停止后文字才落进草稿，发送前可以先看一遍。",
  },
  how: {
    en: ["Click the mic, or press ⌘⇧Space", "✓ or ⌘⇧Space stops and inserts", "↑ or Enter stops and sends", "Esc or × throws it away"],
    zh: ["点麦克风，或按 ⌘⇧Space", "✓ 或 ⌘⇧Space：停止并插入草稿", "↑ 或 Enter：停止并直接发送", "Esc 或 ×：丢弃"],
  },
  bestFor: { en: "Everyday messages. Familiar from ChatGPT and Claude.", zh: "日常发消息。和 ChatGPT、Claude 的做法一致，用户无需学习。" },
  tradeoff: {
    en: "You can't see words while talking, so long dictation feels blind.",
    zh: "说话时看不到文字，长段口述时心里没底。",
  },
};

export const LIVE_DICTATION: ConceptInfo = {
  letter: "B",
  name: { en: "Live dictation", zh: "实时上屏" },
  pitch: {
    en: "Nothing changes shape. Words stream into the draft as you speak; uncertain words stay grey until they settle. An optional Polish cleans it up afterwards.",
    zh: "界面不变形。边说边上屏，还没确定的词显示为灰色，确定后变黑。说完可以一键「润色」。",
  },
  how: {
    en: ["Click the mic, or press ⌘⇧Space", "Click the level meter or press Esc to stop", "Enter stops and sends", "After stopping, try Polish"],
    zh: ["点麦克风，或按 ⌘⇧Space", "点跳动的音量条或按 Esc 停止", "Enter：停止并发送", "停止后试试「润色」"],
  },
  bestFor: { en: "People who think while they talk and want to see it land.", zh: "边想边说、需要看着文字上屏的人。" },
  tradeoff: {
    en: "Raw speech is messy on screen; needs a good streaming recognizer.",
    zh: "口语原文直接上屏会比较乱；依赖流式识别的质量。",
  },
};

export const PUSH_TO_TALK: ConceptInfo = {
  letter: "C",
  name: { en: "Push-to-talk", zh: "按住说话" },
  pitch: {
    en: "Hold a key anywhere and talk; let go and the text lands in the draft. A small HUD is the only sign it's listening. Tap instead of hold to lock it hands-free.",
    zh: "在任何地方按住快捷键说话，松开文字就落进草稿。只有一个小浮层提示正在听。轻点而不是按住，就锁定为免手持模式。",
  },
  how: {
    en: ["Hold ⌥Space (or press and hold the mic), talk, release", "Tap the mic once to lock; tap ■ to stop", "Esc while holding cancels"],
    zh: ["按住 ⌥Space（或按住麦克风按钮）说话，松开", "轻点麦克风一下即锁定，点 ■ 停止", "按住时按 Esc 取消"],
  },
  bestFor: { en: "Power users and the desktop app, where it can become a global shortcut.", zh: "重度用户和桌面端，可以做成全局快捷键。" },
  tradeoff: { en: "Hidden until you learn the shortcut.", zh: "不学快捷键就发现不了。" },
};

export const FOCUS_MODE: ConceptInfo = {
  letter: "D",
  name: { en: "Focus mode", zh: "专注模式" },
  pitch: {
    en: "The mic opens a full-panel listening space: an orb that breathes with your voice, the transcript in large type, pause, and a review step with polished and original side by side.",
    zh: "麦克风打开一个整面板的聆听空间：随声音起伏的圆、大字号实时文字、可暂停，最后有一个审阅步骤，润色版和原文可以切换对比。",
  },
  how: {
    en: ["Click the mic, or press ⌘⇧Space", "Space pauses and resumes", "Done → review → Insert or Send", "Toggle Polish before you finish"],
    zh: ["点麦克风，或按 ⌘⇧Space", "空格键暂停 / 继续", "完成 → 审阅 → 插入或发送", "结束前可以开关「润色」"],
  },
  bestFor: { en: "Long briefs to an agent: specs, bug reports, thinking out loud.", zh: "给智能体口述长需求：规格、Bug 描述、边想边说。" },
  tradeoff: { en: "Covers the conversation; too heavy for a one-liner.", zh: "会盖住对话；一句话的场景太重。" },
};

export const SEND_SLOT: ConceptInfo = {
  letter: "E",
  name: { en: "Hold the send button", zh: "按住发送键" },
  pitch: {
    en: "The messenger pattern. With an empty draft the send button is a mic: hold to talk, release to send, slide left to cancel, slide up to lock.",
    zh: "即时通讯的做法。草稿为空时，发送键就是麦克风：按住说话，松开发送，左滑取消，上滑锁定。",
  },
  how: {
    en: ["Clear the draft; the send button becomes a mic", "Press and hold it, talk, release to send", "While holding: drag left to cancel, drag up to lock", "Tap once to lock hands-free"],
    zh: ["草稿为空时发送键变成麦克风", "按住说话，松开直接发送", "按住时：向左拖取消，向上拖锁定", "轻点一下也可以锁定"],
  },
  bestFor: { en: "Mobile and the compact floating chat, where space is tight.", zh: "移动端和小尺寸的悬浮聊天窗，空间紧张的地方。" },
  tradeoff: { en: "Sends without review; drag gestures are awkward with a mouse.", zh: "不经审阅直接发出；用鼠标做拖拽手势比较别扭。" },
};

export const ALL_CONCEPTS = [INLINE_WAVEFORM, LIVE_DICTATION, PUSH_TO_TALK, FOCUS_MODE, SEND_SLOT];
