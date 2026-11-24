"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Chat 1:1 with an agent",
    chapters: ["Send prompt", "Agent runs", "Structured reply"],
    caption: "Each chat message triggers a private run against the agent's local runtime without creating an issue.",
  },
  zh: {
    title: "与智能体一对一对话",
    chapters: ["发送提问", "实时执行", "返回结构化答复"],
    caption: "每条对话消息都会在智能体绑定的本地运行时上触发一次私有运行，无需为此新建任务。",
  },
  "zh-Hant": {
    title: "與智能體一對一對話",
    chapters: ["發送提問", "即時執行", "返回結構化回覆"],
    caption: "每條對話訊息都會在綁定的本機執行環境觸發一次私人執行，無需建立任務。",
  },
  ja: {
    title: "エージェントと 1 対 1 でチャットする",
    chapters: ["質問を送信", "エージェント実行", "構造化された回答"],
    caption: "タスクを作らずにチャットで直接質問でき、メッセージごとにプライベートな実行が走ります。",
  },
  ko: {
    title: "에이전트와 1:1 채팅하기",
    chapters: ["질문 보내기", "에이전트 실행", "구조화된 답변"],
    caption: "태스크를 만들지 않고도 메시지만 보내면 연결된 런타임에서 비공개 실행이 돌아갑니다.",
  },
  fr: {
    title: "Discuter en direct avec un agent",
    chapters: ["Envoyer un message", "Exécution", "Réponse structurée"],
    caption: "Chaque message lance une exécution privée sur le runtime de l'agent sans créer de tâche.",
  },
};

export function ChatDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.chat ?? { duration: 6.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="chat" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
