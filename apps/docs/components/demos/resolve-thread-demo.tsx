"use client";

import { type ReactNode, useState } from "react";
import { CheckCircle2, ChevronRight, Copy, ListChevronsDownUp, MoreHorizontal } from "lucide-react";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Entry = { author: string; agent: boolean; body: string };

const copy = {
  en: {
    title: "OPS-21 · Pick the venue for the annual party",
    root: {
      author: "Lei Li",
      agent: false,
      body: "Which venue should we book? The options are the Riverside Expo Center and the Westside Hotel.",
    } as Entry,
    replies: [
      {
        author: "Ops helper",
        agent: true,
        body: "I compared both: Riverside seats 300. Westside is 20% cheaper but seats at most 200.",
      },
      { author: "Lei Li", agent: false, body: "260 people have signed up already, so Westside is too small." },
      {
        author: "Ops helper",
        agent: true,
        body: "Then book the Riverside Expo Center: it fits all 260 and costs 12,000 more than Westside.",
      },
    ] as Entry[],
    moreActions: "Comment actions",
    resolveThread: "Resolve thread",
    resolveWithComment: "Resolve thread with comment",
    copyAction: "Copy",
    resolution: "Resolution",
    collapse: "Collapse",
    // Mirrors comment.resolve.bar / fold, with the author list the product builds.
    bar: (count: number, authors: string) => `${count} resolved comments from ${authors}`,
    fold: (count: number, authors: string) => `${count} comments from ${authors}`,
    captions: {
      idle: "A thread that has reached a decision.",
      start:
        "The thread has its answer. Open the menu on the first comment to resolve the whole thread, or open the last reply's menu to mark it as the resolution.",
      rootMenu: "Choose Resolve thread.",
      replyMenu: "Choose Resolve thread with comment: this reply becomes the thread's resolution.",
      rootResolved: "The whole thread folded into one line. Click it to read the thread again.",
      rootExpanded:
        "Nothing was lost. Collapse folds it again, and replying in the thread reopens it. Use Replay to try the other way.",
      replyResolved:
        "The first comment and the resolution stay visible, and the replies in between fold away. A thread keeps only one resolution at a time.",
      replyExpanded:
        "The resolution keeps its place in the thread, marked Resolution. Use Replay to try the other way.",
    },
  },
  zh: {
    title: "OPS-21 · 确定年会场地",
    root: {
      author: "李雷",
      agent: false,
      body: "年会场地定哪家？备选是滨江会展中心和城西酒店。",
    } as Entry,
    replies: [
      {
        author: "运营助手",
        agent: true,
        body: "两家都问过了：滨江能坐 300 人；城西报价低两成，但最多坐 200 人。",
      },
      { author: "李雷", agent: false, body: "报名已经 260 人了，城西坐不下。" },
      {
        author: "运营助手",
        agent: true,
        body: "那就定滨江会展中心：坐得下 260 人，比城西多花 1.2 万元。",
      },
    ] as Entry[],
    moreActions: "评论操作",
    resolveThread: "解决该讨论",
    resolveWithComment: "以此评论解决讨论",
    copyAction: "复制",
    resolution: "结论",
    collapse: "收起",
    bar: (count: number, authors: string) => `来自 ${authors} 的 ${count} 条已解决评论`,
    fold: (count: number, authors: string) => `来自 ${authors} 的 ${count} 条评论`,
    captions: {
      idle: "一条已经有结论的讨论。",
      start:
        "讨论有了答案。打开第一条评论的菜单可以解决整条讨论；打开最后一条回复的菜单，可以把它设为结论。",
      rootMenu: "选择\"解决该讨论\"。",
      replyMenu: "选择\"以此评论解决讨论\"，这条回复会成为讨论的结论。",
      rootResolved: "整条讨论折叠成了一行。点它可以重新展开。",
      rootExpanded: "内容都还在。点\"收起\"再折叠；在讨论里继续回复会自动重新打开它。点右上角可以换另一种方式再试。",
      replyResolved: "第一条评论和结论留在原处，中间的回复折叠起来。一条讨论同时只保留一个结论。",
      replyExpanded: "结论留在原来的位置，带着\"结论\"标记。点右上角可以换另一种方式再试。",
    },
  },
  ja: {
    title: "OPS-21 · 年次パーティーの会場を決める",
    root: {
      author: "李雷",
      agent: false,
      body: "パーティー会場はどちらにしますか？候補はリバーサイド展示センターとウエストサイドホテルです。",
    } as Entry,
    replies: [
      {
        author: "運用アシスタント",
        agent: true,
        body: "両方に問い合わせました。リバーサイドは 300 人まで入れます。ウエストサイドは 2 割安いものの、最大 200 人です。",
      },
      { author: "李雷", agent: false, body: "申し込みがもう 260 人なので、ウエストサイドでは入りきりません。" },
      {
        author: "運用アシスタント",
        agent: true,
        body: "それならリバーサイド展示センターにしましょう。260 人が入り、費用はウエストサイドより 12,000 多くかかります。",
      },
    ] as Entry[],
    moreActions: "コメントの操作",
    resolveThread: "スレッドを解決",
    resolveWithComment: "このコメントでスレッドを解決",
    copyAction: "コピー",
    resolution: "解決",
    collapse: "折りたたむ",
    bar: (count: number, authors: string) => `${authors} による解決済みコメント ${count} 件`,
    fold: (count: number, authors: string) => `${authors} のコメント ${count} 件`,
    captions: {
      idle: "結論が出たスレッドです。",
      start:
        "スレッドに答えが出ました。最初のコメントのメニューからはスレッド全体を解決できます。最後の返信のメニューからは、その返信をスレッドの解決にできます。",
      rootMenu: "「スレッドを解決」を選びます。",
      replyMenu: "「このコメントでスレッドを解決」を選びます。この返信がスレッドの解決になります。",
      rootResolved: "スレッド全体が 1 行に折りたたまれました。クリックすると読み返せます。",
      rootExpanded:
        "内容はそのまま残っています。「折りたたむ」でまた畳め、スレッドに返信すると自動で開き直されます。「もう一度」で別の方法も試せます。",
      replyResolved:
        "最初のコメントと解決はそのまま表示され、間の返信が折りたたまれます。1 つのスレッドに残る解決は常に 1 つだけです。",
      replyExpanded: "解決はスレッド内の元の位置に「解決」の印付きで残ります。「もう一度」で別の方法も試せます。",
    },
  },
  ko: {
    title: "OPS-21 · 연례 파티 장소 정하기",
    root: {
      author: "리레이",
      agent: false,
      body: "파티 장소는 어디로 할까요? 후보는 리버사이드 전시센터와 웨스트사이드 호텔입니다.",
    } as Entry,
    replies: [
      {
        author: "운영 도우미",
        agent: true,
        body: "두 곳 다 알아봤습니다. 리버사이드는 300명까지 앉을 수 있고, 웨스트사이드는 20% 저렴하지만 최대 200명입니다.",
      },
      { author: "리레이", agent: false, body: "벌써 260명이 신청해서 웨스트사이드는 자리가 모자라요." },
      {
        author: "운영 도우미",
        agent: true,
        body: "그럼 리버사이드 전시센터로 하죠. 260명이 모두 앉을 수 있고, 비용은 웨스트사이드보다 12,000 더 듭니다.",
      },
    ] as Entry[],
    moreActions: "댓글 작업",
    resolveThread: "스레드 해결",
    resolveWithComment: "이 댓글로 스레드 해결",
    copyAction: "복사",
    resolution: "해결",
    collapse: "접기",
    bar: (count: number, authors: string) => `${authors}님의 해결된 댓글 ${count}개`,
    fold: (count: number, authors: string) => `${authors}님의 댓글 ${count}개`,
    captions: {
      idle: "결론이 난 스레드입니다.",
      start:
        "스레드에 답이 나왔습니다. 첫 댓글의 메뉴에서는 스레드 전체를 해결할 수 있고, 마지막 답글의 메뉴에서는 그 답글을 스레드의 해결로 지정할 수 있습니다.",
      rootMenu: "'스레드 해결'을 선택하세요.",
      replyMenu: "'이 댓글로 스레드 해결'을 선택하세요. 이 답글이 스레드의 해결이 됩니다.",
      rootResolved: "스레드 전체가 한 줄로 접혔습니다. 클릭하면 다시 읽을 수 있습니다.",
      rootExpanded:
        "내용은 그대로 남아 있습니다. '접기'로 다시 접을 수 있고, 스레드에 답글을 달면 자동으로 다시 열립니다. '다시 보기'로 다른 방법도 해 보세요.",
      replyResolved: "첫 댓글과 해결은 그대로 보이고, 그 사이의 답글은 접힙니다. 한 스레드에는 해결이 하나만 남습니다.",
      replyExpanded: "해결은 스레드 안 원래 자리에 '해결' 표시와 함께 남습니다. '다시 보기'로 다른 방법도 해 보세요.",
    },
  },
  fr: {
    title: "OPS-21 · Choisir le lieu de la soirée annuelle",
    root: {
      author: "Lei Li",
      agent: false,
      body: "Quel lieu réserve-t-on ? Les options sont le Riverside Expo Center et l'hôtel Westside.",
    } as Entry,
    replies: [
      {
        author: "Assistant ops",
        agent: true,
        body: "J'ai comparé les deux : le Riverside accueille 300 personnes. Le Westside est 20 % moins cher, mais 200 places au maximum.",
      },
      { author: "Lei Li", agent: false, body: "260 personnes sont déjà inscrites, le Westside est trop petit." },
      {
        author: "Assistant ops",
        agent: true,
        body: "Alors réservons le Riverside Expo Center : il accueille les 260 personnes et coûte 12 000 de plus que le Westside.",
      },
    ] as Entry[],
    moreActions: "Actions du commentaire",
    resolveThread: "Résoudre le fil",
    resolveWithComment: "Résoudre le fil avec un commentaire",
    copyAction: "Copier",
    resolution: "Résolution",
    collapse: "Replier",
    bar: (count: number, authors: string) => `${count} commentaires résolus de ${authors}`,
    fold: (count: number, authors: string) => `${count} commentaires de ${authors}`,
    captions: {
      idle: "Un fil qui a abouti à une décision.",
      start:
        "Le fil a sa réponse. Ouvrez le menu du premier commentaire pour résoudre tout le fil, ou celui de la dernière réponse pour en faire la résolution.",
      rootMenu: "Choisissez « Résoudre le fil ».",
      replyMenu: "Choisissez « Résoudre le fil avec un commentaire » : cette réponse devient la résolution du fil.",
      rootResolved: "Tout le fil tient maintenant sur une ligne. Cliquez dessus pour le relire.",
      rootExpanded:
        "Rien n'est perdu. « Replier » le referme, et répondre dans le fil le rouvre. Utilisez Rejouer pour essayer l'autre méthode.",
      replyResolved:
        "Le premier commentaire et la résolution restent visibles, les réponses intermédiaires sont repliées. Un fil ne garde qu'une résolution à la fois.",
      replyExpanded:
        "La résolution garde sa place dans le fil, marquée « Résolution ». Utilisez Rejouer pour essayer l'autre méthode.",
    },
  },
};

type Phase =
  | "idle"
  | "start"
  | "rootMenu"
  | "replyMenu"
  | "rootResolved"
  | "rootExpanded"
  | "replyResolved"
  | "replyExpanded";

// Distinct authors in first-seen order, joined the way the product's resolved
// bars name them (two names fit, so no "and N others" here).
function authorsOf(entries: Entry[]): string {
  return [...new Set(entries.map((e) => e.author))].join(", ");
}

export function ResolveThreadDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");

  const shownPhase: Phase = visible ? phase : "idle";
  const replay = () => setPhase("start");

  const lastIndex = t.replies.length - 1;
  const resolution = t.replies[lastIndex]!;
  const folded = t.replies.slice(0, lastIndex);
  const replyMode = phase === "replyResolved" || phase === "replyExpanded";

  const menu = (items: { label: string; onClick?: () => void }[]) => (
    <div
      role="menu"
      aria-label={t.moreActions}
      className="absolute top-full right-0 z-10 mt-1 w-52 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
    >
      {items.map((item) => {
        const Icon = item.onClick ? CheckCircle2 : Copy;
        const row = (
          <button
            type="button"
            role="menuitem"
            disabled={!item.onClick}
            onClick={item.onClick}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:opacity-50 disabled:hover:bg-transparent"
          >
            <Icon aria-hidden className="size-3.5 shrink-0" />
            {item.label}
          </button>
        );
        return item.onClick ? (
          <CoachMark quiet
            key={item.label}
            active
            ringClassName="rounded-md"
            className="flex w-full"
          >
            {row}
          </CoachMark>
        ) : (
          <div key={item.label}>{row}</div>
        );
      })}
    </div>
  );

  const menuButton = (open: boolean, onClick: () => void, coach: boolean) => (
    <CoachMark active={coach} side="left" ringClassName="rounded-md">
      <button
        type="button"
        aria-label={t.moreActions}
        aria-haspopup="menu"
        aria-expanded={open}
        disabled={!visible}
        onClick={onClick}
        className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-muted"
      >
        <MoreHorizontal aria-hidden className="size-4" />
      </button>
    </CoachMark>
  );

  const rootActions =
    phase === "start" || phase === "rootMenu" ? (
      <div className="relative">
        {menuButton(phase === "rootMenu", () => setPhase(phase === "rootMenu" ? "start" : "rootMenu"), shownPhase === "start")}
        {phase === "rootMenu" &&
          menu([
            { label: t.resolveThread, onClick: () => setPhase("rootResolved") },
            { label: t.copyAction },
          ])}
      </div>
    ) : null;

  const resolutionActions =
    phase === "start" || phase === "replyMenu" ? (
      <div className="relative">
        {menuButton(phase === "replyMenu", () => setPhase(phase === "replyMenu" ? "start" : "replyMenu"), false)}
        {phase === "replyMenu" &&
          menu([
            { label: t.resolveWithComment, onClick: () => setPhase("replyResolved") },
            { label: t.copyAction },
          ])}
      </div>
    ) : null;

  const resolutionBadge = replyMode ? (
    <span className="text-caption font-medium text-success motion-safe:animate-in motion-safe:fade-in">
      {t.resolution}
    </span>
  ) : null;

  const thread = (
    <div className="overflow-hidden rounded-lg border bg-background motion-safe:animate-in motion-safe:fade-in">
      {(phase === "rootExpanded" || phase === "replyExpanded") && (
        <button
          type="button"
          onClick={() => setPhase(phase === "rootExpanded" ? "rootResolved" : "replyResolved")}
          className="flex w-full items-center gap-2.5 border-b border-border/50 bg-muted px-3 py-2 text-left text-body text-muted-foreground hover:bg-accent hover:text-accent-foreground"
        >
          <ListChevronsDownUp aria-hidden className="size-3.5" />
          {t.collapse}
        </button>
      )}
      <ThreadEntry entry={t.root} actions={rootActions} />
      <div className="flex flex-col gap-1 pb-2 pl-7">
        {phase === "replyResolved" ? (
          <>
            <CoachMark active side="bottom" align="end" className="flex w-full pr-3" ringClassName="rounded-md">
              <button
                type="button"
                onClick={() => setPhase("replyExpanded")}
                className="flex w-full items-center gap-2.5 rounded-md bg-muted/45 px-3 py-2 text-left text-body text-muted-foreground hover:bg-muted"
              >
                <ChevronRight aria-hidden className="size-3.5 shrink-0 rotate-90" />
                <span className="truncate">{t.fold(folded.length, authorsOf(folded))}</span>
              </button>
            </CoachMark>
            <ThreadEntry entry={resolution} badge={resolutionBadge} />
          </>
        ) : (
          t.replies.map((reply, i) => (
            <ThreadEntry
              key={reply.body}
              entry={reply}
              badge={i === lastIndex ? resolutionBadge : null}
              actions={i === lastIndex ? resolutionActions : null}
            />
          ))
        )}
      </div>
    </div>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-80 flex-col gap-3 p-4">
        {phase === "rootResolved" ? (
          <CoachMark active side="bottom" align="start" className="mt-1 flex w-full" ringClassName="rounded-lg">
            <button
              type="button"
              onClick={() => setPhase("rootExpanded")}
              className="flex w-full items-center justify-between gap-2 rounded-lg border bg-background px-3 py-3 text-left hover:bg-muted/50 motion-safe:animate-in motion-safe:fade-in"
            >
              <span className="flex min-w-0 items-center gap-2.5 text-body text-muted-foreground">
                <CheckCircle2 aria-hidden className="size-4 shrink-0" />
                <span className="truncate">{t.bar(1 + t.replies.length, authorsOf([t.root, ...t.replies]))}</span>
              </span>
              <ChevronRight aria-hidden className="size-3.5 shrink-0 rotate-90 text-muted-foreground" />
            </button>
          </CoachMark>
        ) : (
          thread
        )}
      </div>
    </DemoFrame>
  );
}

function ThreadEntry({ entry, badge, actions }: { entry: Entry; badge?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="min-w-0 px-3 py-2">
      <div className="flex min-h-6 items-center gap-2">
        <DemoAvatar name={entry.author} agent={entry.agent} />
        <span className="truncate text-label font-medium">{entry.author}</span>
        {badge}
        <span className="ml-auto" />
        {actions}
      </div>
      <div className="mt-0.5 pl-7 text-body break-words">{entry.body}</div>
    </div>
  );
}
