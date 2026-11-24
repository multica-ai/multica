"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Create and configure an agent",
    chapters: ["Open New Agent", "Choose runtime", "Configure identity"],
    caption: "Pick an online local runtime, define instructions and model, and set Access before creating the agent.",
  },
  zh: {
    title: "创建并配置一名智能体",
    chapters: ["新建智能体", "选择在线运行时", "填写名称与指令"],
    caption: "先选择在线的本地 AI 编程工具运行时，再配置身份名称、指令、模型与访问权限即可上线接活。",
  },
  "zh-Hant": {
    title: "建立並設定一名智能體",
    chapters: ["新建智能體", "選擇線上執行環境", "填寫名稱與指令"],
    caption: "先選擇線上的本機 AI 程式設計工具執行環境，再設定名稱、指令、模型與權限即可使用。",
  },
  ja: {
    title: "エージェントを作成・設定する",
    chapters: ["新規作成を開く", "ランタイム選択", "名前と指示を設定"],
    caption: "オンラインのランタイムを選び、名前・指示・モデル・アクセス範囲を設定して作成します。",
  },
  ko: {
    title: "새 에이전트 생성 및 구성하기",
    chapters: ["에이전트 만들기", "런타임 선택", "이름 및 지침 설정"],
    caption: "온라인 런타임을 고른 뒤 이름, 지침, 모델, 접근 권한을 설정하면 바로 사용할 수 있습니다.",
  },
  fr: {
    title: "Créer et configurer un agent",
    chapters: ["Nouvel agent", "Choisir le runtime", "Configurer l'identité"],
    caption: "Sélectionnez un runtime en ligne, puis renseignez le nom, les instructions, le modèle et l'accès.",
  },
};

export function CreateAgentDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["create-agent"] ?? { duration: 6.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="create-agent" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
