"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Run recurring work with Autopilots",
    chapters: ["Autopilot list", "Schedule & runbook", "Run now"],
    caption: "Autopilots trigger on a cron schedule or webhook, and can also be tested immediately with Run now.",
  },
  zh: {
    title: "用自动化定期或按事件启动智能体",
    chapters: ["自动化列表", "定时规则与运行手册", "立即运行测试"],
    caption: "自动化按 Cron 时间表或外部 Webhook 自动触发，也支持点击「立即运行」随时试跑一次。",
  },
  "zh-Hant": {
    title: "用自動化定期或按事件啟動智能體",
    chapters: ["自動化列表", "排程與執行手冊", "立即執行測試"],
    caption: "自動化按 Cron 排程或外部 Webhook 觸發，也支援點擊「立即執行」隨時試跑。",
  },
  ja: {
    title: "オートパイロットで定期タスクを自動実行する",
    chapters: ["一覧", "スケジュールと手順", "今すぐ実行"],
    caption: "Cron や Webhook で自動起動でき、「今すぐ実行」でいつでも動作を試せます。",
  },
  ko: {
    title: "오토파일럿으로 반복 작업 자동화하기",
    chapters: ["오토파일럿 목록", "일정 및 런북", "지금 실행"],
    caption: "크론 일정이나 웹훅으로 자동 실행되며, 지금 실행 버튼으로 즉시 테스트할 수 있습니다.",
  },
  fr: {
    title: "Automatiser les tâches récurrentes avec Autopilot",
    chapters: ["Liste", "Horaire et runbook", "Exécuter maintenant"],
    caption: "Les automatisations se déclenchent par cron ou webhook et se testent à tout moment avec Exécuter maintenant.",
  },
};

export function AutopilotDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.autopilot ?? { duration: 6.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="autopilot" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
