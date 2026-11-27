"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Hand off an issue through a project workflow",
    chapters: ["Workflow statuses", "Confirm handoff", "Next owner runs"],
    caption: "Moving an issue to a step with an agent handler confirms the handoff brief and starts the next agent in place.",
  },
  zh: {
    title: "按项目工作流把任务交给下一步",
    chapters: ["查看工作流状态", "确认交接说明", "下一位智能体执行"],
    caption: "将任务移入配置了处理人的状态列时，确认窗展示交接对象与说明，确认后立即在原任务上接力运行。",
  },
  "zh-Hant": {
    title: "按專案工作流將任務交給下一步",
    chapters: ["檢視工作流狀態", "確認交接說明", "下一位智能體執行"],
    caption: "將任務移入設定了處理人的狀態列時，確認視窗會展示交接對象與說明，確認後立即接力執行。",
  },
  ja: {
    title: "プロジェクトのワークフローで次のステップへ引き継ぐ",
    chapters: ["ワークフロー状態", "引き継ぎを確認", "次の担当が実行"],
    caption: "エージェントが割り当てられたステップへ移すと確認画面が開き、同じタスク上で次の実行が始まります。",
  },
  ko: {
    title: "프로젝트 워크플로로 다음 단계 인계하기",
    chapters: ["워크플로 상태", "인계 확인", "다음 담당자 실행"],
    caption: "에이전트가 지정된 단계로 옮기면 인계 확인 후 같은 태스크에서 바로 다음 실행이 시작됩니다.",
  },
  fr: {
    title: "Transmettre une tâche via un workflow de projet",
    chapters: ["Étapes du workflow", "Confirmer le relais", "Exécution suivante"],
    caption: "Passer à une étape assignée à un agent confirme le brief de relais et démarre l'étape suivante sur place.",
  },
};

export function WorkflowDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.workflow ?? { duration: 6.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="workflow" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
