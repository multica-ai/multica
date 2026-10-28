"use client";

import { useState, type ReactNode } from "react";
import { Check, ChevronDown, ChevronRight, FileText, LayoutGrid, MessageSquareText, X } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Version = 1 | 2;

const copy = {
  en: {
    title: "OPS-52 · Compare restaurants for the team dinner",
    agent: "Ops helper",
    member: "Lei Li",
    fileName: "restaurants.pdf",
    fileType: "PDF",
    size: { 1: "84 KB", 2: "86 KB" } as Record<Version, string>,
    uploadedAt: { 1: "Oct 20, 2026, 2:05 PM", 2: "Oct 21, 2026, 10:40 AM" } as Record<Version, string>,
    comments: {
      first: "Three candidates compared in the attachment, sorted by distance from the office.",
      feedback: "The second one has a free private room. Sort them by total price instead?",
      second: "Re-sorted by total price.",
    },
    sectionTitle: "Deliverables",
    viewAll: "View all 1 deliverable",
    versionShort: (v: Version) => `v${v}`,
    versionLabel: (v: Version) => `Version ${v} of 2`,
    versionOf: (v: Version) => `v${v} of 2`,
    locate: "Show in comments",
    close: "Close",
    page: {
      heading: "Team dinner · 12 people",
      sortedBy: { 1: "Sorted by distance", 2: "Sorted by total price" } as Record<Version, string>,
      rows: {
        1: ["Riverside Grill · 5 min walk", "Lotus Garden · 8 min walk", "The Brick Room · 12 min walk"],
        2: ["Lotus Garden · $420, room free", "Riverside Grill · $450", "The Brick Room · $470"],
      } as Record<Version, string[]>,
    },
    captions: {
      idle: "An issue where the agent uploaded a file, got feedback, and uploaded it again.",
      start: "Both uploads share a name and type, so Deliverables lists one file, marked v2. Open it.",
      viewer: "The viewer opens the latest version. Click the version number to see the others.",
      versions: "Newest first. Pick v1.",
      v1: "This is the first version. Show in comments takes you to the comment that posted it.",
      located: "The viewer closed and the page is on the comment that uploaded v1. Use Replay to run it again.",
    },
  },
  zh: {
    title: "OPS-52 · 比较团建餐厅候选",
    agent: "运营助手",
    member: "李雷",
    fileName: "餐厅对比.pdf",
    fileType: "PDF",
    size: { 1: "84 KB", 2: "86 KB" } as Record<Version, string>,
    uploadedAt: { 1: "2026年10月20日 14:05", 2: "2026年10月21日 10:40" } as Record<Version, string>,
    comments: {
      first: "三家候选餐厅的对比在附件里，按离公司的距离排序。",
      feedback: "第二家包间免费，改成按总价排吧。",
      second: "已改成按总价排序。",
    },
    sectionTitle: "产物",
    viewAll: "查看全部 1 个产物",
    versionShort: (v: Version) => `v${v}`,
    versionLabel: (v: Version) => `第 ${v} 版，共 2 版`,
    versionOf: (v: Version) => `v${v}（共 2 版）`,
    locate: "定位到评论",
    close: "关闭",
    page: {
      heading: "团建聚餐 · 12 人",
      sortedBy: { 1: "按距离排序", 2: "按总价排序" } as Record<Version, string>,
      rows: {
        1: ["江边小馆 · 步行 5 分钟", "荷塘人家 · 步行 8 分钟", "红砖餐厅 · 步行 12 分钟"],
        2: ["荷塘人家 · 1680 元，包间免费", "江边小馆 · 1800 元", "红砖餐厅 · 1860 元"],
      } as Record<Version, string[]>,
    },
    captions: {
      idle: "一条任务：智能体上传了文件，收到意见后又传了一版。",
      start: "两次上传的文件同名、同类型，所以产物里只列一份，标着 v2。点开它。",
      viewer: "查看器打开的是最新版。点版本号看其他版本。",
      versions: "新版本在上面。选 v1。",
      v1: "现在看的是第一版。点定位到评论，回到上传它的那条评论。",
      located: "查看器关了，页面停在上传 v1 的评论上。点右上角可以再演示一遍。",
    },
  },
  ja: {
    title: "OPS-52 · チーム食事会のお店候補を比較",
    agent: "運用アシスタント",
    member: "李雷",
    fileName: "店舗比較.pdf",
    fileType: "PDF",
    size: { 1: "84 KB", 2: "86 KB" } as Record<Version, string>,
    uploadedAt: { 1: "2026/10/20 14:05", 2: "2026/10/21 10:40" } as Record<Version, string>,
    comments: {
      first: "候補 3 店の比較を添付しました。会社からの距離順です。",
      feedback: "2 軒目は個室料が無料です。総額順に並べ替えてもらえますか？",
      second: "総額順に並べ替えました。",
    },
    sectionTitle: "成果物",
    viewAll: "成果物 1 件をすべて表示",
    versionShort: (v: Version) => `v${v}`,
    versionLabel: (v: Version) => `バージョン ${v}（全 2 版）`,
    versionOf: (v: Version) => `v${v}（全 2 版）`,
    locate: "コメントで表示",
    close: "閉じる",
    page: {
      heading: "チーム食事会 · 12 人",
      sortedBy: { 1: "距離順", 2: "総額順" } as Record<Version, string>,
      rows: {
        1: ["川辺食堂 · 徒歩 5 分", "蓮の庭 · 徒歩 8 分", "レンガ亭 · 徒歩 12 分"],
        2: ["蓮の庭 · 54,000 円、個室無料", "川辺食堂 · 58,000 円", "レンガ亭 · 60,000 円"],
      } as Record<Version, string[]>,
    },
    captions: {
      idle: "エージェントがファイルを上げ、意見を受けてもう一度上げたタスクです。",
      start: "2 回のアップロードは名前も種類も同じなので、成果物には 1 件だけ v2 として並びます。開いてみます。",
      viewer: "ビューアは最新版で開きます。バージョン番号をクリックすると、ほかの版が見られます。",
      versions: "新しい版が上です。v1 を選びます。",
      v1: "いま見ているのは最初の版です。「コメントで表示」で、これを投稿したコメントに戻ります。",
      located: "ビューアが閉じ、v1 をアップロードしたコメントが表示されました。「もう一度」で最初から試せます。",
    },
  },
  ko: {
    title: "OPS-52 · 팀 회식 식당 후보 비교",
    agent: "운영 도우미",
    member: "리레이",
    fileName: "식당-비교.pdf",
    fileType: "PDF",
    size: { 1: "84 KB", 2: "86 KB" } as Record<Version, string>,
    uploadedAt: { 1: "2026. 10. 20. 오후 2:05", 2: "2026. 10. 21. 오전 10:40" } as Record<Version, string>,
    comments: {
      first: "후보 식당 세 곳 비교를 첨부했습니다. 회사에서 가까운 순입니다.",
      feedback: "두 번째 집은 룸이 무료예요. 총액 순으로 바꿔 줄래요?",
      second: "총액 순으로 다시 정렬했습니다.",
    },
    sectionTitle: "산출물",
    viewAll: "산출물 1개 모두 보기",
    versionShort: (v: Version) => `v${v}`,
    versionLabel: (v: Version) => `버전 ${v} / 전체 2개`,
    versionOf: (v: Version) => `v${v} / 전체 2개`,
    locate: "댓글에서 보기",
    close: "닫기",
    page: {
      heading: "팀 회식 · 12명",
      sortedBy: { 1: "거리순", 2: "총액순" } as Record<Version, string>,
      rows: {
        1: ["강변식당 · 도보 5분", "연못집 · 도보 8분", "벽돌집 · 도보 12분"],
        2: ["연못집 · 46만 원, 룸 무료", "강변식당 · 49만 원", "벽돌집 · 51만 원"],
      } as Record<Version, string[]>,
    },
    captions: {
      idle: "에이전트가 파일을 올리고, 의견을 받은 뒤 다시 올린 태스크입니다.",
      start: "두 번 올린 파일은 이름과 형식이 같아서 산출물에는 하나만 v2로 표시됩니다. 열어 보세요.",
      viewer: "뷰어는 최신 버전으로 열립니다. 버전 번호를 클릭하면 다른 버전을 볼 수 있습니다.",
      versions: "새 버전이 위에 있습니다. v1을 고르세요.",
      v1: "지금 보는 것은 첫 버전입니다. '댓글에서 보기'를 누르면 이 파일을 올린 댓글로 돌아갑니다.",
      located: "뷰어가 닫히고 v1을 올린 댓글이 표시됐습니다. '다시 보기'로 처음부터 해 보세요.",
    },
  },
  fr: {
    title: "OPS-52 · Comparer les restaurants du dîner d'équipe",
    agent: "Assistant ops",
    member: "Lei Li",
    fileName: "comparatif.pdf",
    fileType: "PDF",
    size: { 1: "84 Ko", 2: "86 Ko" } as Record<Version, string>,
    uploadedAt: { 1: "20 oct. 2026, 14:05", 2: "21 oct. 2026, 10:40" } as Record<Version, string>,
    comments: {
      first: "Le comparatif des trois restaurants est en pièce jointe, trié par distance du bureau.",
      feedback: "Le deuxième offre le salon privé. Tu peux trier par prix total ?",
      second: "Trié par prix total.",
    },
    sectionTitle: "Livrables",
    viewAll: "Voir le livrable",
    versionShort: (v: Version) => `v${v}`,
    versionLabel: (v: Version) => `Version ${v} sur 2`,
    versionOf: (v: Version) => `v${v} sur 2`,
    locate: "Afficher dans les commentaires",
    close: "Fermer",
    page: {
      heading: "Dîner d'équipe · 12 personnes",
      sortedBy: { 1: "Trié par distance", 2: "Trié par prix total" } as Record<Version, string>,
      rows: {
        1: ["Le Quai · 5 min à pied", "Le Jardin du Lotus · 8 min à pied", "La Brique · 12 min à pied"],
        2: ["Le Jardin du Lotus · 380 €, salon offert", "Le Quai · 410 €", "La Brique · 425 €"],
      } as Record<Version, string[]>,
    },
    captions: {
      idle: "Une tâche où l'agent a envoyé un fichier, reçu un retour, puis l'a renvoyé.",
      start: "Les deux envois ont le même nom et le même type : Livrables n'affiche qu'un fichier, marqué v2. Ouvrez-le.",
      viewer: "La visionneuse s'ouvre sur la dernière version. Cliquez sur le numéro de version pour voir les autres.",
      versions: "La plus récente en haut. Choisissez v1.",
      v1: "Voici la première version. Afficher dans les commentaires ramène au commentaire qui l'a publiée.",
      located: "La visionneuse s'est fermée sur le commentaire qui a envoyé la v1. Utilisez Rejouer pour recommencer.",
    },
  },
};

type Phase = "idle" | "start" | "viewer" | "versions" | "v1" | "located";

export function DeliverablesDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");

  const shownPhase: Phase = visible ? phase : "idle";
  const viewerOpen = phase === "viewer" || phase === "versions" || phase === "v1";
  const version: Version = phase === "v1" ? 1 : 2;
  const replay = () => setPhase("start");

  const attachment = (v: Version) => (
    <span className="mt-1.5 flex w-full max-w-64 items-center gap-2 rounded-md border bg-card px-2 py-1.5 text-caption">
      <FileText aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
      <span className="min-w-0 truncate">{t.fileName}</span>
      <VersionBadge label={t.versionShort(v)} />
      <span className="ml-auto shrink-0 text-micro text-muted-foreground tabular-nums">{t.size[v]}</span>
    </span>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative min-h-96">
        <div className={cn("grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_13rem]", viewerOpen && "invisible")}>
          <div className="flex min-w-0 flex-col gap-2">
            <Comment
              author={t.agent}
              agent
              highlighted={phase === "located"}
              body={t.comments.first}
              attachment={attachment(1)}
            />
            <Comment author={t.member} body={t.comments.feedback} />
            <Comment author={t.agent} agent body={t.comments.second} attachment={attachment(2)} />
          </div>

          <aside className="order-first min-w-0 sm:order-none">
            <div className="mb-1.5 flex items-center gap-1 px-1 text-caption font-medium">
              {t.sectionTitle}
              <span className="rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground tabular-nums">1</span>
              <ChevronRight aria-hidden className="size-3 rotate-90 text-muted-foreground" />
            </div>
            <CoachMark active={shownPhase === "start"} side="bottom" align="end" ringClassName="rounded-md" className="flex w-full">
              <button
                type="button"
                aria-label={t.fileName}
                disabled={!visible || phase !== "start"}
                onClick={() => setPhase("viewer")}
                className="flex w-full items-center gap-2 rounded-md px-1 py-1 text-left text-caption hover:bg-accent/50 disabled:hover:bg-transparent"
              >
                <FileText aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 truncate">{t.fileName}</span>
                <VersionBadge label={t.versionShort(2)} />
                <span className="ml-auto shrink-0 text-micro text-muted-foreground tabular-nums">{t.size[2]}</span>
              </button>
            </CoachMark>
            <span className="mt-0.5 flex items-center gap-2 px-1 py-1 text-caption text-muted-foreground">
              <LayoutGrid aria-hidden className="size-3.5 shrink-0" />
              {t.viewAll}
            </span>
          </aside>
        </div>

        {viewerOpen && (
          <div
            role="dialog"
            aria-label={t.fileName}
            className="dark absolute inset-0 z-20 flex flex-col bg-black/95 text-foreground motion-safe:animate-in motion-safe:fade-in"
          >
            <div className="flex items-center gap-2 border-b border-white/10 px-3 py-2">
              <FileText aria-hidden className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 truncate text-body font-medium">{t.fileName}</span>
              <span className="relative shrink-0">
                <CoachMark active={phase === "viewer"} side="bottom" align="start" ringClassName="rounded-md">
                  <button
                    type="button"
                    aria-label={t.versionLabel(version)}
                    aria-haspopup="menu"
                    aria-expanded={phase === "versions"}
                    disabled={phase !== "viewer" && phase !== "versions"}
                    onClick={() => setPhase(phase === "versions" ? "viewer" : "versions")}
                    className="flex items-center gap-0.5 rounded-xs bg-secondary px-1.5 py-0.5 text-micro font-medium text-muted-foreground tabular-nums hover:text-foreground"
                  >
                    {t.versionShort(version)}
                    <ChevronDown aria-hidden className="size-3" />
                  </button>
                </CoachMark>
                {phase === "versions" && (
                  <div
                    role="menu"
                    aria-label={t.versionLabel(version)}
                    className="absolute top-full left-0 z-10 mt-1 w-60 max-w-[calc(100vw-3rem)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
                  >
                    {([2, 1] as Version[]).map((v) => {
                      const row = (
                        <button
                          type="button"
                          role="menuitem"
                          disabled={v !== 1}
                          onClick={() => setPhase("v1")}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:hover:bg-transparent"
                        >
                          <span className="w-6 font-medium tabular-nums">{t.versionShort(v)}</span>
                          <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground tabular-nums">
                            {t.uploadedAt[v]} · {t.size[v]}
                          </span>
                          {v === version && <Check aria-hidden className="size-3.5 shrink-0" />}
                        </button>
                      );
                      return v === 1 ? (
                        <CoachMark key={v} active quiet ringClassName="rounded-md" className="flex w-full">
                          {row}
                        </CoachMark>
                      ) : (
                        <div key={v}>{row}</div>
                      );
                    })}
                  </div>
                )}
              </span>
              <span className="ml-auto flex shrink-0 items-center gap-1">
                <CoachMark active={phase === "v1"} side="bottom" align="end" ringClassName="rounded-md">
                  <button
                    type="button"
                    aria-label={t.locate}
                    disabled={phase !== "v1"}
                    onClick={() => setPhase("located")}
                    className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-caption text-muted-foreground hover:bg-secondary hover:text-foreground disabled:hover:bg-transparent"
                  >
                    <MessageSquareText aria-hidden className="size-3.5" />
                    <span className="max-sm:hidden">{t.locate}</span>
                  </button>
                </CoachMark>
                <button
                  type="button"
                  disabled
                  aria-label={t.close}
                  className="rounded-md p-1 text-muted-foreground opacity-60"
                >
                  <X className="size-4" />
                </button>
              </span>
            </div>

            <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-2 p-4">
              <div
                key={version}
                className="w-full max-w-72 rounded-md bg-white px-4 py-3 text-neutral-900 shadow-lg motion-safe:animate-in motion-safe:fade-in"
              >
                <div className="text-label font-medium">{t.page.heading}</div>
                <div className="mt-0.5 text-micro text-neutral-500">{t.page.sortedBy[version]}</div>
                <ol className="mt-2 flex list-decimal flex-col gap-1 ps-4 text-caption">
                  {t.page.rows[version].map((row) => (
                    <li key={row} className="m-0">
                      {row}
                    </li>
                  ))}
                </ol>
              </div>
              <div className="text-caption text-muted-foreground tabular-nums">
                {t.fileType} · {t.size[version]} · {t.versionOf(version)}
              </div>
            </div>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}

function VersionBadge({ label }: { label: string }) {
  return (
    <span
      aria-hidden
      className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground tabular-nums"
    >
      {label}
    </span>
  );
}

function Comment({
  author,
  agent = false,
  highlighted = false,
  body,
  attachment,
}: {
  author: string;
  agent?: boolean;
  highlighted?: boolean;
  body: string;
  attachment?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "min-w-0 rounded-lg border bg-background px-3 py-2 transition-colors",
        highlighted && "border-brand/50 bg-brand/5 ring-2 ring-brand/30",
      )}
    >
      <div className="flex items-center gap-2">
        <DemoAvatar name={author} agent={agent} />
        <span className="text-label font-medium">{author}</span>
      </div>
      <div className="mt-1 pl-7 text-body break-words">
        {body}
        {attachment}
      </div>
    </div>
  );
}
