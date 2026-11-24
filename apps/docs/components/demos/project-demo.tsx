"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Organize issues and context inside a project",
    chapters: ["Project overview", "Tracked issues", "Shared resources"],
    caption: "A project tracks progress across its issues and shares its description, repositories, and workflow with every run inside it.",
  },
  zh: {
    title: "在项目中组织任务与共享上下文",
    chapters: ["项目概览与进度", "项目内任务", "共享资源与说明"],
    caption: "项目自动汇总名下任务进度，并把项目说明、代码仓库与工作流提供给其中的每一次智能体运行。",
  },
  "zh-Hant": {
    title: "在專案中組織任務與共享上下文",
    chapters: ["專案概覽與進度", "專案內任務", "共享資源與說明"],
    caption: "專案自動彙整名下任務進度，並把專案說明、倉庫與工作流提供給其中的每一次執行。",
  },
  ja: {
    title: "プロジェクトでタスクと共有コンテキストを管理する",
    chapters: ["プロジェクト概要", "紐づくタスク", "共有リソース"],
    caption: "プロジェクト全体の進捗を追いながら、説明・リポジトリ・ワークフローを全実行へ共有できます。",
  },
  ko: {
    title: "프로젝트에서 태스크와 공유 맥락 관리하기",
    chapters: ["프로젝트 개요", "포함된 태스크", "공유 리소스"],
    caption: "프로젝트가 진행률을 집계하고 설명·저장소·워크플로를 하위 모든 실행에 자동으로 공유합니다.",
  },
  fr: {
    title: "Organiser tâches et contexte dans un projet",
    chapters: ["Vue d'ensemble", "Tâches suivies", "Ressources partagées"],
    caption: "Un projet suit l'avancement global et partage description, dépôts et workflow avec chaque exécution.",
  },
};

export function ProjectDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.project ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="project" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
