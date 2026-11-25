"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Assign an issue to an agent",
    chapters: ["Pick agent", "Confirm start", "Live run", "Delivered"],
    caption: "Assigning an agent opens a confirmation dialog, moves the issue to In Progress during execution, and lands in In Review with the reply.",
  },
  zh: {
    title: "将任务分配给智能体",
    chapters: ["选择智能体", "确认启动", "实时运行", "交付审核"],
    caption: "选择智能体后会弹出确认窗口；开始执行时状态自动推进为进行中，交付后移至审核中并写回评论。",
  },
  "zh-Hant": {
    title: "將任務指派給智能體",
    chapters: ["選擇智能體", "確認啟動", "即時執行", "交付審核"],
    caption: "選擇智能體後會彈出確認視窗；執行時狀態自動推進為進行中，交付後移至審核中並寫回評論。",
  },
  ja: {
    title: "タスクをエージェントに割り当てる",
    chapters: ["エージェント選択", "開始を確認", "実行中", "納品・レビュー中"],
    caption: "エージェントを選ぶと確認ダイアログが開き、実行中は進行中、完了後はレビュー中へ自動で移ります。",
  },
  ko: {
    title: "태스크를 에이전트에게 할당하기",
    chapters: ["에이전트 선택", "시작 확인", "실행 중", "전달 완료"],
    caption: "에이전트를 선택하면 확인 창이 열리고, 작업 중에는 진행 중으로, 전달 후에는 리뷰 중으로 이동합니다.",
  },
  fr: {
    title: "Assigner une tâche à un agent",
    chapters: ["Choisir l'agent", "Confirmer", "En cours", "Livré"],
    caption: "Assigner un agent ouvre une confirmation, passe la tâche à En cours pendant l'exécution, puis à En relecture.",
  },
};

export function AssignDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.assign ?? { duration: 7.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="assign" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
