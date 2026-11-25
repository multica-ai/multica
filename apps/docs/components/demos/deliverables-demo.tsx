"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Review structured deliverables in an issue",
    chapters: ["Issue context", "Structured tables", "Diff & summary"],
    caption: "Tables, diffs, charts, and attached files stay directly on the issue timeline so reviewers verify outcomes in one place.",
  },
  zh: {
    title: "在任务时间线中验收结构化交付物",
    chapters: ["任务上下文", "结构化对比表", "改动 Diff 与结论"],
    caption: "对比表、代码 Diff、图表与附件完整沉淀在任务时间线中，验收者不必切工具即可一屏核对。",
  },
  "zh-Hant": {
    title: "在任務時間線中驗收結構化交付物",
    chapters: ["任務上下文", "結構化對比表", "改動 Diff 與結論"],
    caption: "對比表、程式碼 Diff、圖表與附件完整沉澱在任務時間線中，驗收者可於同頁核對。",
  },
  ja: {
    title: "タスク上で構造化された成果物をレビューする",
    chapters: ["タスクの背景", "比較テーブル", "差分とまとめ"],
    caption: "テーブル、diff、グラフ、添付ファイルが同じタイムラインに残り、その場で確認できます。",
  },
  ko: {
    title: "태스크 타임라인에서 구조화된 결과물 검토하기",
    chapters: ["태스크 맥락", "비교 테이블", "Diff 및 요약"],
    caption: "표, 코드 diff, 차트, 첨부 파일이 같은 타임라인에 정리되어 한곳에서 검증할 수 있습니다.",
  },
  fr: {
    title: "Relire des livrables structurés dans une tâche",
    chapters: ["Contexte", "Tableaux", "Diff et résumé"],
    caption: "Tableaux, diffs et pièces jointes restent sur le fil de la tâche pour tout vérifier au même endroit.",
  },
};

export function DeliverablesDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.deliverables ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="deliverables" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
