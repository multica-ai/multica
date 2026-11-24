"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Schedule a follow-up wakeup on an issue",
    chapters: ["Open Wakeups", "Pick time or event", "Rule scheduled"],
    caption: "Add a time or event wakeup in the issue sidebar so the agent returns to the same issue automatically when the condition holds.",
  },
  zh: {
    title: "在任务侧栏配置定时或事件唤醒",
    chapters: ["打开任务唤醒", "设置时间或事件条件", "规则生效"],
    caption: "在任务右侧栏添加唤醒规则后，到期或条件满足时智能体会自动在同一条任务上续跑，无需人工盯守。",
  },
  "zh-Hant": {
    title: "在任務側欄設定定時或事件喚醒",
    chapters: ["開啟任務喚醒", "設定時間或事件條件", "規則生效"],
    caption: "在任務右側欄新增喚醒規則後，到期或條件滿足時智能體會自動在同一條任務上繼續執行。",
  },
  ja: {
    title: "タスクのサイドバーでウェイクアップを設定する",
    chapters: ["ウェイクアップを開く", "時刻・条件を設定", "ルール登録完了"],
    caption: "指定した時刻や条件を満たすと、エージェントが同じタスクへ自動で戻って作業を再開します。",
  },
  ko: {
    title: "태스크 사이드바에서 깨우기 예약하기",
    chapters: ["깨우기 열기", "시각·조건 설정", "규칙 등록"],
    caption: "설정한 시각이 되거나 조건이 충족되면 에이전트가 같은 태스크로 자동 복귀해 이어서 작업합니다.",
  },
  fr: {
    title: "Planifier un réveil sur une tâche",
    chapters: ["Ouvrir Réveils", "Choisir condition", "Règle enregistrée"],
    caption: "Ajoutez un réveil temporel ou événementiel pour que l'agent reprenne la même tâche automatiquement.",
  },
};

export function WakeupDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.wakeup ?? { duration: 6.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="wakeup" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
