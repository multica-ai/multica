"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Inspect and reuse workspace Skills",
    chapters: ["Workspace skills", "SKILL.md & files", "Mounted agents"],
    caption: "Each skill packages SKILL.md and optional supporting files; mounting it syncs the method to every run automatically.",
  },
  zh: {
    title: "管理并复用工作区 Skills",
    chapters: ["工作区 Skills 列表", "SKILL.md 与文件", "挂载的智能体"],
    caption: "每个 Skill 以 SKILL.md 为核心打包可复用方法；挂载给智能体后，Multica 会在执行时自动同步到运行时。",
  },
  "zh-Hant": {
    title: "管理並複用工作區 Skills",
    chapters: ["工作區 Skills 列表", "SKILL.md 與檔案", "掛載的智能體"],
    caption: "每個 Skill 以 SKILL.md 打包可複用方法；掛載給智能體後，執行時會自動同步至執行環境。",
  },
  ja: {
    title: "ワークスペースの Skills を管理・再利用する",
    chapters: ["Skills 一覧", "SKILL.md 詳細", "割り当て先"],
    caption: "SKILL.md と付属ファイルをパッケージ化し、エージェントへ紐づけると実行時に自動同期されます。",
  },
  ko: {
    title: "워크스페이스 Skills 관리 및 재사용하기",
    chapters: ["Skills 목록", "SKILL.md 상세", "연결된 에이전트"],
    caption: "SKILL.md와 보조 파일을 패키지로 묶어 여러 에이전트에 장착하면 실행 시 자동으로 동기화됩니다.",
  },
  fr: {
    title: "Gérer et réutiliser les Skills de l'espace",
    chapters: ["Liste des Skills", "Détail SKILL.md", "Agents associés"],
    caption: "Chaque Skill regroupe SKILL.md et ses fichiers annexes, synchronisés automatiquement à chaque exécution.",
  },
};

export function SkillDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.skill ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="skill" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
