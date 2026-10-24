"use client";

import { useState } from "react";
import { Archive, ArchiveRestore, ChevronLeft, ChevronRight, CircleCheck, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoComment, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Notice = {
  id: string;
  issue: string;
  title: string;
  actor: string;
  agent: boolean;
  detail: string;
  time: string;
  /** What the issue shows when the notification is opened. */
  body: string;
  /** A mention that opens the comment body. */
  mention?: string;
  failed?: boolean;
};

const copy = {
  en: {
    title: "Inbox",
    heading: "Inbox",
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "Sort out March expense claims",
        actor: "Lei Li",
        agent: false,
        detail: "Mentioned",
        time: "5m",
        mention: "@You",
        body: "Two March invoices are still missing. Can you send them by Friday?",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "Update the customer service FAQ",
        actor: "Ops helper",
        agent: true,
        detail: "Run failed",
        time: "20m",
        body: "Ops helper's run failed before it finished.",
        failed: true,
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "Prepare the quarterly supplier review",
        actor: "Lei Li",
        agent: false,
        detail: "Assigned to You",
        time: "1h",
        body: "",
      },
      {
        id: "comment",
        issue: "OPS-44",
        title: "Write the November campaign post",
        actor: "Writing helper",
        agent: true,
        detail: "The first draft is in the comments. Pick one of the two titles.",
        time: "3h",
        body: "",
      },
    ] as Notice[],
    selectPrompt: "Select a notification to view details",
    archive: "Archive",
    unarchive: "Unarchive",
    archivedTitle: "Archived",
    archivedToast: "Notification archived",
    runFailed: "Run failed",
    captions: {
      idle: "Updates that concern you collect in the inbox.",
      start: "Four unread notifications. Open the one where someone mentioned you.",
      opened:
        "Opening it marked it read, and the detail shows its issue. When you're done with it, click Archive.",
      archived:
        "Archiving takes the notification out of the list and leaves the issue alone. The next one opened and was marked read. Click Archived at the bottom.",
      archivedView:
        "Archived notifications keep their read state and can be unarchived from here. Use Replay to watch again.",
    },
  },
  zh: {
    title: "收件箱",
    heading: "收件箱",
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "整理三月报销单",
        actor: "李雷",
        agent: false,
        detail: "提及了你",
        time: "5 分钟",
        mention: "@你",
        body: "三月还差两张发票，周五前能补上吗？",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "更新客服常见问题",
        actor: "运营助手",
        agent: true,
        detail: "运行失败",
        time: "20 分钟",
        body: "运营助手的运行没有完成。",
        failed: true,
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "准备季度供应商评估",
        actor: "李雷",
        agent: false,
        detail: "分配给 你",
        time: "1 小时",
        body: "",
      },
      {
        id: "comment",
        issue: "OPS-44",
        title: "写 11 月活动推文",
        actor: "文案助手",
        agent: true,
        detail: "初稿放在评论里了，标题有两个版本，请选一个。",
        time: "3 小时",
        body: "",
      },
    ] as Notice[],
    selectPrompt: "选择一条通知查看详情",
    archive: "归档",
    unarchive: "取消归档",
    archivedTitle: "已归档",
    archivedToast: "已归档通知",
    runFailed: "运行失败",
    captions: {
      idle: "和你有关的动态集中在收件箱。",
      start: "四条未读通知。打开有人 @你 的那一条。",
      opened: "打开后这条通知标为已读，详情里显示对应的任务。处理完点\"归档\"。",
      archived: "归档只把通知移出列表，任务不受影响。下一条通知自动打开，也标为已读。点底部的\"已归档\"。",
      archivedView: "归档的通知保留已读状态，可以在这里取消归档。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "インボックス",
    heading: "インボックス",
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "3 月の経費精算を整理する",
        actor: "李雷",
        agent: false,
        detail: "メンションされました",
        time: "5分",
        mention: "@あなた",
        body: "3 月分の領収書がまだ 2 枚足りません。金曜までに出してもらえますか？",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "カスタマーサポートの FAQ を更新する",
        actor: "運用アシスタント",
        agent: true,
        detail: "実行失敗",
        time: "20分",
        body: "運用アシスタントの実行は、終わる前に失敗しました。",
        failed: true,
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "四半期のサプライヤー評価を準備する",
        actor: "李雷",
        agent: false,
        detail: "あなた に割り当て",
        time: "1時間",
        body: "",
      },
      {
        id: "comment",
        issue: "OPS-44",
        title: "11 月のキャンペーン投稿を書く",
        actor: "ライティングアシスタント",
        agent: true,
        detail: "初稿をコメントに載せました。タイトル案が 2 つあるので、どちらか選んでください。",
        time: "3時間",
        body: "",
      },
    ] as Notice[],
    selectPrompt: "詳細を表示する通知を選択してください",
    archive: "アーカイブ",
    unarchive: "アーカイブ解除",
    archivedTitle: "アーカイブ済み",
    archivedToast: "通知をアーカイブしました",
    runFailed: "実行失敗",
    captions: {
      idle: "あなたに関係する更新はインボックスに集まります。",
      start: "未読の通知が 4 件あります。あなたがメンションされた通知を開きます。",
      opened: "開いた通知は既読になり、詳細には対象のタスクが表示されます。対応が済んだら「アーカイブ」をクリックします。",
      archived:
        "アーカイブしても通知がリストから外れるだけで、タスクには影響しません。次の通知が開いて既読になりました。下にある「アーカイブ済み」をクリックします。",
      archivedView: "アーカイブした通知は既読のまま残り、ここからアーカイブを解除できます。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "인박스",
    heading: "인박스",
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "3월 경비 청구 정리",
        actor: "리레이",
        agent: false,
        detail: "멘션됨",
        time: "5분",
        mention: "@나",
        body: "3월 영수증이 아직 두 장 빠져 있어요. 금요일까지 보내 주실 수 있나요?",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "고객 지원 FAQ 업데이트",
        actor: "운영 도우미",
        agent: true,
        detail: "실행 실패함",
        time: "20분",
        body: "운영 도우미의 실행이 끝나기 전에 실패했습니다.",
        failed: true,
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "분기별 공급업체 평가 준비",
        actor: "리레이",
        agent: false,
        detail: "나에게 할당",
        time: "1시간",
        body: "",
      },
      {
        id: "comment",
        issue: "OPS-44",
        title: "11월 캠페인 게시물 작성",
        actor: "글쓰기 도우미",
        agent: true,
        detail: "초안을 댓글에 올렸습니다. 제목이 두 가지인데 하나를 골라 주세요.",
        time: "3시간",
        body: "",
      },
    ] as Notice[],
    selectPrompt: "자세히 볼 알림을 선택하세요",
    archive: "보관",
    unarchive: "보관 해제",
    archivedTitle: "보관됨",
    archivedToast: "알림을 보관했습니다",
    runFailed: "실행 실패함",
    captions: {
      idle: "나와 관련된 업데이트는 인박스에 모입니다.",
      start: "읽지 않은 알림이 4개 있습니다. 누군가 나를 멘션한 알림을 여세요.",
      opened: "연 알림은 읽음으로 표시되고, 상세에는 해당 태스크가 보입니다. 처리가 끝나면 '보관'을 클릭하세요.",
      archived:
        "보관하면 알림만 목록에서 빠지고 태스크는 그대로입니다. 다음 알림이 열리면서 읽음으로 표시되었습니다. 아래쪽의 '보관됨'을 클릭하세요.",
      archivedView: "보관한 알림은 읽음 상태 그대로 남고, 여기서 보관을 해제할 수 있습니다. '다시 보기'로 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Boîte de réception",
    heading: "Boîte de réception",
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "Régulariser les notes de frais de mars",
        actor: "Lei Li",
        agent: false,
        detail: "Mentionné",
        time: "5 min",
        mention: "@Vous",
        body: "Il manque encore deux factures de mars. Pouvez-vous les envoyer d'ici vendredi ?",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "Mettre à jour la FAQ du service client",
        actor: "Assistant ops",
        agent: true,
        detail: "Exécution en échec",
        time: "20 min",
        body: "L'exécution d'Assistant ops a échoué avant la fin.",
        failed: true,
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "Préparer la revue trimestrielle des fournisseurs",
        actor: "Lei Li",
        agent: false,
        detail: "Assigné à vous",
        time: "1 h",
        body: "",
      },
      {
        id: "comment",
        issue: "OPS-44",
        title: "Rédiger le post de la campagne de novembre",
        actor: "Assistant rédaction",
        agent: true,
        detail: "Le premier jet est dans les commentaires. Choisissez l'un des deux titres.",
        time: "3 h",
        body: "",
      },
    ] as Notice[],
    selectPrompt: "Sélectionnez une notification pour afficher les détails",
    archive: "Archiver",
    unarchive: "Désarchiver",
    archivedTitle: "Archivées",
    archivedToast: "Notification archivée",
    runFailed: "Exécution en échec",
    captions: {
      idle: "Les mises à jour qui vous concernent arrivent dans la boîte de réception.",
      start: "Quatre notifications non lues. Ouvrez celle où quelqu'un vous a mentionné.",
      opened:
        "En l'ouvrant, vous l'avez marquée comme lue, et le détail affiche sa tâche. Une fois qu'elle est traitée, cliquez sur Archiver.",
      archived:
        "L'archivage retire la notification de la liste sans toucher à la tâche. La suivante s'est ouverte et a été marquée comme lue. Cliquez sur Archivées en bas.",
      archivedView:
        "Les notifications archivées gardent leur état de lecture et peuvent être désarchivées ici. Utilisez Rejouer pour revoir la démo.",
    },
  },
};

type Phase = "idle" | "start" | "opened" | "archived" | "archivedView";

export function InboxDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");

  const shownPhase: Phase = visible ? phase : "idle";
  const [mention, failed] = t.notices;
  const archivedIds = phase === "archived" || phase === "archivedView" ? ["mention"] : [];
  const readIds =
    phase === "start" ? [] : phase === "opened" ? ["mention"] : ["mention", "failed"];
  const selectedId = phase === "opened" ? "mention" : phase === "archived" ? "failed" : phase === "archivedView" ? "mention" : null;
  const selected = t.notices.find((n) => n.id === selectedId) ?? null;
  const archivedView = phase === "archivedView";

  const rows = archivedView ? [mention!] : t.notices.filter((n) => !archivedIds.includes(n.id));
  const unread = t.notices.filter((n) => !readIds.includes(n.id) && !archivedIds.includes(n.id)).length;

  const replay = () => setPhase("start");

  const list = (
    <div className="flex min-w-0 flex-col sm:w-[52%] sm:shrink-0 sm:border-r">
      <div className="flex h-10 items-center gap-2 border-b px-3">
        <span className="text-label font-medium">{t.heading}</span>
        {unread > 0 && (
          <span className="rounded-full bg-brand/12 px-1.5 text-micro font-medium text-brand">{unread}</span>
        )}
      </div>
      {archivedView && (
        <div className="flex items-center gap-1.5 border-b px-3 py-2 text-caption font-medium text-muted-foreground">
          <ChevronLeft aria-hidden className="size-4 shrink-0" />
          {t.archivedTitle}
        </div>
      )}
      <div className="flex flex-1 flex-col gap-0.5 p-1.5">
        {rows.map((notice) => {
          const isUnread = !readIds.includes(notice.id) && !archivedView;
          const clickable = visible && phase === "start" && notice.id === "mention";
          return (
            <CoachMark
              key={notice.id}
              active={clickable}
              side="top"
              align="end"
              ringClassName="rounded-md"
              className="flex w-full"
            >
              <button
                type="button"
                disabled={!clickable}
                onClick={() => setPhase("opened")}
                className={cn(
                  "flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left motion-safe:animate-in motion-safe:fade-in",
                  notice.id === selectedId ? "bg-accent" : "enabled:hover:bg-accent/50",
                )}
              >
                <span className="flex size-7 shrink-0 items-center justify-center">
                  <DemoAvatar name={notice.actor} agent={notice.agent} />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex min-w-0 items-center gap-1.5">
                    {isUnread && <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />}
                    <span className={cn("truncate text-body", isUnread ? "font-medium" : "text-muted-foreground")}>
                      {notice.title}
                    </span>
                  </span>
                  <span className="mt-0.5 flex min-w-0 items-center justify-between gap-2 text-caption text-muted-foreground">
                    <span className="truncate">{notice.detail}</span>
                    <span className="shrink-0">{notice.time}</span>
                  </span>
                </span>
              </button>
            </CoachMark>
          );
        })}
      </div>
      {!archivedView && (
        <div className="border-t p-1.5">
          <CoachMark quiet active={phase === "archived"} ringClassName="rounded-md" className="flex w-full">
            <button
              type="button"
              disabled={phase !== "archived"}
              onClick={() => setPhase("archivedView")}
              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption text-muted-foreground enabled:hover:bg-accent/50"
            >
              <Archive aria-hidden className="size-4" />
              <span className="flex-1">{t.archivedTitle}</span>
              <ChevronRight aria-hidden className="size-3.5" />
            </button>
          </CoachMark>
        </div>
      )}
    </div>
  );

  const detail = selected ? (
    <div key={selected.id + phase} className="flex min-w-0 flex-1 flex-col motion-safe:animate-in motion-safe:fade-in">
      <div className="flex h-10 items-center gap-2 border-b px-3">
        <span className="shrink-0 text-caption text-muted-foreground">{selected.issue}</span>
        <span className="min-w-0 flex-1 truncate text-label font-medium">{selected.title}</span>
        <CoachMark active={phase === "opened"} side="bottom" align="end" ringClassName="rounded-md">
          <Button type="button" variant="outline" size="xs" disabled={phase !== "opened"} onClick={() => setPhase("archived")}>
            {archivedView ? <ArchiveRestore /> : <Archive />}
            {archivedView ? t.unarchive : t.archive}
          </Button>
        </CoachMark>
      </div>
      <div className="flex flex-col gap-2 p-3">
        {selected.failed ? (
          <div className="flex items-center gap-2 text-body">
            <DemoAvatar name={selected.actor} agent />
            <span className="font-medium">{selected.actor}</span>
            <span className="inline-flex items-center gap-1 text-caption text-destructive">
              <X aria-hidden className="size-3" />
              {t.runFailed}
            </span>
          </div>
        ) : (
          <DemoComment author={selected.actor} agent={selected.agent}>
            {selected.mention && <span className="font-medium text-brand">{selected.mention} </span>}
            {selected.body}
          </DemoComment>
        )}
        {selected.failed && <p className="pl-7 text-caption text-muted-foreground">{selected.body}</p>}
      </div>
    </div>
  ) : (
    <div className="flex min-h-32 flex-1 items-center justify-center p-4 text-caption text-muted-foreground">
      {t.selectPrompt}
    </div>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-96 flex-col max-sm:divide-y sm:flex-row">
        {list}
        {detail}
        {phase === "archived" && (
          <div className="absolute right-3 bottom-3 z-20 flex items-center gap-2 rounded-lg border bg-popover px-3 py-2 text-caption text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2">
            <CircleCheck aria-hidden className="size-3.5 text-success" />
            {t.archivedToast}
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
