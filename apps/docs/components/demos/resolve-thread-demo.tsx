"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Resolve a finished comment thread",
    chapters: ["Open thread", "Resolve action", "Collapsed summary"],
    caption: "Resolving a thread folds settled back-and-forth into one quiet summary bar while keeping the full history expandable.",
  },
  zh: {
    title: "解决已达成结论的讨论串",
    chapters: ["查看讨论串", "标记为已解决", "折叠为简洁摘要"],
    caption: "解决讨论后会把已定案的往复对话折叠成一行摘要，保持任务时间线清爽，同时随时可以展开回看。",
  },
  "zh-Hant": {
    title: "解決已達成結論的討論串",
    chapters: ["檢視討論串", "標記為已解決", "折疊為摘要列"],
    caption: "解決討論後會把定案的來回對話折疊為一行摘要，維持時間線整潔且隨時可展開。",
  },
  ja: {
    title: "結論が出たコメントスレッドを解決する",
    chapters: ["スレッドを確認", "スレッドを解決", "折りたたまれた要約"],
    caption: "スレッドを解決するとやり取りが 1 行に折りたたまれ、必要なときにいつでも展開できます。",
  },
  ko: {
    title: "결론이 난 댓글 스레드 해결하기",
    chapters: ["스레드 확인", "스레드 해결", "접힌 요약 표시"],
    caption: "스레드를 해결하면 완료된 논의가 한 줄 요약으로 접히고, 필요할 때 언제든 다시 펼칠 수 있습니다.",
  },
  fr: {
    title: "Résoudre un fil de discussion terminé",
    chapters: ["Fil ouvert", "Résoudre", "Résumé replié"],
    caption: "Résoudre un fil replie l'échange en une ligne tout en gardant l'historique consultable à tout moment.",
  },
};

export function ResolveThreadDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST["resolve-thread"] ?? { duration: 5.5, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="resolve-thread" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
