"use client";

import { useState } from "react";
import { Check, ChevronsUpDown, Inbox, ListTodo, Pin, PinOff, UserRound, X } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Ws = "a" | "b";
type Status = "todo" | "in_progress" | "in_review";
type Issue = { id: string; title: string; status: Status };

const copy = {
  en: {
    title: "Desktop · Each workspace keeps its own tabs",
    workspaces: { a: "Ops team", b: "Marketing team" } as Record<Ws, string>,
    workspacesLabel: "Workspaces",
    nav: { inbox: "Inbox", myIssues: "My Issues", issues: "Issues" },
    issues: {
      a: [
        { id: "OPS-31", title: "Travel expense rules", status: "in_progress" },
        { id: "OPS-32", title: "New-hire handbook", status: "todo" },
        { id: "OPS-33", title: "Quarterly team outing", status: "todo" },
      ],
      b: [
        { id: "MKT-8", title: "Holiday campaign poster", status: "in_review" },
        { id: "MKT-9", title: "Customer story", status: "in_progress" },
      ],
    } as Record<Ws, Issue[]>,
    status: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<Status, string>,
    pin: "Pin tab",
    unpin: "Unpin tab",
    close: "Close tab",
    pinnedName: (title: string) => `${title} (pinned)`,
    captions: {
      idle: "The Ops team workspace already has three tabs open.",
      pin: "First pin the Issues list: click the pin on its tab.",
      open: "A pinned tab stays on its page. Click OPS-33, and it opens in a new tab.",
      switch: "Move to another workspace: click the workspace name at the top left.",
      pickB: "Choose Marketing team.",
      inB: "These are Marketing team's own tabs; none of Ops team's tabs came along. Now switch back to Ops team.",
      pickA: "Choose Ops team.",
      done: "Ops team's tabs are all still here, and OPS-33 is still the active tab.",
    },
  },
  zh: {
    title: "Desktop · 每个工作区保留自己的标签页",
    workspaces: { a: "运营团队", b: "市场团队" } as Record<Ws, string>,
    workspacesLabel: "工作区",
    nav: { inbox: "收件箱", myIssues: "我的任务", issues: "任务" },
    issues: {
      a: [
        { id: "OPS-31", title: "差旅报销规则", status: "in_progress" },
        { id: "OPS-32", title: "新人手册", status: "todo" },
        { id: "OPS-33", title: "季度团建", status: "todo" },
      ],
      b: [
        { id: "MKT-8", title: "节日活动海报", status: "in_review" },
        { id: "MKT-9", title: "客户案例", status: "in_progress" },
      ],
    } as Record<Ws, Issue[]>,
    status: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<Status, string>,
    pin: "固定标签页",
    unpin: "取消固定标签页",
    close: "关闭标签页",
    pinnedName: (title: string) => `${title}（已固定）`,
    captions: {
      idle: "运营团队工作区里已经开着三个标签页。",
      pin: `先把"任务"列表固定住：点它标签上的图钉。`,
      open: "固定的标签页会停在原页面。点 OPS-33，它会在新标签页里打开。",
      switch: "切到另一个工作区：点左上角的工作区名称。",
      pickB: `选择"市场团队"。`,
      inB: "这里是市场团队自己的标签页，运营团队的标签一个也没带过来。再切回运营团队看看。",
      pickA: `选择"运营团队"。`,
      done: "运营团队的标签页都还在，OPS-33 仍是当前标签。",
    },
  },
  ja: {
    title: "Desktop · ワークスペースごとにタブを保持",
    workspaces: { a: "運用チーム", b: "マーケティングチーム" } as Record<Ws, string>,
    workspacesLabel: "ワークスペース",
    nav: { inbox: "インボックス", myIssues: "マイタスク", issues: "タスク" },
    issues: {
      a: [
        { id: "OPS-31", title: "出張旅費の精算ルール", status: "in_progress" },
        { id: "OPS-32", title: "新入社員ハンドブック", status: "todo" },
        { id: "OPS-33", title: "四半期のチームイベント", status: "todo" },
      ],
      b: [
        { id: "MKT-8", title: "季節キャンペーンのポスター", status: "in_review" },
        { id: "MKT-9", title: "導入事例", status: "in_progress" },
      ],
    } as Record<Ws, Issue[]>,
    status: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<Status, string>,
    pin: "タブをピン留め",
    unpin: "タブのピン留めを解除",
    close: "タブを閉じる",
    pinnedName: (title: string) => `${title}（ピン留め）`,
    captions: {
      idle: "運用チームのワークスペースでは、すでに 3 つのタブが開いています。",
      pin: "まず「タスク」一覧をピン留めします。タブのピンをクリックします。",
      open: "ピン留めしたタブは元のページにとどまります。OPS-33 をクリックすると、新しいタブで開きます。",
      switch: "別のワークスペースに移ります。左上のワークスペース名をクリックします。",
      pickB: "「マーケティングチーム」を選びます。",
      inB: "ここはマーケティングチーム自身のタブです。運用チームのタブは 1 つも入っていません。運用チームに戻ってみます。",
      pickA: "「運用チーム」を選びます。",
      done: "運用チームのタブはすべて残っていて、OPS-33 もアクティブなままです。",
    },
  },
  ko: {
    title: "Desktop · 워크스페이스마다 탭을 따로 보관",
    workspaces: { a: "운영팀", b: "마케팅팀" } as Record<Ws, string>,
    workspacesLabel: "워크스페이스",
    nav: { inbox: "인박스", myIssues: "내 태스크", issues: "태스크" },
    issues: {
      a: [
        { id: "OPS-31", title: "출장비 정산 규칙", status: "in_progress" },
        { id: "OPS-32", title: "신입 사원 안내서", status: "todo" },
        { id: "OPS-33", title: "분기 팀 행사", status: "todo" },
      ],
      b: [
        { id: "MKT-8", title: "시즌 캠페인 포스터", status: "in_review" },
        { id: "MKT-9", title: "고객 사례", status: "in_progress" },
      ],
    } as Record<Ws, Issue[]>,
    status: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<Status, string>,
    pin: "탭 고정",
    unpin: "탭 고정 해제",
    close: "탭 닫기",
    pinnedName: (title: string) => `${title} (고정됨)`,
    captions: {
      idle: "운영팀 워크스페이스에는 이미 탭 세 개가 열려 있습니다.",
      pin: "먼저 '태스크' 목록을 고정합니다. 탭의 핀을 클릭하세요.",
      open: "고정한 탭은 원래 페이지에 머뭅니다. OPS-33을 클릭하면 새 탭에서 열립니다.",
      switch: "다른 워크스페이스로 이동합니다. 왼쪽 위의 워크스페이스 이름을 클릭하세요.",
      pickB: "'마케팅팀'을 선택하세요.",
      inB: "여기는 마케팅팀의 탭입니다. 운영팀의 탭은 하나도 따라오지 않았습니다. 다시 운영팀으로 돌아가 보세요.",
      pickA: "'운영팀'을 선택하세요.",
      done: "운영팀의 탭이 모두 남아 있고 OPS-33도 여전히 현재 탭입니다.",
    },
  },
  fr: {
    title: "Desktop · Chaque espace de travail garde ses onglets",
    workspaces: { a: "Équipe ops", b: "Équipe marketing" } as Record<Ws, string>,
    workspacesLabel: "Espaces de travail",
    nav: { inbox: "Boîte de réception", myIssues: "Mes tâches", issues: "Tâches" },
    issues: {
      a: [
        { id: "OPS-31", title: "Règles des notes de frais", status: "in_progress" },
        { id: "OPS-32", title: "Guide des nouveaux arrivants", status: "todo" },
        { id: "OPS-33", title: "Sortie d'équipe trimestrielle", status: "todo" },
      ],
      b: [
        { id: "MKT-8", title: "Affiche de la campagne saisonnière", status: "in_review" },
        { id: "MKT-9", title: "Témoignage client", status: "in_progress" },
      ],
    } as Record<Ws, Issue[]>,
    status: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<Status, string>,
    pin: "Épingler l'onglet",
    unpin: "Désépingler l'onglet",
    close: "Fermer l'onglet",
    pinnedName: (title: string) => `${title} (épinglé)`,
    captions: {
      idle: "L'espace de travail Équipe ops a déjà trois onglets ouverts.",
      pin: "Épinglez d'abord la liste des tâches : cliquez sur l'épingle de son onglet.",
      open: "Un onglet épinglé reste sur sa page. Cliquez sur OPS-33 : la tâche s'ouvre dans un nouvel onglet.",
      switch: "Passez à un autre espace de travail : cliquez sur son nom en haut à gauche.",
      pickB: "Choisissez Équipe marketing.",
      inB: "Voici les onglets propres à Équipe marketing ; aucun onglet d'Équipe ops ne l'a suivie. Revenez maintenant à Équipe ops.",
      pickA: "Choisissez Équipe ops.",
      done: "Les onglets d'Équipe ops sont tous là, et OPS-33 est toujours l'onglet actif.",
    },
  },
};

type Phase = "idle" | "pin" | "open" | "switch" | "pickB" | "inB" | "pickA" | "done";

// A tab is the workspace's issue list (issue === null) or one issue.
type TabState = { key: string; issue: number | null; pinned: boolean };
type Group = { tabs: TabState[]; active: string };

const INITIAL: Record<Ws, Group> = {
  a: {
    tabs: [
      { key: "list", issue: null, pinned: false },
      { key: "0", issue: 0, pinned: false },
      { key: "1", issue: 1, pinned: false },
    ],
    active: "list",
  },
  b: {
    tabs: [
      { key: "list", issue: null, pinned: false },
      { key: "0", issue: 0, pinned: false },
      { key: "1", issue: 1, pinned: false },
    ],
    active: "0",
  },
};

const statusDot: Record<Status, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

export function DesktopTabsDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("pin");
  const [ws, setWs] = useState<Ws>("a");
  const [groups, setGroups] = useState(INITIAL);

  const shownPhase: Phase = visible ? phase : "idle";
  const group = groups[ws];
  const activeTab = group.tabs.find((tab) => tab.key === group.active)!;
  const menuOpen = phase === "pickB" || phase === "pickA";
  const issuesOf = t.issues[ws];

  const tabTitle = (tab: TabState) => (tab.issue === null ? t.nav.issues : issuesOf[tab.issue]!.title);

  // Pinning keeps the list where it is: it is already the first tab, which is
  // the end of the (empty) pinned zone.
  const pinList = () => {
    setGroups((g) => ({
      ...g,
      a: { ...g.a, tabs: g.a.tabs.map((tab) => (tab.key === "list" ? { ...tab, pinned: true } : tab)) },
    }));
    setPhase("open");
  };

  // A push from a pinned tab opens a new, active tab right of the opener
  // (never inside the pinned zone), so the list stays parked.
  const openFromPinned = (issue: number) => {
    setGroups((g) => {
      const tabs = g.a.tabs;
      const activeIndex = tabs.findIndex((tab) => tab.key === g.a.active);
      const boundary = tabs.filter((tab) => tab.pinned).length;
      const at = Math.max(activeIndex + 1, boundary);
      const tab: TabState = { key: String(issue), issue, pinned: false };
      return { ...g, a: { tabs: [...tabs.slice(0, at), tab, ...tabs.slice(at)], active: tab.key } };
    });
    setPhase("switch");
  };

  const pickWorkspace = (target: Ws) => {
    setWs(target);
    setPhase(target === "b" ? "inB" : "done");
  };

  const replay = () => {
    setPhase("pin");
    setWs("a");
    setGroups(INITIAL);
  };

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="grid min-h-80 grid-cols-1 grid-rows-[auto_1fr] sm:grid-cols-[10rem_minmax(0,1fr)] sm:grid-rows-1">
        {/* Sidebar: the workspace switcher sits at its top, as in Desktop. */}
        <div className="relative border-b bg-muted/30 p-2 sm:border-r sm:border-b-0">
          <CoachMark
            active={shownPhase === "switch" || shownPhase === "inB"}
            // A label below would cover the sidebar's first entry; the caption
            // already names the workspace switcher.
            quiet
            ringClassName="rounded-md"
            className="flex w-full"
          >
            <button
              type="button"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              disabled={!visible || (phase !== "switch" && phase !== "inB")}
              onClick={() => setPhase(phase === "switch" ? "pickB" : "pickA")}
              className="flex h-8 w-full min-w-0 items-center gap-2 rounded-md px-1.5 text-left text-label font-medium hover:bg-muted disabled:hover:bg-transparent"
            >
              <WorkspaceAvatar name={t.workspaces[ws]} />
              <span className="min-w-0 flex-1 truncate">{t.workspaces[ws]}</span>
              <ChevronsUpDown aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
            </button>
          </CoachMark>
          {menuOpen && (
            <div
              role="menu"
              aria-label={t.workspacesLabel}
              className="absolute top-11 left-2 z-30 w-52 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
            >
              <div className="px-2 pt-1 pb-0.5 text-micro text-muted-foreground">{t.workspacesLabel}</div>
              {(["a", "b"] as Ws[]).map((option) => {
                const target = phase === "pickB" ? "b" : "a";
                return (
                  <CoachMark
                    key={option}
                    active={option === target}
                    quiet
                    ringClassName="rounded-md"
                    className="flex w-full"
                  >
                    <button
                      type="button"
                      role="menuitemradio"
                      aria-checked={option === ws}
                      disabled={option !== target}
                      onClick={() => pickWorkspace(option)}
                      className="flex w-full min-w-0 items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:hover:bg-transparent"
                    >
                      <WorkspaceAvatar name={t.workspaces[option]} />
                      <span className="min-w-0 flex-1 truncate">{t.workspaces[option]}</span>
                      {option === ws && <Check aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />}
                    </button>
                  </CoachMark>
                );
              })}
            </div>
          )}
          <ul aria-hidden className="m-0 mt-10 hidden list-none flex-col gap-0.5 p-0 text-caption text-muted-foreground sm:flex">
            {[
              { Icon: Inbox, label: t.nav.inbox },
              { Icon: UserRound, label: t.nav.myIssues },
              { Icon: ListTodo, label: t.nav.issues },
            ].map(({ Icon, label }) => (
              <li key={label} className="m-0 flex items-center gap-2 rounded-md px-1.5 py-1">
                <Icon className="size-3.5 shrink-0" />
                <span className="truncate">{label}</span>
              </li>
            ))}
          </ul>
        </div>

        <div className="flex min-w-0 flex-col">
          <div role="tablist" aria-label={t.workspaces[ws]} className="flex min-w-0 gap-0.5 border-b bg-muted/30 px-1.5 pt-1.5">
            {group.tabs.map((tab) => {
              const isActive = tab.key === group.active;
              const title = tabTitle(tab);
              const issue = tab.issue === null ? null : issuesOf[tab.issue]!;
              const pinTarget = ws === "a" && tab.key === "list" && shownPhase === "pin";
              return (
                <div
                  key={`${ws}-${tab.key}`}
                  role="tab"
                  aria-selected={isActive}
                  aria-label={tab.pinned ? t.pinnedName(title) : title}
                  className={cn(
                    "flex h-8 min-w-0 max-w-40 flex-1 items-center gap-1.5 rounded-t-md border border-b-0 px-2 text-caption motion-safe:animate-in motion-safe:fade-in",
                    tab.pinned ? "shrink-0 flex-none" : "",
                    isActive
                      ? "-mb-px min-w-24 border-border bg-card font-medium text-foreground"
                      : "border-transparent text-muted-foreground",
                  )}
                >
                  {issue ? (
                    <span aria-hidden className={cn("size-2.5 shrink-0 rounded-full border-2", statusDot[issue.status])} />
                  ) : (
                    <ListTodo aria-hidden className="size-3.5 shrink-0" />
                  )}
                  <span className="min-w-0 flex-1 truncate">{title}</span>
                  {pinTarget ? (
                    <CoachMark active side="bottom" ringClassName="rounded-sm">
                      <button
                        type="button"
                        aria-label={t.pin}
                        onClick={pinList}
                        className="inline-flex size-4 items-center justify-center rounded-sm text-muted-foreground hover:bg-muted-foreground/20 hover:text-foreground"
                      >
                        <Pin className="size-3" />
                      </button>
                    </CoachMark>
                  ) : tab.pinned ? (
                    <PinOff aria-label={t.unpin} role="img" className="size-3 shrink-0 text-muted-foreground" />
                  ) : (
                    isActive && <X aria-label={t.close} role="img" className="size-3 shrink-0 text-muted-foreground" />
                  )}
                </div>
              );
            })}
          </div>

          <div key={`${ws}-${group.active}`} className="min-w-0 flex-1 px-4 pt-3 pb-4 motion-safe:animate-in motion-safe:fade-in">
            {activeTab.issue === null ? (
              <>
                <div className="flex h-8 items-center text-label font-medium">{t.nav.issues}</div>
                <ul className="m-0 mt-1 flex list-none flex-col p-0">
                  {issuesOf.map((issue, i) => {
                    const target = ws === "a" && i === 2 && shownPhase === "open";
                    return (
                      <li key={issue.id} className="m-0 flex min-h-9 items-center gap-2 border-b last:border-b-0">
                        <span
                          aria-hidden
                          className={cn("size-3 shrink-0 rounded-full border-2", statusDot[issue.status])}
                        />
                        <span className="w-14 shrink-0 text-caption text-muted-foreground">{issue.id}</span>
                        <CoachMark active={target} side="right" ringClassName="rounded-sm" className="min-w-0">
                          <button
                            type="button"
                            disabled={!target}
                            onClick={() => openFromPinned(i)}
                            className="max-w-full truncate rounded-sm text-left text-body hover:underline disabled:hover:no-underline"
                          >
                            {issue.title}
                          </button>
                        </CoachMark>
                      </li>
                    );
                  })}
                </ul>
              </>
            ) : (
              <IssueDetail issue={issuesOf[activeTab.issue]!} statusLabel={t.status[issuesOf[activeTab.issue]!.status]} />
            )}
          </div>
        </div>
      </div>
    </DemoFrame>
  );
}

function WorkspaceAvatar({ name }: { name: string }) {
  return (
    <span
      aria-hidden
      className="inline-flex size-5 shrink-0 items-center justify-center rounded-sm bg-brand/12 text-micro font-medium text-brand"
    >
      {name.slice(0, 1)}
    </span>
  );
}

function IssueDetail({ issue, statusLabel }: { issue: Issue; statusLabel: string }) {
  return (
    <div className="min-w-0">
      <div className="flex h-8 items-center text-caption text-muted-foreground">{issue.id}</div>
      <div className="text-title-sm font-medium">{issue.title}</div>
      <div className="mt-2 flex items-center gap-1.5 text-caption text-muted-foreground">
        <span aria-hidden className={cn("size-3 rounded-full border-2", statusDot[issue.status])} />
        {statusLabel}
      </div>
      <div aria-hidden className="mt-4 flex flex-col gap-2">
        <span className="h-2 w-11/12 rounded-full bg-muted" />
        <span className="h-2 w-4/5 rounded-full bg-muted" />
        <span className="h-2 w-2/3 rounded-full bg-muted" />
      </div>
    </div>
  );
}
