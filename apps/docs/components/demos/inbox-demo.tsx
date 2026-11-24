"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Triage notifications in the Work panel Inbox",
    chapters: ["Open unread", "Archive & advance", "Archived tab"],
    caption: "Opening a notification jumps to the comment; archiving removes it from Inbox and advances cleanly without changing issue status.",
  },
  zh: {
    title: "在工作面板收件箱中高效处理通知",
    chapters: ["打开未读通知", "归档并切到下一条", "查看已归档标签"],
    caption: "点开通知直达对应评论，按归档（E）后自动推进到下一条；归档仅清理通知，不会修改任务状态。",
  },
  "zh-Hant": {
    title: "在工作面板收件箱中處理通知",
    chapters: ["開啟未讀通知", "歸檔並切至下一條", "檢視已歸檔分頁"],
    caption: "點開通知直達評論，歸檔（E）後自動推進至下一條，且不會改變任務狀態。",
  },
  ja: {
    title: "ワークパネルのインボックスで通知を整理する",
    chapters: ["未読を開く", "アーカイブして次へ", "アーカイブ済み"],
    caption: "通知を開くと該当コメントへ移動し、アーカイブ（E）で次へ進みます。タスクのステータスは変わりません。",
  },
  ko: {
    title: "작업 패널 인박스에서 알림 정리하기",
    chapters: ["읽지 않은 알림 열기", "보관 후 다음 이동", "보관됨 탭"],
    caption: "알림을 열면 댓글로 이동하고 보관(E)하면 다음 알림이 열립니다. 태스크 상태는 변하지 않습니다.",
  },
  fr: {
    title: "Trier les notifications dans la boîte de réception",
    chapters: ["Ouvrir non lu", "Archiver", "Onglet Archivées"],
    caption: "Archiver retire la notification et passe à la suivante sans jamais modifier le statut de la tâche.",
  },
};

export function InboxDemo() {
  const t = useDemoCopy(copy);
  const meta = CLIP_MANIFEST.inbox ?? { duration: 6.0, chapters: [] };
  const chapters = t.chapters.map((label, i) => ({
    label,
    at: meta.chapters[i]?.at ?? (i * meta.duration) / t.chapters.length,
  }));
  return <EditorialClip slug="inbox" title={t.title} chapters={chapters} duration={meta.duration} caption={t.caption} />;
}
