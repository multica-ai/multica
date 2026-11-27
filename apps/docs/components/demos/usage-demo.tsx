"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Inspect token usage, cost, and error breakdowns",
    chapters: ["Usage overview", "Switch metrics", "Errors tab"],
    caption: "Analytics aggregates tokens, cost, runtime duration, and failure classes across projects and agents.",
  },
  zh: {
    title: "在统计页查看 Token、费用与错误分类",
    chapters: ["用量总览", "切换指标与周期", "错误分类排查"],
    caption: "统计页汇总各项目与智能体的 Token 消耗、费用、运行时长及失败类型分布，便于快速定位瓶颈。",
  },
  "zh-Hant": {
    title: "在統計頁檢視 Token、費用與錯誤分類",
    chapters: ["用量總覽", "切換指標與週期", "錯誤分類排查"],
    caption: "統計頁彙整各專案與智能體的 Token 消耗、費用、執行時長與失敗類型分佈。",
  },
  ja: {
    title: "トークン使用量・コスト・エラー内訳を確認する",
    chapters: ["使用量サマリー", "指標を切替", "エラータブ"],
    caption: "プロジェクトやエージェントごとのトークン数・コスト・実行時間・エラー種別を一覧で確認できます。",
  },
  ko: {
    title: "토큰 사용량·비용·오류 분포 확인하기",
    chapters: ["사용량 개요", "지표 전환", "오류 탭"],
    caption: "프로젝트와 에이전트별 토큰, 비용, 실행 시간, 실패 유형을 한눈에 집계해 보여 줍니다.",
  },
  fr: {
    title: "Suivre la consommation, les coûts et les erreurs",
    chapters: ["Vue d'ensemble", "Changer de métrique", "Onglet Erreurs"],
    caption: "Analytics regroupe tokens, coûts, durée d'exécution et types d'erreurs par projet et par agent.",
  },
};

export function UsageDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.usage ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="usage" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
