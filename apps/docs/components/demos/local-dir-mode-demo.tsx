"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type Mode = "worktree" | "in_place";

const copy = {
  en: {
    kicker: "EXECUTION MODE",
    title: "Two concurrent runs on one local folder",
    tabs: { worktree: "Parallel (worktree)", in_place: "Direct (in place)" } as Record<Mode, string>,
    run1: "ENG-21 · Update pricing copy",
    run2: "ENG-22 · Add FAQ answers",
    workingCopy: "Your working copy",
    path: "~/Projects/marketing-site",
    untouched: "Untouched while runs execute",
    modifiedDirectly: "Edited directly, one run at a time",
    waiting: "Waits until ENG-21 finishes",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "Isolated worktree",
    captions: {
      worktree: "In Parallel mode, each run gets its own git worktree and returns an `agent/...` branch for review. Your working copy stays untouched.",
      in_place: "In Direct mode, runs execute one at a time straight inside your folder. ENG-22 waits for the directory lock until ENG-21 finishes.",
    },
  },
  zh: {
    kicker: "执行模式",
    title: "两条并发运行如何共用同一个本地目录",
    tabs: { worktree: "并行隔离（worktree）", in_place: "原地修改（Direct）" } as Record<Mode, string>,
    run1: "ENG-21 · 更新价格页文案",
    run2: "ENG-22 · 补充常见问题",
    workingCopy: "你的本地工作目录",
    path: "~/Projects/marketing-site",
    untouched: "执行期间保持原样不被改动",
    modifiedDirectly: "直接修改当前目录，同一时间仅一条运行",
    waiting: "等待 ENG-21 释放目录锁",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "独立 worktree",
    captions: {
      worktree: "并行隔离模式下，每条运行各自在独立的 git worktree 中执行并交回 agent/… 分支，完全不碰你当前的工作目录。",
      in_place: "原地修改模式下，同一时间只允许一条运行直接改动目录；ENG-22 会等待 ENG-21 结束后再接力执行。",
    },
  },
  "zh-Hant": {
    kicker: "執行模式",
    title: "兩條並行執行如何共用同一個本機目錄",
    tabs: { worktree: "並行隔離（worktree）", in_place: "原地修改（Direct）" } as Record<Mode, string>,
    run1: "ENG-21 · 更新定價頁文案",
    run2: "ENG-22 · 補充常見問題",
    workingCopy: "你的本機工作目錄",
    path: "~/Projects/marketing-site",
    untouched: "執行期間保持原樣不被改動",
    modifiedDirectly: "直接修改目前目錄，一次僅一條執行",
    waiting: "等待 ENG-21 釋放目錄鎖",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "獨立 worktree",
    captions: {
      worktree: "並行隔離模式下，每條執行各自在獨立 git worktree 中進行並交回 agent/… 分支，不會動到你的工作目錄。",
      in_place: "原地修改模式下，同一時間僅一條執行直接修改目錄；ENG-22 會等待 ENG-21 結束後再開始。",
    },
  },
  ja: {
    kicker: "EXECUTION MODE",
    title: "同じローカルフォルダでの 2 つの同時実行",
    tabs: { worktree: "並列・分離 (worktree)", in_place: "直接編集 (Direct)" } as Record<Mode, string>,
    run1: "ENG-21 · 価格ページを更新",
    run2: "ENG-22 · FAQ を追記",
    workingCopy: "あなたの作業フォルダ",
    path: "~/Projects/marketing-site",
    untouched: "実行中も変更されない",
    modifiedDirectly: "フォルダを直接編集（1 件ずつ順番に実行）",
    waiting: "ENG-21 の完了待ち",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "独立 worktree",
    captions: {
      worktree: "並列モードでは各実行が独立した git worktree で進み、agent/... ブランチを返します。元のフォルダはそのままです。",
      in_place: "直接モードでは 1 件ずつ順番にフォルダへ直接書き込みます。ENG-22 は ENG-21 の完了を待ちます。",
    },
  },
  ko: {
    kicker: "EXECUTION MODE",
    title: "하나의 로컬 폴더에서 두 실행이 동작하는 방식",
    tabs: { worktree: "병렬 격리 (worktree)", in_place: "직접 수정 (Direct)" } as Record<Mode, string>,
    run1: "ENG-21 · 가격 페이지 문구 수정",
    run2: "ENG-22 · FAQ 답변 추가",
    workingCopy: "내 작업 폴더",
    path: "~/Projects/marketing-site",
    untouched: "실행 중에도 변경되지 않음",
    modifiedDirectly: "폴더를 직접 수정 (한 번에 하나씩 실행)",
    waiting: "ENG-21 완료 대기",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "독립 worktree",
    captions: {
      worktree: "병렬 모드에서는 각 실행이 독립 git worktree에서 동시에 진행되며 agent/... 브랜치를 반환합니다.",
      in_place: "직접 모드에서는 한 번에 한 실행만 폴더를 수정하며, 두 번째 실행은 잠금이 풀릴 때까지 대기합니다.",
    },
  },
  fr: {
    kicker: "MODE D'EXÉCUTION",
    title: "Deux exécutions concurrentes sur un même dossier local",
    tabs: { worktree: "Parallèle (worktree)", in_place: "Direct (sur place)" } as Record<Mode, string>,
    run1: "ENG-21 · Mettre à jour les tarifs",
    run2: "ENG-22 · Compléter la FAQ",
    workingCopy: "Votre copie de travail",
    path: "~/Projects/marketing-site",
    untouched: "Intacte pendant les exécutions",
    modifiedDirectly: "Modifiée directement, une exécution à la fois",
    waiting: "Attend la fin de ENG-21",
    branch1: "agent/engineer/eng-21",
    branch2: "agent/engineer/eng-22",
    isolated: "Worktree isolé",
    captions: {
      worktree: "En mode Parallèle, chaque exécution utilise son propre worktree git et rend une branche agent/... sans toucher votre dossier.",
      in_place: "En mode Direct, les exécutions passent une par une directement dans votre dossier.",
    },
  },
};

export function LocalDirModeDemo() {
  const t = useDemoCopy(copy);
  const [mode, setMode] = useState<Mode>("worktree");
  const parallel = mode === "worktree";

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      actions={
        <div className="flex items-center gap-3" role="tablist" aria-label={t.title}>
          {(["worktree", "in_place"] as Mode[]).map((m) => (
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
      <svg viewBox="0 0 640 206" className="block h-auto w-full select-none" role="img" aria-label={t.title}>
        <defs>
          <marker id="ld-hi" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
          <marker id="ld-lo" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5.5" markerHeight="5.5" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Left: Two Runs */}
        <rect x="14" y="24" width="208" height="58" rx="4" fill="var(--accent)" stroke="var(--primary)" strokeWidth="1.5" />
        <text x="26" y="47" fill="var(--foreground)" fontSize="12" fontWeight="600">{t.run1}</text>
        <text x="26" y="66" fill="var(--primary)" fontFamily="var(--font-mono)" fontSize="10">{parallel ? "RUN #1 · ACTIVE" : "RUN #1 · HOLDS LOCK"}</text>

        <rect
          x="14"
          y="108"
          width="208"
          height="58"
          rx="4"
          fill={parallel ? "var(--accent)" : "var(--card)"}
          stroke={parallel ? "var(--primary)" : "var(--border)"}
          strokeWidth={parallel ? 1.5 : 1}
        />
        <text x="26" y="131" fill="var(--foreground)" fontSize="12" fontWeight="600">{t.run2}</text>
        <text x="26" y="150" fill={parallel ? "var(--primary)" : "var(--muted-foreground)"} fontFamily="var(--font-mono)" fontSize="10">
          {parallel ? "RUN #2 · ACTIVE" : t.waiting}
        </text>

        {parallel ? (
          <>
            {/* Parallel connectors to two worktrees */}
            <path d="M222 53 H308" fill="none" stroke="var(--primary)" strokeWidth="1.6" markerEnd="url(#ld-hi)" />
            <path d="M222 137 H308" fill="none" stroke="var(--primary)" strokeWidth="1.6" markerEnd="url(#ld-hi)" />

            {/* Two isolated worktree boxes */}
            <rect x="310" y="24" width="164" height="58" rx="4" fill="var(--card)" stroke="var(--primary)" />
            <text x="322" y="45" fill="var(--muted-foreground)" fontSize="10">{t.isolated}</text>
            <text x="322" y="65" fill="var(--foreground)" fontFamily="var(--font-mono)" fontSize="10.5">{t.branch1}</text>

            <rect x="310" y="108" width="164" height="58" rx="4" fill="var(--card)" stroke="var(--primary)" />
            <text x="322" y="129" fill="var(--muted-foreground)" fontSize="10">{t.isolated}</text>
            <text x="322" y="149" fill="var(--foreground)" fontFamily="var(--font-mono)" fontSize="10.5">{t.branch2}</text>

            {/* Right: Untouched Working Copy */}
            <rect x="494" y="24" width="132" height="142" rx="5" fill="var(--background)" stroke="var(--border)" strokeDasharray="4 3" />
            <text x="506" y="54" fill="var(--foreground)" fontSize="12" fontWeight="600">{t.workingCopy}</text>
            <text x="506" y="74" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5">{t.path}</text>
            <text x="506" y="120" fill="var(--success)" fontSize="11" fontWeight="500">✓ {t.untouched}</text>
          </>
        ) : (
          <>
            {/* Direct serialized connectors into single working copy */}
            <path d="M222 53 H338 V78 H366" fill="none" stroke="var(--primary)" strokeWidth="1.6" markerEnd="url(#ld-hi)" />
            <path d="M222 137 H338 V114 H366" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" strokeDasharray="4 3" markerEnd="url(#ld-lo)" />

            {/* Single Working Copy box */}
            <rect x="368" y="34" width="258" height="124" rx="5" fill="var(--accent)" stroke="var(--primary)" strokeWidth="1.5" />
            <text x="384" y="64" fill="var(--foreground)" fontSize="13" fontWeight="600">{t.workingCopy}</text>
            <text x="384" y="85" fill="var(--primary)" fontFamily="var(--font-mono)" fontSize="11">{t.path}</text>
            <text x="384" y="124" fill="var(--muted-foreground)" fontSize="11.5">{t.modifiedDirectly}</text>
          </>
        )}

        <text x="14" y="194" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="10">
          {t.path}
        </text>
      </svg>
    </EditorialFigure>
  );
}
