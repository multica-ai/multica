"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Move an issue across status categories",
    chapters: ["Backlog → Active", "In Progress → Review", "Mark Done"],
    caption: "Every status belongs to a lifecycle category (unstarted, started, done, closed) and records transitions in the activity timeline.",
  },
  zh: {
    title: "在生命周期状态之间推进任务",
    chapters: ["移出 Backlog", "进行中 → 审核中", "验收完成"],
    caption: "每个状态归属明确的生命周期分类（未开始、进行中、已完成、已关闭），每次变更自动记入活动历史。",
  },
  "zh-Hant": {
    title: "在生命週期狀態之間推進任務",
    chapters: ["移出 Backlog", "進行中 → 審核中", "驗收完成"],
    caption: "每個狀態對應明確的生命週期分類，每次狀態變更都會自動記錄於活動時間線。",
  },
  ja: {
    title: "ステータスに沿ってタスクを進める",
    chapters: ["Backlog から移動", "進行中 → レビュー中", "完了にする"],
    caption: "各ステータスはライフサイクル分類に属し、変更履歴はすべてアクティビティに残ります。",
  },
  ko: {
    title: "상태 단계를 따라 태스크 진행하기",
    chapters: ["백로그에서 이동", "진행 중 → 리뷰 중", "완료 처리"],
    caption: "모든 상태는 생명주기 범주에 속하며, 상태 전환 내역은 활동 타임라인에 기록됩니다.",
  },
  fr: {
    title: "Faire avancer une tâche à travers ses statuts",
    chapters: ["Sortir du Backlog", "En cours → Relecture", "Terminer"],
    caption: "Chaque statut appartient à une catégorie de cycle de vie et enregistre chaque transition dans l'historique.",
  },
};

export function IssueStatusDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["issue-status"] ?? { duration: 6.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="issue-status" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
