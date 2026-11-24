"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Steer a running agent mid-turn",
    chapters: ["Active run", "Send correction", "Redirected output"],
    caption: "Send a comment while the agent is working to add it to the current run instead of stopping and starting over.",
  },
  zh: {
    title: "给运行中的智能体插话纠偏",
    chapters: ["运行进行中", "发送插话修正", "沿新方向完成"],
    caption: "在智能体执行期间直接发送修正评论，消息会加入当前运行并在下一步边界生效，无需中断重来。",
  },
  "zh-Hant": {
    title: "給執行中的智能體插話糾偏",
    chapters: ["執行進行中", "發送插話修正", "沿新方向完成"],
    caption: "在智能體執行期間直接發送修正評論，訊息會加入目前執行，無需中斷重來。",
  },
  ja: {
    title: "実行中のエージェントに途中で指示を追加",
    chapters: ["実行中", "修正コメント送信", "方向転換して完了"],
    caption: "実行を止めずにコメントで修正を送ると、現在の実行に取り込まれて新しい方針で完了します。",
  },
  ko: {
    title: "실행 중인 에이전트 방향 수정하기",
    chapters: ["실행 중", "수정 지시 전송", "새 방향으로 완료"],
    caption: "실행을 멈추지 않고 댓글을 보내면 현재 실행에 합류해 수정된 방향으로 작업을 마무리합니다.",
  },
  fr: {
    title: "Réorienter un agent en cours d'exécution",
    chapters: ["Exécution active", "Envoyer correction", "Résultat ajusté"],
    caption: "Envoyez une correction pendant le travail : elle s'ajoute à l'exécution en cours sans tout redémarrer.",
  },
};

export function SteerDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.steer ?? { duration: 7.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="steer" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
