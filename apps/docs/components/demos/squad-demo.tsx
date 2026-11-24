"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Coordinate multiple agents with a Squad",
    chapters: ["Squad list", "Leader & roster", "Workflow instructions"],
    caption: "Assigning an issue to a squad hands coordination to its leader, who routes work across specialist members.",
  },
  zh: {
    title: "用小队统筹多名智能体协作",
    chapters: ["小队列表", "队长与成员分工", "协作工作流指令"],
    caption: "把任务分配给小队后，队长智能体负责拆解约束、调度专业成员并汇总最终交付。",
  },
  "zh-Hant": {
    title: "用小隊統籌多名智能體協作",
    chapters: ["小隊列表", "隊長與成員分工", "協作工作流指令"],
    caption: "將任務指派給小隊後，隊長智能體負責拆解需求、調度成員並彙整最終交付。",
  },
  ja: {
    title: "スクワッドで複数エージェントを連携させる",
    chapters: ["スクワッド一覧", "リーダーとメンバー", "連携ワークフロー"],
    caption: "タスクをスクワッドに割り当てるとリーダーが窓口になり、専門メンバーへ仕事を振り分けます。",
  },
  ko: {
    title: "스쿼드로 여러 에이전트 협업 조율하기",
    chapters: ["스쿼드 목록", "리더와 멤버 구성", "협업 지침"],
    caption: "태스크를 스쿼드에 할당하면 리더 에이전트가 전문 멤버들에게 작업을 분배하고 결과를 취합합니다.",
  },
  fr: {
    title: "Coordonner plusieurs agents avec un Squad",
    chapters: ["Liste des squads", "Leader et membres", "Instructions d'équipe"],
    caption: "Assigner une tâche à un squad confie la coordination à son leader, qui répartit le travail entre spécialistes.",
  },
};

export function SquadDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.squad ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="squad" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
