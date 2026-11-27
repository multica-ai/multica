"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type Mode = "offline" | "online";

const copy = {
  en: {
    kicker: "RUNTIME BINDING",
    title: "What happens when a runtime is offline",
    tabs: { offline: "Bound machine offline", online: "Bound machine reconnects" } as Record<Mode, string>,
    queueTitle: "MULTICA QUEUE",
    runTag: "Run #42 · Engineer",
    queuedState: "Waiting in queue",
    claimedState: "Claimed → Working",
    boundTitle: "Maya's MacBook Pro",
    boundRole: "Bound runtime · Claude Code",
    otherTitle: "Alex's Mac Studio",
    otherRole: "Other workspace machine · Online",
    neverMigrates: "Never migrates to another machine",
    drains: "Daemon reconnects → claims queued run",
    status: { offline: "Offline", online: "Online" },
    captions: {
      offline: "Runs stay pinned to the agent's bound runtime. Even when another teammate's machine is online, the run waits in the queue until the bound machine returns.",
      online: "As soon as Desktop opens or `multica daemon start` reconnects the bound machine, the queued run is claimed automatically.",
    },
  },
  zh: {
    kicker: "运行时绑定",
    title: "绑定的电脑离线时会发生什么",
    tabs: { offline: "绑定电脑离线", online: "绑定电脑重新上线" } as Record<Mode, string>,
    queueTitle: "MULTICA 运行队列",
    runTag: "Run #42 · Engineer",
    queuedState: "在队列中等待",
    claimedState: "已领取 → 正在工作",
    boundTitle: "Maya 的 MacBook Pro",
    boundRole: "绑定的运行时 · Claude Code",
    otherTitle: "队友的 Mac Studio",
    otherRole: "其他在线电脑 · 在线",
    neverMigrates: "不会转移到其他电脑执行",
    drains: "守护进程重连 → 自动领取排队运行",
    status: { offline: "离线", online: "在线" },
    captions: {
      offline: "运行严格绑定到智能体配置的运行时。即使团队其他电脑在线，运行也会留在队列中等待，不会转移过去。",
      online: "打开 Multica Desktop 或运行 multica daemon start 重新上线后，守护进程会自动领取队列中的运行并开始执行。",
    },
  },
  "zh-Hant": {
    kicker: "執行環境綁定",
    title: "綁定的電腦離線時會發生什麼",
    tabs: { offline: "綁定電腦離線", online: "綁定電腦重新上線" } as Record<Mode, string>,
    queueTitle: "MULTICA 執行佇列",
    runTag: "Run #42 · Engineer",
    queuedState: "在佇列中等待",
    claimedState: "已領取 → 正在工作",
    boundTitle: "Maya 的 MacBook Pro",
    boundRole: "綁定的執行環境 · Claude Code",
    otherTitle: "隊友的 Mac Studio",
    otherRole: "其他線上電腦 · 線上",
    neverMigrates: "不會轉移到其他電腦執行",
    drains: "守護行程重連 → 自動領取排隊執行",
    status: { offline: "離線", online: "線上" },
    captions: {
      offline: "執行嚴格綁定到智能體設定的執行環境。即使其他電腦在線上，執行也不會轉移過去。",
      online: "重新開啟 Multica Desktop 或執行 multica daemon start 上線後，佇列中的執行會自動被領取。",
    },
  },
  ja: {
    kicker: "RUNTIME BINDING",
    title: "ランタイムがオフラインのときの動作",
    tabs: { offline: "紐づく PC がオフライン", online: "再接続後" } as Record<Mode, string>,
    queueTitle: "MULTICA キュー",
    runTag: "Run #42 · Engineer",
    queuedState: "キューで待機中",
    claimedState: "取得済み → 作業中",
    boundTitle: "Maya の MacBook Pro",
    boundRole: "紐づくランタイム · Claude Code",
    otherTitle: "別メンバーの Mac Studio",
    otherRole: "他の PC · オンライン",
    neverMigrates: "他の PC へは移動しない",
    drains: "再接続 → 待機中の実行を自動取得",
    status: { offline: "オフライン", online: "オンライン" },
    captions: {
      offline: "実行は指定ランタイムに固定されます。別の PC がオンラインでも移動せずキューで待機します。",
      online: "Desktop を開くか multica daemon start で再接続すると、待機中の実行が自動的に開始されます。",
    },
  },
  ko: {
    kicker: "RUNTIME BINDING",
    title: "연결된 런타임이 오프라인일 때",
    tabs: { offline: "바인딩 PC 오프라인", online: "다시 온라인 전환" } as Record<Mode, string>,
    queueTitle: "MULTICA 대기열",
    runTag: "Run #42 · Engineer",
    queuedState: "대기열에서 대기 중",
    claimedState: "수신 완료 → 작업 중",
    boundTitle: "Maya의 MacBook Pro",
    boundRole: "바인딩 런타임 · Claude Code",
    otherTitle: "팀원의 Mac Studio",
    otherRole: "다른 컴퓨터 · 온라인",
    neverMigrates: "다른 컴퓨터로 이동하지 않음",
    drains: "재연결 → 대기 중인 실행 자동 수신",
    status: { offline: "오프라인", online: "온라인" },
    captions: {
      offline: "실행은 에이전트에 지정된 런타임에 고정됩니다. 다른 컴퓨터가 온라인이어도 옮겨 가지 않습니다.",
      online: "Desktop을 열거나 multica daemon start로 다시 연결되면 대기 중인 실행을 자동으로 가져옵니다.",
    },
  },
  fr: {
    kicker: "LIAISON RUNTIME",
    title: "Comportement lorsqu'un runtime est hors ligne",
    tabs: { offline: "Machine liée hors ligne", online: "Machine reconnectée" } as Record<Mode, string>,
    queueTitle: "FILE MULTICA",
    runTag: "Run #42 · Engineer",
    queuedState: "En attente dans la file",
    claimedState: "Prise en charge → En cours",
    boundTitle: "MacBook Pro de Maya",
    boundRole: "Runtime lié · Claude Code",
    otherTitle: "Mac Studio d'Alex",
    otherRole: "Autre machine · En ligne",
    neverMigrates: "Ne migre jamais vers une autre machine",
    drains: "Reconnexion → reprend la file automatiquement",
    status: { offline: "Hors ligne", online: "En ligne" },
    captions: {
      offline: "L'exécution reste liée au runtime choisi et attend en file même si d'autres machines sont en ligne.",
      online: "Dès que Multica Desktop ou multica daemon start se reconnecte, l'exécution en file démarre.",
    },
  },
};

export function RuntimeOfflineDemo() {
  const t = useDemoCopy(copy);
  const [mode, setMode] = useState<Mode>("offline");
  const online = mode === "online";

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      actions={
        <div className="flex items-center gap-3" role="tablist" aria-label={t.title}>
          {(["offline", "online"] as Mode[]).map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={mode === m}
              onClick={() => setMode(m)}
              className={cn(
                "border-b-[1.5px] pb-1 text-[0.75rem] transition-colors",
                mode === m
                  ? "border-[var(--primary)] font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.tabs[m]}
            </button>
          ))}
        </div>
      }
      caption={t.captions[mode]}
    >
      <svg viewBox="0 0 640 198" className="block h-auto w-full select-none" role="img" aria-label={t.title}>
        <defs>
          <marker id="ro-hi" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Left: Multica Queue */}
        <rect x="14" y="42" width="194" height="114" rx="5" fill="var(--background)" stroke="var(--border)" />
        <text x="28" y="63" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="0.8">
          {t.queueTitle}
        </text>
        <rect
          x="26"
          y="76"
          width="170"
          height="62"
          rx="4"
          fill={online ? "var(--accent)" : "var(--card)"}
          stroke={online ? "var(--primary)" : "var(--docs-rule)"}
          strokeWidth={online ? 1.5 : 1}
        />
        <text x="38" y="99" fill="var(--foreground)" fontSize="12" fontWeight="600">
          {t.runTag}
        </text>
        <text x="38" y="121" fill={online ? "var(--primary)" : "var(--muted-foreground)"} fontSize="11" fontWeight="500">
          {online ? t.claimedState : t.queuedState}
        </text>

        {/* Top connector to Bound Machine */}
        <path
          d="M208 72 H382"
          fill="none"
          stroke={online ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={online ? 1.75 : 1.25}
          strokeDasharray={online ? undefined : "4 4"}
          markerEnd={online ? "url(#ro-hi)" : undefined}
        />
        <text
          x="295"
          y="63"
          textAnchor="middle"
          fill={online ? "var(--primary)" : "var(--muted-foreground)"}
          fontSize="10"
          fontWeight="500"
        >
          {online ? t.drains : t.queuedState}
        </text>

        {/* Bottom blocked connector to Other Machine */}
        <path d="M208 144 H382" fill="none" stroke="var(--border)" strokeWidth="1.2" strokeDasharray="3 4" />
        <line x1="289" y1="138" x2="301" y2="150" stroke="var(--destructive)" strokeWidth="1.6" strokeLinecap="round" />
        <line x1="301" y1="138" x2="289" y2="150" stroke="var(--destructive)" strokeWidth="1.6" strokeLinecap="round" />
        <text x="295" y="166" textAnchor="middle" fill="var(--muted-foreground)" fontSize="10">
          {t.neverMigrates}
        </text>

        {/* Right Top: Bound Machine */}
        <rect
          x="384"
          y="22"
          width="242"
          height="74"
          rx="5"
          fill={online ? "var(--accent)" : "var(--card)"}
          stroke={online ? "var(--primary)" : "var(--border)"}
          strokeWidth={online ? 1.5 : 1}
        />
        <circle cx="402" cy="46" r="4" fill={online ? "var(--success)" : "var(--muted-foreground)"} />
        <text x="414" y="50" fill="var(--foreground)" fontSize="12.5" fontWeight="600">
          {t.boundTitle}
        </text>
        <text x="612" y="50" textAnchor="end" fill={online ? "var(--success)" : "var(--muted-foreground)"} fontFamily="var(--font-mono)" fontSize="10" fontWeight="600">
          {online ? t.status.online : t.status.offline}
        </text>
        <text x="400" y="74" fill="var(--muted-foreground)" fontSize="11">
          {t.boundRole}
        </text>

        {/* Right Bottom: Other Online Machine */}
        <rect x="384" y="114" width="242" height="64" rx="5" fill="var(--card)" stroke="var(--border)" opacity="0.72" />
        <circle cx="402" cy="138" r="4" fill="var(--success)" />
        <text x="414" y="142" fill="var(--foreground)" fontSize="12" fontWeight="500">
          {t.otherTitle}
        </text>
        <text x="400" y="162" fill="var(--muted-foreground)" fontSize="11">
          {t.otherRole}
        </text>
      </svg>
    </EditorialFigure>
  );
}
