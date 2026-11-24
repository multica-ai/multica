"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

const copy = {
  en: {
    kicker: "EXECUTION BOUNDARY",
    title: "How a run moves between Multica and your computer",
    leftZone: "MULTICA · RECORDS & COORDINATES",
    rightZone: "YOUR COMPUTER · EXECUTES LOCALLY",
    nodes: {
      issue: { tag: "01", title: "Issue & agent config", sub: "Description, thread, skills, model" },
      queue: { tag: "02", title: "Queued run", sub: "Waiting for the bound runtime" },
      daemon: { tag: "03", title: "Daemon claims run", sub: "Checks out workspace & syncs skills" },
      tool: { tag: "04", title: "AI coding tool", sub: "Reads files, runs commands locally" },
      writeback: "05 · Progress, comments & PRs stream back",
    },
    steps: [
      { label: "01 Context", detail: "The issue holds the prompt and discussion; Multica pairs it with the agent's instructions, skills, and model." },
      { label: "02 Queue", detail: "Triggering creates a run in Multica's queue, bound to the agent's configured runtime." },
      { label: "03 Claim", detail: "The online daemon on your computer claims the run and prepares the local workspace." },
      { label: "04 Execute", detail: "Your local AI coding tool (Claude Code, Codex, etc.) edits files and runs tests using your local credentials." },
      { label: "05 Write back", detail: "Live logs, comments, status updates, and linked PRs stream back to the original issue." },
    ],
  },
  zh: {
    kicker: "执行边界",
    title: "一次运行如何在 Multica 与你的电脑之间流转",
    leftZone: "MULTICA · 记录与协调",
    rightZone: "你的电脑 · 本地执行",
    nodes: {
      issue: { tag: "01", title: "任务与智能体配置", sub: "描述、评论、Skills 与模型" },
      queue: { tag: "02", title: "运行队列", sub: "等待绑定的运行时领取" },
      daemon: { tag: "03", title: "守护进程领取", sub: "准备目录并下发 Skills" },
      tool: { tag: "04", title: "本地 AI 编程工具", sub: "读写代码、执行命令与测试" },
      writeback: "05 · 进度日志、评论与 PR 实时写回任务",
    },
    steps: [
      { label: "01 上下文", detail: "任务保存描述与讨论，Multica 结合智能体的指令、Skills 与模型配置组装上下文。" },
      { label: "02 入队", detail: "触发后在 Multica 创建一条运行并进入队列，绑定到该智能体配置的运行时。" },
      { label: "03 领取", detail: "你电脑上的在线守护进程领取运行，检出仓库或准备本地工作目录。" },
      { label: "04 本地执行", detail: "本机 AI 编程工具（Claude Code、Codex 等）使用本地凭据修改代码并运行命令。" },
      { label: "05 结果回写", detail: "实时执行日志、评论、状态变更与 PR 链接同步写回原任务时间线。" },
    ],
  },
  "zh-Hant": {
    kicker: "執行邊界",
    title: "一次執行如何在 Multica 與你的電腦之間流轉",
    leftZone: "MULTICA · 記錄與協調",
    rightZone: "你的電腦 · 本機執行",
    nodes: {
      issue: { tag: "01", title: "任務與智能體設定", sub: "描述、評論、Skills 與模型" },
      queue: { tag: "02", title: "執行佇列", sub: "等待綁定的執行環境領取" },
      daemon: { tag: "03", title: "守護行程領取", sub: "準備目錄並同步 Skills" },
      tool: { tag: "04", title: "本機 AI 程式設計工具", sub: "讀寫程式碼、執行指令與測試" },
      writeback: "05 · 進度日誌、評論與 PR 即時寫回任務",
    },
    steps: [
      { label: "01 上下文", detail: "任務保存描述與討論，Multica 結合智能體的指令、Skills 與模型組裝上下文。" },
      { label: "02 入列", detail: "觸發後在 Multica 建立一條執行並進入佇列，綁定至該智能體設定的執行環境。" },
      { label: "03 領取", detail: "你電腦上的線上守護行程領取執行並準備本機工作目錄。" },
      { label: "04 本機執行", detail: "本機 AI 程式設計工具使用本機憑證修改檔案並執行命令。" },
      { label: "05 結果回寫", detail: "即時執行日誌、評論、狀態更新與 PR 同步寫回原任務時間線。" },
    ],
  },
  ja: {
    kicker: "EXECUTION BOUNDARY",
    title: "Multica とローカル PC の間の実行フロー",
    leftZone: "MULTICA · 記録と調整",
    rightZone: "あなたの PC · ローカル実行",
    nodes: {
      issue: { tag: "01", title: "タスクとエージェント設定", sub: "説明・コメント・Skills・モデル" },
      queue: { tag: "02", title: "実行キュー", sub: "紐づくランタイムの取得待ち" },
      daemon: { tag: "03", title: "デーモンが取得", sub: "作業ディレクトリと Skills を準備" },
      tool: { tag: "04", title: "AI コーディングツール", sub: "ファイル編集・コマンド実行" },
      writeback: "05 · ログ・コメント・PR をタスクへ書き戻す",
    },
    steps: [
      { label: "01 コンテキスト", detail: "タスクの説明と議論に、エージェントの指示・Skills・モデルを組み合わせて渡します。" },
      { label: "02 キュー", detail: "トリガーにより実行が作成され、指定ランタイム向けのキューに入ります。" },
      { label: "03 取得", detail: "PC 上のオンラインデーモンが実行を取得し、作業ディレクトリを準備します。" },
      { label: "04 ローカル実行", detail: "PC に導入済みの AI コーディングツールがローカル認証情報で作業します。" },
      { label: "05 書き戻し", detail: "進捗ログ、コメント、ステータス更新、PR が元のタスクに反映されます。" },
    ],
  },
  ko: {
    kicker: "EXECUTION BOUNDARY",
    title: "Multica와 내 컴퓨터 사이의 실행 흐름",
    leftZone: "MULTICA · 기록 및 조율",
    rightZone: "내 컴퓨터 · 로컬 실행",
    nodes: {
      issue: { tag: "01", title: "태스크 및 에이전트 구성", sub: "설명, 댓글, Skills, 모델" },
      queue: { tag: "02", title: "실행 대기열", sub: "연결된 런타임 수신 대기" },
      daemon: { tag: "03", title: "데몬이 실행 수신", sub: "작업 디렉터리 및 Skills 동기화" },
      tool: { tag: "04", title: "AI 코딩 도구", sub: "로컬 파일 수정 및 명령 실행" },
      writeback: "05 · 로그·댓글·PR이 원래 태스크로 기록됨",
    },
    steps: [
      { label: "01 컨텍스트", detail: "태스크 설명·댓글과 에이전트 지침·Skills·모델 설정을 결합합니다." },
      { label: "02 대기열", detail: "트리거 시 실행이 생성되어 해당 런타임 전용 대기열에 들어갑니다." },
      { label: "03 수신", detail: "컴퓨터의 온라인 데몬이 실행을 받아 로컬 작업 환경을 준비합니다." },
      { label: "04 로컬 실행", detail: "로컬 AI 코딩 도구가 내 인증 정보로 파일을 읽고 명령을 실행합니다." },
      { label: "05 결과 반환", detail: "실시간 로그, 댓글, 상태 변경, PR 링크가 태스크 타임라인에 기록됩니다." },
    ],
  },
  fr: {
    kicker: "FRONTIÈRE D'EXÉCUTION",
    title: "Circulation d'une exécution entre Multica et votre ordinateur",
    leftZone: "MULTICA · ENREGISTRE ET COORDONNE",
    rightZone: "VOTRE ORDINATEUR · EXÉCUTE LOCALEMENT",
    nodes: {
      issue: { tag: "01", title: "Tâche & config agent", sub: "Description, fil, skills, modèle" },
      queue: { tag: "02", title: "File d'exécution", sub: "En attente du runtime lié" },
      daemon: { tag: "03", title: "Le daemon prend la tâche", sub: "Prépare le dossier & skills" },
      tool: { tag: "04", title: "Outil de codage IA", sub: "Édite et exécute localement" },
      writeback: "05 · Journaux, commentaires et PR réécrits",
    },
    steps: [
      { label: "01 Contexte", detail: "La tâche fournit le contexte, combiné aux instructions, skills et modèle de l'agent." },
      { label: "02 File", detail: "Chaque déclenchement crée une exécution liée au runtime configuré." },
      { label: "03 Prise en charge", detail: "Le daemon en ligne sur votre machine récupère l'exécution et prépare le dépôt." },
      { label: "04 Exécution locale", detail: "Votre outil IA local modifie les fichiers et lance les commandes avec vos propres accès." },
      { label: "05 Retour", detail: "Journaux en direct, commentaires, statut et PR reviennent sur la tâche." },
    ],
  },
};

export function RunPathDemo() {
  const t = useDemoCopy(copy);
  const [step, setStep] = useState(0);

  const on = (i: number) => step === i;
  const stroke = (i: number) => (on(i) ? "var(--primary)" : "var(--border)");
  const fill = (i: number) => (on(i) ? "var(--accent)" : "var(--card)");

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      actions={
        <div className="flex flex-wrap items-center gap-1" role="tablist" aria-label={t.title}>
          {t.steps.map((s, i) => (
            <button
              key={s.label}
              type="button"
              role="tab"
              aria-selected={step === i}
              onClick={() => setStep(i)}
              className={cn(
                "border-b-[1.5px] px-2 pb-1 font-mono text-[0.7rem] transition-colors",
                step === i
                  ? "border-[var(--primary)] font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {s.label}
            </button>
          ))}
        </div>
      }
      caption={t.steps[step]!.detail}
    >
      <svg viewBox="0 0 640 248" className="block h-auto w-full select-none" role="img" aria-label={t.title}>
        <defs>
          <marker id="rp-hi" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
          <marker id="rp-lo" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5.5" markerHeight="5.5" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Left & Right boundary zones */}
        <rect x="8" y="8" width="296" height="186" rx="5" fill="var(--background)" stroke="var(--border)" />
        <rect x="336" y="8" width="296" height="186" rx="5" fill="var(--background)" stroke="var(--border)" />
        <line x1="320" y1="8" x2="320" y2="194" stroke="var(--docs-rule)" strokeDasharray="4 4" />

        <text x="22" y="27" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="0.8">
          {t.leftZone}
        </text>
        <text x="350" y="27" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="0.8">
          {t.rightZone}
        </text>

        {/* Connectors */}
        <path d="M156 96 V120" fill="none" stroke={on(1) ? "var(--primary)" : "var(--docs-faint)"} strokeWidth={on(1) ? 1.75 : 1.25} markerEnd={on(1) ? "url(#rp-hi)" : "url(#rp-lo)"} />
        <path d="M286 148 H318 V69 H350" fill="none" stroke={on(2) ? "var(--primary)" : "var(--docs-faint)"} strokeWidth={on(2) ? 1.75 : 1.25} markerEnd={on(2) ? "url(#rp-hi)" : "url(#rp-lo)"} />
        <path d="M484 96 V120" fill="none" stroke={on(3) ? "var(--primary)" : "var(--docs-faint)"} strokeWidth={on(3) ? 1.75 : 1.25} markerEnd={on(3) ? "url(#rp-hi)" : "url(#rp-lo)"} />

        {/* Write-back return */}
        <path
          d="M484 176 V222 H40 V69 H24"
          fill="none"
          stroke={on(4) ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={on(4) ? 1.75 : 1.25}
          strokeDasharray="4 3"
          markerEnd={on(4) ? "url(#rp-hi)" : "url(#rp-lo)"}
        />
        <rect x="176" y="212" width="288" height="20" rx="3" fill="var(--card)" />
        <text
          x="320"
          y="225"
          textAnchor="middle"
          fill={on(4) ? "var(--primary)" : "var(--muted-foreground)"}
          fontSize="10.5"
          fontWeight={on(4) ? "600" : "500"}
          style={{ cursor: "pointer" }}
          onClick={() => setStep(4)}
        >
          {t.nodes.writeback}
        </text>

        {/* 4 Stage Boxes */}
        {(
          [
            [0, 26, 42, 260, 54, t.nodes.issue],
            [1, 26, 122, 260, 54, t.nodes.queue],
            [2, 354, 42, 260, 54, t.nodes.daemon],
            [3, 354, 122, 260, 54, t.nodes.tool],
          ] as [number, number, number, number, number, { tag: string; title: string; sub: string }][]
        ).map(([idx, x, y, w, h, node]) => (
          <g key={idx} style={{ cursor: "pointer" }} onClick={() => setStep(idx)}>
            <rect x={x} y={y} width={w} height={h} rx="4" fill={fill(idx)} stroke={stroke(idx)} strokeWidth={on(idx) ? 1.6 : 1} />
            <text x={x + 12} y={y + 21} fill="var(--primary)" fontFamily="var(--font-mono)" fontSize="9.5" fontWeight="600">
              {node.tag}
            </text>
            <text x={x + 32} y={y + 21} fill="var(--foreground)" fontSize="12.5" fontWeight="600">
              {node.title}
            </text>
            <text x={x + 12} y={y + 40} fill="var(--muted-foreground)" fontSize="11">
              {node.sub}
            </text>
          </g>
        ))}
      </svg>
    </EditorialFigure>
  );
}
