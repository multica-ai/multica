"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Track linked Pull Requests in the issue sidebar",
    chapters: ["Pull requests card", "Link another PR", "Synced state"],
    caption: "Pull requests referencing the issue identifier link automatically, showing diff stats, CI status, and merge transitions.",
  },
  zh: {
    title: "在任务右侧栏追踪关联的 Pull Request",
    chapters: ["Pull requests 卡片", "手动关联 PR", "同步状态与合并"],
    caption: "标题或分支包含任务编号的 PR 会自动关联到右侧栏，实时展示改动行数、CI 检查与合并流转。",
  },
  "zh-Hant": {
    title: "在任務右側欄追蹤關聯的 Pull Request",
    chapters: ["Pull requests 卡片", "手動關聯 PR", "同步狀態與合併"],
    caption: "標題或分支包含任務編號的 PR 會自動關聯至右側欄，即時顯示改動行數、CI 檢查與合併狀態。",
  },
  ja: {
    title: "タスクのサイドバーで関連 PR を追跡する",
    chapters: ["PR カード", "PR を手動リンク", "ステータス同期"],
    caption: "タスク番号を含む PR は自動で紐づき、差分行数・CI チェック・マージ状況をリアルタイム表示します。",
  },
  ko: {
    title: "태스크 사이드바에서 연결된 PR 추적하기",
    chapters: ["Pull requests 카드", "PR 직접 연결", "상태 동기화"],
    caption: "태스크 번호가 포함된 PR은 자동 연결되며 변경 라인 수, CI 체크, 병합 상태를 함께 보여 줍니다.",
  },
  fr: {
    title: "Suivre les Pull Requests liées dans la barre latérale",
    chapters: ["Carte PR", "Lier une PR", "Statut synchronisé"],
    caption: "Les PR mentionnant l'identifiant de tâche se relient automatiquement avec diff, vérifications CI et statut de fusion.",
  },
};

export function PrLinkDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["pr-link"] ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="pr-link" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
