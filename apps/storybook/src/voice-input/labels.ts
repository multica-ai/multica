import type { Lang } from "./copy";

const LABELS = {
  idle: { en: "Idle", zh: "空闲" },
  idleDraft: { en: "Idle, with a draft", zh: "空闲（已有草稿）" },
  requesting: { en: "Asking for the microphone", zh: "请求麦克风权限" },
  listeningEmpty: { en: "Listening, nothing said yet", zh: "正在听，还没开口" },
  listening: { en: "Listening", zh: "正在听" },
  locked: { en: "Locked hands-free", zh: "已锁定（免手持）" },
  paused: { en: "Paused", zh: "已暂停" },
  processing: { en: "Transcribing", zh: "转写中" },
  polish: { en: "Inserted, Polish offered", zh: "已插入，提供润色" },
  review: { en: "Review", zh: "审阅" },
  cancelling: { en: "Sliding to cancel", zh: "左滑取消中" },
  locking: { en: "Sliding to lock", zh: "上滑锁定中" },
  filled: { en: "Filled from voice", zh: "已从语音填好" },
  denied: { en: "Microphone blocked", zh: "麦克风被禁用" },
  noSpeech: { en: "Nothing heard", zh: "没有听到声音" },
  network: { en: "Transcription failed", zh: "转写失败" },
} satisfies Record<string, Record<Lang, string>>;

export type StateLabel = keyof typeof LABELS;

export const label = (key: StateLabel, lang: Lang) => LABELS[key][lang];
