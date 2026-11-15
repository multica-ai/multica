"use client";

import { useState, type ReactNode } from "react";
import {
  Archive,
  ArchiveRestore,
  Bot,
  Check,
  ChevronRight,
  Circle,
  CircleCheck,
  CircleDashed,
  CircleDot,
  Filter,
  MoreHorizontal,
  SlidersHorizontal,
  X,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { Lang } from "@/lib/i18n";
import { CoachMark, DemoAvatar, DemoComment, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type NoticeId = "mention" | "draft" | "failed" | "assigned";

type Notice = {
  id: NoticeId;
  issue: string;
  title: string;
  actor: string;
  agent: boolean;
  /** Who did what, under the title. */
  detail: string;
  time: string;
  /** What the issue shows when the notification is opened. */
  body: string;
  /** A mention that opens the comment body. */
  mention?: string;
};

type Copy = {
  title: string;
  inbox: string;
  /** The Work panel's other sections, below the Inbox. */
  sections: string[];
  /** Newest first, as the section lists them. */
  notices: Notice[];
  selectPrompt: string;
  markRead: string;
  markUnread: string;
  markDone: string;
  archive: string;
  archivedToast: string;
  runFailed: string;
  /** The section's ⋯ menu: View archived first, then the bulk actions. */
  menu: [string, ...string[]];
  menuLabel: string;
  archivedTitle: string;
  unarchive: string;
  captions: Record<Phase, string>;
};

type Phase = "idle" | "start" | "opened" | "archived" | "done" | "menu" | "archivedView";

const copy: { en: Copy } & Partial<Record<Lang, Copy>> = {
  en: {
    title: "Work",
    inbox: "Inbox",
    sections: ["Pinned", "Views", "Projects"],
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "Sort out March expense claims",
        actor: "Lei Li",
        agent: false,
        detail: "Mentioned you",
        time: "5m",
        mention: "@You",
        body: "Two March invoices are still missing. Can you send them by Friday?",
      },
      {
        id: "draft",
        issue: "OPS-44",
        title: "Write the November campaign post",
        actor: "Writing helper",
        agent: true,
        detail: "The first draft is in the comments.",
        time: "20m",
        body: "The first draft is ready. Pick one of the two titles and I'll finish it.",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "Update the customer service FAQ",
        actor: "Ops helper",
        agent: true,
        detail: "Run failed",
        time: "1h",
        body: "Ops helper's run failed before it finished.",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "Prepare the quarterly supplier review",
        actor: "Lei Li",
        agent: false,
        detail: "Assigned to you",
        time: "3h",
        body: "",
      },
    ],
    selectPrompt: "Open a notification from the Inbox",
    markRead: "Mark as read",
    markUnread: "Mark as unread",
    markDone: "Mark as done",
    archive: "Archive",
    archivedToast: "Notification archived",
    runFailed: "Run failed",
    menu: ["View archived", "Mark all as read", "Archive all read", "Archive completed", "Archive all"],
    menuLabel: "Inbox actions",
    archivedTitle: "Archived",
    unarchive: "Unarchive",
    captions: {
      idle: "Notifications that concern you sit at the top of the Work panel.",
      start: "Three unread notifications. Open the one where someone mentioned you.",
      opened:
        "The issue opened at the comment. The notification turned read and stayed in the list. When you're done with it, click Archive (E).",
      archived:
        "The row left the Inbox and the next notification opened: the draft is in. Click Mark as done to finish the issue.",
      done: "You marked the issue done, so its notification was archived and the next one opened. Archived notifications are in the section's ⋯ menu. Click ⋯.",
      menu: "The ⋯ menu holds View archived and the bulk actions. Click View archived.",
      archivedView:
        "Archived notifications keep their read state, and you can unarchive any of them here. Use Replay to watch again.",
    },
  },
  zh: {
    title: "工作",
    inbox: "收件箱",
    sections: ["固定", "视图", "项目"],
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
        id: "draft",
        issue: "OPS-44",
        title: "写 11 月活动推文",
        actor: "文案助手",
        agent: true,
        detail: "初稿放在评论里了。",
        time: "20 分钟",
        body: "初稿写好了。两个标题选一个，我来收尾。",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "更新客服常见问题",
        actor: "运营助手",
        agent: true,
        detail: "运行失败",
        time: "1 小时",
        body: "运营助手的运行没有完成。",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "准备季度供应商评估",
        actor: "李雷",
        agent: false,
        detail: "分配给你",
        time: "3 小时",
        body: "",
      },
    ],
    selectPrompt: "从收件箱打开一条通知",
    markRead: "标为已读",
    markUnread: "标为未读",
    markDone: "标记为已完成",
    archive: "归档",
    archivedToast: "已归档通知",
    runFailed: "运行失败",
    menu: ["查看已归档", "全部标为已读", "归档全部已读", "归档已完成", "归档全部"],
    menuLabel: "收件箱操作",
    archivedTitle: "已归档",
    unarchive: "取消归档",
    captions: {
      idle: "和你有关的通知在工作面板的最上方。",
      start: "三条未读通知。打开有人 @你 的那一条。",
      opened: "任务打开并定位到那条评论。通知变为已读，但仍留在列表里。处理完点\"归档\"（E）。",
      archived: "这一行离开了收件箱，下一条通知自动打开：初稿到了。点\"标记为已完成\"结束这个任务。",
      done: "你把任务标记为已完成，这条通知随之归档，下一条自动打开。已归档的通知在分区的 ⋯ 菜单里。点 ⋯。",
      menu: "⋯ 菜单里有\"查看已归档\"和批量操作。点\"查看已归档\"。",
      archivedView: "已归档的通知保留已读状态，可以在这里取消归档。点右上角可以重新演示。",
    },
  },
  "zh-Hant": {
    title: "工作",
    inbox: "收件箱",
    sections: ["固定", "檢視", "專案"],
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "整理三月報銷單",
        actor: "李雷",
        agent: false,
        detail: "提及了你",
        time: "5 分鐘",
        mention: "@你",
        body: "三月還差兩張發票，週五前能補上嗎？",
      },
      {
        id: "draft",
        issue: "OPS-44",
        title: "寫 11 月活動推文",
        actor: "文案助手",
        agent: true,
        detail: "初稿放在評論裡了。",
        time: "20 分鐘",
        body: "初稿寫好了。兩個標題選一個，我來收尾。",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "更新客服常見問題",
        actor: "營運助手",
        agent: true,
        detail: "執行失敗",
        time: "1 小時",
        body: "營運助手的執行沒有完成。",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "準備季度供應商評估",
        actor: "李雷",
        agent: false,
        detail: "分配給你",
        time: "3 小時",
        body: "",
      },
    ],
    selectPrompt: "從收件箱開啟一條通知",
    markRead: "標為已讀",
    markUnread: "標為未讀",
    markDone: "標記為已完成",
    archive: "歸檔",
    archivedToast: "已歸檔通知",
    runFailed: "執行失敗",
    menu: ["查看已歸檔", "全部標為已讀", "歸檔全部已讀", "歸檔已完成", "歸檔全部"],
    menuLabel: "收件箱操作",
    archivedTitle: "已歸檔",
    unarchive: "取消歸檔",
    captions: {
      idle: "和你有關的通知在工作面板的最上方。",
      start: "三條未讀通知。開啟有人 @你 的那一條。",
      opened: "任務開啟並定位到那條評論。通知變為已讀，但仍留在列表裡。處理完點\"歸檔\"（E）。",
      archived: "這一列離開了收件箱，下一條通知自動開啟：初稿到了。點\"標記為已完成\"結束這個任務。",
      done: "你把任務標記為已完成，這條通知隨之歸檔，下一條自動開啟。已歸檔的通知在分區的 ⋯ 選單裡。點 ⋯。",
      menu: "⋯ 選單裡有\"查看已歸檔\"和批次操作。點\"查看已歸檔\"。",
      archivedView: "已歸檔的通知保留已讀狀態，可以在這裡取消歸檔。點右上角可以重新播放。",
    },
  },
  ja: {
    title: "作業",
    inbox: "インボックス",
    sections: ["ピン留め済み", "ビュー", "プロジェクト"],
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
        id: "draft",
        issue: "OPS-44",
        title: "11 月のキャンペーン投稿を書く",
        actor: "ライティングアシスタント",
        agent: true,
        detail: "初稿をコメントに載せました。",
        time: "20分",
        body: "初稿ができました。タイトル案を 2 つから選んでもらえれば仕上げます。",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "カスタマーサポートの FAQ を更新する",
        actor: "運用アシスタント",
        agent: true,
        detail: "実行失敗",
        time: "1時間",
        body: "運用アシスタントの実行は、終わる前に失敗しました。",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "四半期のサプライヤー評価を準備する",
        actor: "李雷",
        agent: false,
        detail: "あなた に割り当て",
        time: "3時間",
        body: "",
      },
    ],
    selectPrompt: "インボックスから通知を開いてください",
    markRead: "既読にする",
    markUnread: "未読にする",
    markDone: "完了にする",
    archive: "アーカイブ",
    archivedToast: "通知をアーカイブしました",
    runFailed: "実行失敗",
    menu: [
      "アーカイブ済みを表示",
      "すべて既読にする",
      "既読の項目をすべてアーカイブ",
      "完了した項目をアーカイブ",
      "すべてアーカイブ",
    ],
    menuLabel: "インボックスの操作",
    archivedTitle: "アーカイブ済み",
    unarchive: "アーカイブ解除",
    captions: {
      idle: "あなたに関係する通知は作業パネルの一番上に並びます。",
      start: "未読の通知が 3 件あります。あなたがメンションされた通知を開きます。",
      opened:
        "タスクがそのコメントの位置で開きました。通知は既読になりましたが、リストには残っています。対応が済んだら「アーカイブ」（E）をクリックします。",
      archived:
        "行がインボックスから外れ、次の通知が開きました。初稿が届いています。「完了にする」をクリックしてタスクを終えます。",
      done: "タスクを完了にしたので通知はアーカイブされ、次の通知が開きました。アーカイブした通知はセクションの ⋯ メニューから見られます。⋯ をクリックします。",
      menu: "⋯ メニューには「アーカイブ済みを表示」と一括操作があります。「アーカイブ済みを表示」をクリックします。",
      archivedView: "アーカイブした通知は既読のまま残り、ここからアーカイブを解除できます。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "작업",
    inbox: "인박스",
    sections: ["고정됨", "뷰", "프로젝트"],
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
        id: "draft",
        issue: "OPS-44",
        title: "11월 캠페인 게시물 작성",
        actor: "글쓰기 도우미",
        agent: true,
        detail: "초안을 댓글에 올렸습니다.",
        time: "20분",
        body: "초안이 준비됐습니다. 제목 두 가지 중 하나를 골라 주시면 마무리하겠습니다.",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "고객 지원 FAQ 업데이트",
        actor: "운영 도우미",
        agent: true,
        detail: "실행 실패함",
        time: "1시간",
        body: "운영 도우미의 실행이 끝나기 전에 실패했습니다.",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "분기별 공급업체 평가 준비",
        actor: "리레이",
        agent: false,
        detail: "나에게 할당",
        time: "3시간",
        body: "",
      },
    ],
    selectPrompt: "인박스에서 알림을 여세요",
    markRead: "읽음으로 표시",
    markUnread: "읽지 않음으로 표시",
    markDone: "완료로 표시",
    archive: "보관",
    archivedToast: "알림을 보관했습니다",
    runFailed: "실행 실패함",
    menu: ["보관된 알림 보기", "모두 읽음으로 표시", "읽은 항목 모두 보관", "완료된 항목 보관", "모두 보관"],
    menuLabel: "인박스 작업",
    archivedTitle: "보관됨",
    unarchive: "보관 해제",
    captions: {
      idle: "나와 관련된 알림은 작업 패널 맨 위에 모입니다.",
      start: "읽지 않은 알림이 3개 있습니다. 누군가 나를 멘션한 알림을 여세요.",
      opened:
        "태스크가 그 댓글 위치에서 열렸습니다. 알림은 읽음이 되었지만 목록에 그대로 남아 있습니다. 처리가 끝나면 '보관'(E)을 클릭하세요.",
      archived: "행이 인박스에서 빠지고 다음 알림이 열렸습니다. 초안이 도착했습니다. '완료로 표시'를 클릭해 태스크를 끝내세요.",
      done: "완료로 표시해서 알림이 보관되고 다음 알림이 열렸습니다. 보관된 알림은 섹션의 ⋯ 메뉴에서 볼 수 있습니다. ⋯을 클릭하세요.",
      menu: "⋯ 메뉴에는 '보관된 알림 보기'와 일괄 작업이 있습니다. '보관된 알림 보기'를 클릭하세요.",
      archivedView: "보관된 알림은 읽음 상태를 유지하고, 여기서 보관을 해제할 수 있습니다. '다시 보기'로 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Travail",
    inbox: "Boîte de réception",
    sections: ["Épinglés", "Vues", "Projets"],
    notices: [
      {
        id: "mention",
        issue: "OPS-41",
        title: "Régulariser les notes de frais de mars",
        actor: "Lei Li",
        agent: false,
        detail: "Vous a mentionné",
        time: "5 min",
        mention: "@Vous",
        body: "Il manque encore deux factures de mars. Pouvez-vous les envoyer d'ici vendredi ?",
      },
      {
        id: "draft",
        issue: "OPS-44",
        title: "Rédiger le post de la campagne de novembre",
        actor: "Assistant rédaction",
        agent: true,
        detail: "Le premier jet est dans les commentaires.",
        time: "20 min",
        body: "Le premier jet est prêt. Choisissez l'un des deux titres et je le termine.",
      },
      {
        id: "failed",
        issue: "OPS-42",
        title: "Mettre à jour la FAQ du service client",
        actor: "Assistant ops",
        agent: true,
        detail: "Exécution en échec",
        time: "1 h",
        body: "L'exécution d'Assistant ops a échoué avant la fin.",
      },
      {
        id: "assigned",
        issue: "OPS-43",
        title: "Préparer la revue trimestrielle des fournisseurs",
        actor: "Lei Li",
        agent: false,
        detail: "Assigné à vous",
        time: "3 h",
        body: "",
      },
    ],
    selectPrompt: "Ouvrez une notification depuis la boîte de réception",
    markRead: "Marquer comme lu",
    markUnread: "Marquer comme non lu",
    markDone: "Marquer comme terminé",
    archive: "Archiver",
    archivedToast: "Notification archivée",
    runFailed: "Exécution en échec",
    menu: [
      "Voir les archivées",
      "Tout marquer comme lu",
      "Archiver tout ce qui est lu",
      "Archiver les éléments terminés",
      "Tout archiver",
    ],
    menuLabel: "Actions de la boîte de réception",
    archivedTitle: "Archivées",
    unarchive: "Désarchiver",
    captions: {
      idle: "Les notifications qui vous concernent sont en haut du panneau Travail.",
      start: "Trois notifications non lues. Ouvrez celle où quelqu'un vous a mentionné.",
      opened:
        "La tâche s'est ouverte au commentaire. La notification est devenue lue et reste dans la liste. Une fois qu'elle est traitée, cliquez sur Archiver (E).",
      archived:
        "La ligne a quitté la boîte de réception et la notification suivante s'est ouverte : le premier jet est arrivé. Cliquez sur Marquer comme terminé pour clore la tâche.",
      done: "Vous avez marqué la tâche comme terminée : sa notification a été archivée et la suivante s'est ouverte. Les notifications archivées sont dans le menu ⋯ de la section. Cliquez sur ⋯.",
      menu: "Le menu ⋯ regroupe Voir les archivées et les actions groupées. Cliquez sur Voir les archivées.",
      archivedView:
        "Les notifications archivées gardent leur état de lecture et peuvent être désarchivées ici. Utilisez Rejouer pour revoir la démo.",
    },
  },
};

type Step = Exclude<Phase, "idle">;

// What each step has archived, read, and open.
const ARCHIVED: Record<Step, NoticeId[]> = {
  start: [],
  opened: [],
  archived: ["mention"],
  done: ["mention", "draft"],
  menu: ["mention", "draft"],
  archivedView: ["mention", "draft"],
};
const READ: Record<Step, NoticeId[]> = {
  start: ["assigned"],
  opened: ["assigned", "mention"],
  archived: ["assigned", "mention", "draft"],
  done: ["assigned", "mention", "draft", "failed"],
  menu: ["assigned", "mention", "draft", "failed"],
  archivedView: ["assigned", "mention", "draft", "failed"],
};
const OPEN: Record<Step, NoticeId | null> = {
  start: null,
  opened: "mention",
  archived: "draft",
  done: "failed",
  menu: "failed",
  archivedView: null,
};

type Status = "todo" | "in_progress" | "in_review" | "done";

function statusOf(id: NoticeId, step: Step): Status {
  if (id === "draft") return ARCHIVED[step].includes("draft") ? "done" : "in_review";
  return id === "failed" ? "in_progress" : "todo";
}

/** Leads a row the way the issue's status icon does in the product. */
function DemoStatus({ status }: { status: Status }) {
  const Icon = { todo: Circle, in_progress: CircleDashed, in_review: CircleDot, done: CircleCheck }[status];
  const color = {
    todo: "text-muted-foreground",
    in_progress: "text-warning",
    in_review: "text-success",
    done: "text-info",
  }[status];
  return <Icon aria-hidden className={cn("size-3.5 shrink-0", color)} />;
}

/** The actor's avatar on a row's second line, smaller than DemoAvatar. */
function RowAvatar({ name, agent }: { name: string; agent: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex size-4 shrink-0 items-center justify-center rounded-full text-[0.625rem] font-medium",
        agent ? "bg-brand/12 text-brand" : "bg-muted text-foreground",
      )}
    >
      {agent ? <Bot className="size-2.5" /> : name.slice(0, 1)}
    </span>
  );
}

function HeaderAction({
  label,
  coach,
  onClick,
  children,
}: {
  label: string;
  coach: boolean;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <CoachMark active={coach} side="bottom" align="end" ringClassName="rounded-md">
      <Button
        type="button"
        variant="ghost"
        size="icon-xs"
        aria-label={label}
        title={label}
        disabled={!coach}
        onClick={onClick}
        className="text-muted-foreground disabled:opacity-100"
      >
        {children}
      </Button>
    </CoachMark>
  );
}

export function InboxDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Step>("start");

  const shownPhase: Phase = visible ? phase : "idle";
  const rows = t.notices.filter((n) => !ARCHIVED[phase].includes(n.id));
  const isRead = (id: NoticeId) => READ[phase].includes(id);
  const unread = rows.filter((n) => !isRead(n.id)).length;
  const selected = t.notices.find((n) => n.id === OPEN[phase]) ?? null;

  const panel = (
    <div className="flex min-w-0 flex-col gap-0.5 bg-muted/30 p-1.5 sm:w-[46%] sm:shrink-0 sm:border-r">
      <div className="flex h-7 items-center gap-1 px-2">
        <span className="min-w-0 flex-1 truncate text-caption font-medium text-muted-foreground">{t.inbox}</span>
        <span aria-hidden className="flex items-center gap-1.5 text-faint-foreground">
          <Filter className="size-3.5" />
          <SlidersHorizontal className="size-3.5" />
        </span>
        <span className="relative">
          <CoachMark active={visible && phase === "done"} quiet ringClassName="rounded-md">
            <button
              type="button"
              aria-label={t.menuLabel}
              aria-expanded={phase === "menu"}
              disabled={phase !== "done"}
              onClick={() => setPhase("menu")}
              className={cn(
                "flex size-5 items-center justify-center rounded-md text-faint-foreground",
                phase === "menu" && "bg-accent text-foreground",
              )}
            >
              <MoreHorizontal className="size-3.5" />
            </button>
          </CoachMark>
          {phase === "menu" && (
            <span className="absolute top-full right-0 z-30 sm:right-auto sm:left-0 mt-1 flex w-max min-w-44 flex-col rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in">
              {t.menu.map((label, index) =>
                index === 0 ? (
                  <CoachMark key={label} active quiet ringClassName="rounded-md" className="flex w-full">
                    <button
                      type="button"
                      onClick={() => setPhase("archivedView")}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-caption hover:bg-accent"
                    >
                      <Archive aria-hidden className="size-3.5 text-muted-foreground" />
                      {label}
                    </button>
                  </CoachMark>
                ) : (
                  <span key={label} className="px-2 py-1.5 text-caption text-muted-foreground">
                    {label}
                  </span>
                ),
              )}
            </span>
          )}
        </span>
        {unread > 0 && <span className="w-4 text-right text-caption font-medium text-brand tabular-nums">{unread}</span>}
      </div>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-0">
        {rows.map((notice) => {
          const rowUnread = !isRead(notice.id);
          const active = notice.id === selected?.id;
          const clickable = visible && phase === "start" && notice.id === "mention";
          return (
            <li key={notice.id} className="m-0 p-0">
              <CoachMark active={clickable} side="right" ringClassName="rounded-md" className="flex w-full">
                <button
                  type="button"
                  disabled={!clickable}
                  aria-current={active ? "page" : undefined}
                  onClick={() => setPhase("opened")}
                  className={cn(
                    "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left motion-safe:animate-in motion-safe:fade-in",
                    active ? "bg-accent" : "enabled:hover:bg-accent/50",
                  )}
                >
                  <span className="mt-[3px] flex shrink-0">
                    <DemoStatus status={statusOf(notice.id, phase)} />
                  </span>
                  <span className="flex min-w-0 flex-1 flex-col">
                    <span className="flex min-w-0 items-center gap-2 leading-5">
                      <span
                        className={cn(
                          "min-w-0 flex-1 truncate text-body",
                          rowUnread ? "font-medium text-foreground" : "text-muted-foreground",
                        )}
                      >
                        {notice.title}
                      </span>
                      <span className="shrink-0 text-caption text-faint-foreground tabular-nums">{notice.time}</span>
                    </span>
                    <span className="mt-0.5 flex min-w-0 items-center gap-2 text-caption leading-4 text-muted-foreground">
                      <span className="flex min-w-0 flex-1 items-center gap-1.5">
                        <RowAvatar name={notice.actor} agent={notice.agent} />
                        <span className="min-w-0 flex-1 truncate">
                          <span className="font-medium">{notice.actor}</span> {notice.detail}
                        </span>
                      </span>
                      {rowUnread && <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-brand" />}
                    </span>
                  </span>
                </button>
              </CoachMark>
            </li>
          );
        })}
      </ul>
      <div aria-hidden className="mt-1 flex flex-col gap-0.5">
        {t.sections.map((label) => (
          <span key={label} className="flex h-7 items-center gap-1 px-2 text-caption font-medium text-faint-foreground">
            <ChevronRight className="size-3" />
            {label}
          </span>
        ))}
      </div>
    </div>
  );

  const detail = selected ? (
    <div key={selected.id} className="flex min-w-0 flex-1 flex-col motion-safe:animate-in motion-safe:fade-in">
      <div className="flex h-10 items-center gap-2 border-b px-3">
        <span className="shrink-0 text-caption text-muted-foreground">{selected.issue}</span>
        <span className="min-w-0 flex-1 truncate text-label font-medium">{selected.title}</span>
        <span className="flex shrink-0 items-center gap-0.5">
          <HeaderAction label={isRead(selected.id) ? t.markUnread : t.markRead} coach={false}>
            {isRead(selected.id) ? <CircleDot /> : <Check />}
          </HeaderAction>
          <HeaderAction label={t.markDone} coach={phase === "archived"} onClick={() => setPhase("done")}>
            <CircleCheck />
          </HeaderAction>
          <HeaderAction label={`${t.archive} (E)`} coach={phase === "opened"} onClick={() => setPhase("archived")}>
            <Archive />
          </HeaderAction>
        </span>
      </div>
      <div className="flex flex-col gap-2 p-3">
        {selected.id === "failed" ? (
          <>
            <div className="flex items-center gap-2 text-body">
              <DemoAvatar name={selected.actor} agent />
              <span className="font-medium">{selected.actor}</span>
              <span className="inline-flex items-center gap-1 text-caption text-destructive">
                <X aria-hidden className="size-3" />
                {t.runFailed}
              </span>
            </div>
            <p className="pl-7 text-caption text-muted-foreground">{selected.body}</p>
          </>
        ) : (
          <DemoComment author={selected.actor} agent={selected.agent}>
            {selected.mention && <span className="font-medium text-brand">{selected.mention} </span>}
            {selected.body}
          </DemoComment>
        )}
      </div>
    </div>
  ) : phase === "archivedView" ? (
    <div className="flex min-w-0 flex-1 flex-col motion-safe:animate-in motion-safe:fade-in">
      <div className="flex h-10 items-center gap-2 border-b px-3">
        <Archive aria-hidden className="size-3.5 text-muted-foreground" />
        <span className="text-label font-medium">{t.archivedTitle}</span>
      </div>
      <ul className="m-0 flex list-none flex-col gap-0.5 p-1.5">
        {t.notices
          .filter((n) => ARCHIVED[phase].includes(n.id))
          .map((notice) => (
            <li key={notice.id} className="m-0 flex items-start gap-2 rounded-md px-2 py-1.5">
              <span className="mt-[3px] flex shrink-0">
                <DemoStatus status={statusOf(notice.id, phase)} />
              </span>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-body leading-5 text-muted-foreground">{notice.title}</span>
                <span className="mt-0.5 flex min-w-0 items-center gap-1.5 text-caption leading-4 text-muted-foreground">
                  <RowAvatar name={notice.actor} agent={notice.agent} />
                  <span className="min-w-0 flex-1 truncate">
                    <span className="font-medium">{notice.actor}</span> {notice.detail}
                  </span>
                </span>
              </span>
              <span className="inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-0.5 text-caption text-muted-foreground">
                <ArchiveRestore aria-hidden className="size-3.5" />
                {t.unarchive}
              </span>
            </li>
          ))}
      </ul>
    </div>
  ) : (
    <div className="flex min-h-32 flex-1 items-center justify-center p-4 text-caption text-muted-foreground">
      {t.selectPrompt}
    </div>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={() => setPhase("start")}>
      <div className="relative flex min-h-96 flex-col max-sm:divide-y sm:flex-row">
        {panel}
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
