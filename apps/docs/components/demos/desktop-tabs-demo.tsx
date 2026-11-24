"use client";

import { useState } from "react";
import { Pin } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type Ws = "acme" | "personal";

const copy = {
  en: {
    kicker: "DESKTOP TABS",
    title: "Each workspace keeps its own tab strip",
    workspaces: {
      acme: {
        name: "Acme Engineering",
        tabs: [
          { label: "Issues", pinned: true },
          { label: "ACM-142 · Rate limiting", active: true },
          { label: "ACM-151 · Safari checkout" },
        ],
        summary: "Pinned tabs stay anchored to their page; opening an issue spawns a new tab instead of overwriting the list.",
      },
      personal: {
        name: "Personal Site",
        tabs: [
          { label: "Inbox", pinned: true },
          { label: "SITE-1 · Portfolio build", active: true },
        ],
        summary: "Switching workspaces swaps the entire tab strip cleanly. When you return to Acme Engineering, every tab and scroll position is still right where you left it.",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 switch tabs · ⌘T new tab · ⌘W close",
  },
  zh: {
    kicker: "桌面端标签页",
    title: "每个工作区独立保留自己的标签栏",
    workspaces: {
      acme: {
        name: "Acme 研发团队",
        tabs: [
          { label: "任务列表", pinned: true },
          { label: "ACM-142 · API 限流", active: true },
          { label: "ACM-151 · Safari 结算测试" },
        ],
        summary: "固定后的标签页始终停留在原视图；从中打开具体任务时会自动在新标签页展开，不会顶掉列表。",
      },
      personal: {
        name: "个人作品集",
        tabs: [
          { label: "收件箱", pinned: true },
          { label: "SITE-1 · 搭建摄影师网站", active: true },
        ],
        summary: "切换工作区时整个标签栏随之切换；切回 Acme 研发团队后，原有标签与各自滚动位置原封不动保留。",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 切换标签 · ⌘T 新建标签 · ⌘W 关闭",
  },
  "zh-Hant": {
    kicker: "桌面端分頁",
    title: "每個工作區獨立保留自己的分頁列",
    workspaces: {
      acme: {
        name: "Acme 研發團隊",
        tabs: [
          { label: "任務列表", pinned: true },
          { label: "ACM-142 · API 限流", active: true },
          { label: "ACM-151 · Safari 結帳測試" },
        ],
        summary: "固定後的分頁會停在原頁面；開啟任務時會在新分頁展開，不會覆蓋列表。",
      },
      personal: {
        name: "個人作品集",
        tabs: [
          { label: "收件箱", pinned: true },
          { label: "SITE-1 · 建立攝影師網站", active: true },
        ],
        summary: "切換工作區時分頁列會獨立切換；切回後原工作區的分頁與捲動位置完整保留。",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 切換分頁 · ⌘T 新分頁 · ⌘W 關閉",
  },
  ja: {
    kicker: "DESKTOP TABS",
    title: "ワークスペースごとに独立したタブを保持",
    workspaces: {
      acme: {
        name: "Acme Engineering",
        tabs: [
          { label: "タスク一覧", pinned: true },
          { label: "ACM-142 · レート制限", active: true },
          { label: "ACM-151 · Safari テスト" },
        ],
        summary: "ピン留めしたタブは元のページに留まり、タスクを開くと新しいタブで開かれます。",
      },
      personal: {
        name: "Personal Site",
        tabs: [
          { label: "インボックス", pinned: true },
          { label: "SITE-1 · ポートフォリオ制作", active: true },
        ],
        summary: "ワークスペースを切り替えるとタブ一覧も独立して切り替わり、戻ったとき元の状態がそのまま残ります。",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 タブ切替 · ⌘T 新規タブ · ⌘W 閉じる",
  },
  ko: {
    kicker: "DESKTOP TABS",
    title: "워크스페이스마다 독립된 탭 목록 유지",
    workspaces: {
      acme: {
        name: "Acme Engineering",
        tabs: [
          { label: "태스크 목록", pinned: true },
          { label: "ACM-142 · API 속도 제한", active: true },
          { label: "ACM-151 · Safari 테스트" },
        ],
        summary: "고정된 탭은 원래 페이지에 유지되며, 태스크를 클릭하면 새 탭에서 열립니다.",
      },
      personal: {
        name: "Personal Site",
        tabs: [
          { label: "인박스", pinned: true },
          { label: "SITE-1 · 포트폴리오 제작", active: true },
        ],
        summary: "워크스페이스를 전환하면 탭 목록도 따로 바뀌며, 돌아왔을 때 기존 탭과 스크롤 위치가 그대로 남습니다.",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 탭 전환 · ⌘T 새 탭 · ⌘W 닫기",
  },
  fr: {
    kicker: "ONGLETS DESKTOP",
    title: "Chaque espace conserve ses propres onglets",
    workspaces: {
      acme: {
        name: "Acme Engineering",
        tabs: [
          { label: "Tâches", pinned: true },
          { label: "ACM-142 · Rate limiting", active: true },
          { label: "ACM-151 · Test Safari" },
        ],
        summary: "Les onglets épinglés restent sur leur vue ; ouvrir une tâche crée un nouvel onglet.",
      },
      personal: {
        name: "Personal Site",
        tabs: [
          { label: "Boîte", pinned: true },
          { label: "SITE-1 · Portfolio", active: true },
        ],
        summary: "Changer d'espace bascule toute la barre d'onglets ; chaque espace retrouve ses onglets au retour.",
      },
    } as Record<Ws, { name: string; tabs: { label: string; pinned?: boolean; active?: boolean }[]; summary: string }>,
    shortcuts: "⌘1–9 onglets · ⌘T nouvel onglet · ⌘W fermer",
  },
};

export function DesktopTabsDemo() {
  const t = useDemoCopy(copy);
  const [ws, setWs] = useState<Ws>("acme");
  const cur = t.workspaces[ws];

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      actions={
        <div className="flex items-center gap-3" role="tablist" aria-label={t.title}>
          {(["acme", "personal"] as Ws[]).map((key) => (
            <button
              key={key}
              type="button"
              role="tab"
              aria-selected={ws === key}
              onClick={() => setWs(key)}
              className={cn(
                "border-b-[1.5px] pb-1 text-[0.75rem] transition-colors",
                ws === key
                  ? "border-[var(--primary)] font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.workspaces[key].name}
            </button>
          ))}
        </div>
      }
      caption={cur.summary}
    >
      <div className="flex flex-col gap-3">
        {/* Stylized Desktop tab bar */}
        <div className="flex flex-wrap items-center gap-1.5 rounded-[4px] border border-border bg-background p-1.5">
          <span className="mr-1 inline-flex items-center gap-1.5 rounded-[3px] bg-muted px-2 py-1 font-mono text-[0.6875rem] font-medium text-foreground">
            {cur.name}
          </span>
          {cur.tabs.map((tab) => (
            <span
              key={tab.label}
              className={cn(
                "inline-flex items-center gap-1.5 rounded-[3px] border px-2.5 py-1 text-[0.78125rem]",
                tab.active
                  ? "border-[var(--primary)]/50 bg-accent font-medium text-foreground"
                  : "border-border bg-card text-muted-foreground",
              )}
            >
              {tab.pinned && <Pin aria-hidden className="size-3 text-[var(--primary)]" />}
              {tab.label}
            </span>
          ))}
        </div>

        <div className="flex items-center justify-between text-[0.75rem] text-muted-foreground">
          <span className="font-mono">{t.shortcuts}</span>
        </div>
      </div>
    </EditorialFigure>
  );
}
