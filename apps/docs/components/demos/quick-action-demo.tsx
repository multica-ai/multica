"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Run a saved Quick Action from an issue",
    chapters: ["Quick actions panel", "Pick template", "Trigger run"],
    caption: "Quick actions save who to invoke and what prompt to send so recurring asks take one click without changing the assignee.",
  },
  zh: {
    title: "在任务中一键运行保存好的快捷操作",
    chapters: ["查看快捷操作", "点击预设模板", "立即触发运行"],
    caption: "把常用的智能体与指令存成快捷操作，在任意任务上一点即可触发，不改动当前负责人。",
  },
  "zh-Hant": {
    title: "在任務中一鍵執行快捷操作",
    chapters: ["檢視快捷操作", "點擊預設模板", "立即觸發執行"],
    caption: "將常用的智能體與指令儲存為快捷操作，在任意任務上一鍵觸發且不改動負責人。",
  },
  ja: {
    title: "保存したクイックアクションをタスクから実行する",
    chapters: ["クイックアクション", "テンプレート選択", "実行開始"],
    caption: "よく使う依頼先とプロンプトを保存しておき、担当者を変えずにワンクリックで起動できます。",
  },
  ko: {
    title: "태스크에서 저장된 퀵 액션 실행하기",
    chapters: ["퀵 액션 패널", "템플릿 선택", "실행 시작"],
    caption: "자주 쓰는 요청과 대상 에이전트를 저장해 두면 담당자를 바꾸지 않고 클릭 한 번으로 실행할 수 있습니다.",
  },
  fr: {
    title: "Lancer une action rapide depuis une tâche",
    chapters: ["Actions rapides", "Choisir modèle", "Démarrer"],
    caption: "Enregistrez l'agent et le prompt une seule fois pour relancer la même demande en un clic.",
  },
};

export function QuickActionDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["quick-action"] ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="quick-action" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
