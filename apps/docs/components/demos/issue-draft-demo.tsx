"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Create an issue with an agent (C)",
    chapters: ["Press C", "Describe the goal", "Create & start"],
    caption: "Press C from anywhere in the workspace, write a plain-language goal, and let Multica draft and start the issue.",
  },
  zh: {
    title: "按 C 快速通过智能体创建任务",
    chapters: ["按 C 打开弹窗", "用自然语言描述目标", "创建并启动"],
    caption: "在工作区任意位置按 C 呼出新建任务窗，直接描述目标即可自动生成结构化任务并交给智能体处理。",
  },
  "zh-Hant": {
    title: "按 C 透過智能體快速建立任務",
    chapters: ["按 C 開啟視窗", "以自然語言描述目標", "建立並啟動"],
    caption: "在工作區任一位置按 C 呼出新建任務視窗，輸入目標即可自動建立並交給智能體執行。",
  },
  ja: {
    title: "エージェントと一緒にタスクを作成 (C)",
    chapters: ["C キーで開く", "目的を自然文で入力", "作成して開始"],
    caption: "ワークスペースのどこからでも C で作成ダイアログを開き、目的を書くだけでタスクを起票できます。",
  },
  ko: {
    title: "에이전트로 태스크 빠르게 만들기 (C)",
    chapters: ["C 키로 열기", "목표 설명 입력", "생성 및 시작"],
    caption: "어디서나 C 키로 새 태스크 창을 열고 원하는 결과를 적으면 바로 생성하고 실행을 시작합니다.",
  },
  fr: {
    title: "Créer une tâche avec un agent (C)",
    chapters: ["Appuyer sur C", "Décrire l'objectif", "Créer et démarrer"],
    caption: "Appuyez sur C n'importe où dans l'espace, décrivez le besoin et créez la tâche en un geste.",
  },
};

export function IssueDraftDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["issue-draft"] ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="issue-draft" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
