"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "@mention an agent in a comment",
    chapters: ["Type @mention", "Trigger preview", "Agent replies"],
    caption: "The trigger preview under the composer shows who starts when sent; the issue's assignee stays unchanged.",
  },
  zh: {
    title: "在评论中 @智能体",
    chapters: ["输入 @提及", "触发预览", "智能体回复"],
    caption: "发送前评论框下方会预览即将启动的智能体；运行完成后任务的负责人与状态保持不变。",
  },
  "zh-Hant": {
    title: "在評論中 @智能體",
    chapters: ["輸入 @提及", "觸發預覽", "智能體回覆"],
    caption: "發送前評論框下方會預覽即將啟動的智能體；任務負責人與狀態維持不變。",
  },
  ja: {
    title: "コメントでエージェントを @メンション",
    chapters: ["@メンション入力", "トリガープレビュー", "エージェントが返信"],
    caption: "送信前に起動するエージェントがプレビューされ、タスクの担当者は変わらないまま返信が届きます。",
  },
  ko: {
    title: "댓글에서 에이전트 @멘션하기",
    chapters: ["@멘션 입력", "트리거 미리보기", "에이전트 답변"],
    caption: "보내기 전 시작될 에이전트가 미리보기로 표시되며, 태스크 담당자는 그대로 유지됩니다.",
  },
  fr: {
    title: "@mentionner un agent dans un commentaire",
    chapters: ["Saisir @mention", "Aperçu", "Réponse"],
    caption: "L'aperçu sous la zone de saisie indique quel agent démarre sans modifier le responsable de la tâche.",
  },
};

export function MentionDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.mention ?? { duration: 6.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="mention" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
