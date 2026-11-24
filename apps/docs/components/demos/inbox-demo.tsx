"use client";

import { CLIP_MANIFEST } from "./clip-manifest";
import { EditorialClip, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    title: "Triage notifications in the Work panel Inbox",
    chapters: ["Open unread", "Archive from floating bar", "Archived tab"],
    caption: "Opening a notification jumps to the comment with its floating action bar above the composer; archiving advances to the next item without changing issue status.",
  },
  zh: {
    title: "在工作面板收件箱中高效处理通知",
    chapters: ["打开未读通知", "从悬浮栏归档并推进", "查看已归档标签"],
    caption: "点开通知直达对应评论，输入框上方悬浮显示通知操作栏；归档（E）后自动推进到下一条，且不会修改任务状态。",
  },
  "zh-Hant": {
    title: "在工作面板收件箱中處理通知",
    chapters: ["開啟未讀通知", "從懸浮列歸檔並推進", "檢視已歸檔分頁"],
    caption: "點開通知直達評論，輸入框上方懸浮顯示通知操作列；歸檔（E）後自動推進至下一條，且不會改變任務狀態。",
  },
  ja: {
    title: "ワークパネルのインボックスで通知を整理する",
    chapters: ["未読を開く", "フローティングバーでアーカイブ", "アーカイブ済み"],
    caption: "通知を開くと入力欄の上部に操作バーが浮かび、アーカイブ（E）で次へ進みます。タスクのステータスは変わりません。",
  },
  ko: {
    title: "작업 패널 인박스에서 알림 정리하기",
    chapters: ["읽지 않은 알림 열기", "플로팅 바에서 보관", "보관됨 탭"],
    caption: "알림을 열면 입력창 위에 작업 바가 표시되며, 보관(E)하면 태스크 상태를 바꾸지 않고 다음 알림으로 이동합니다.",
  },
  fr: {
    title: "Trier les notifications dans la boîte de réception",
    chapters: ["Ouvrir non lu", "Archiver depuis la barre flottante", "Onglet Archivées"],
    caption: "Les actions flottent au-dessus de la zone de commentaire ; archiver passe à la notification suivante sans modifier le statut de la tâche.",
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
