"use client";

import { useState } from "react";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type StateId = "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";

const copy = {
  en: {
    kicker: "STATE MACHINE",
    title: "Run lifecycle and retry paths",
    retryLabel: "Manual retry → new run",
    stopLabel: "Stop run",
    states: {
      queued: { label: "Queued", detail: "Created by an assignment, @mention, chat, or autopilot. Waits until the agent's bound runtime is online and free." },
      dispatched: { label: "Starting", detail: "The online runtime claimed the run, prepared the working directory, and is launching the local AI coding tool." },
      running: { label: "Working", detail: "The tool is actively executing. Live steps stream into the Execution log; you can steer mid-run or stop it." },
      completed: { label: "Completed", detail: "This run finished cleanly. A completed run does not automatically mark the issue Done — status stays with the workflow." },
      failed: { label: "Failed", detail: "Stopped with an error (e.g. provider rate limit or auth error). The failed record stays in Latest; Retry creates a fresh run." },
      cancelled: { label: "Cancelled", detail: "Stopped manually by a member. Cancelling leaves the issue status untouched and can be retried anytime." },
    } as Record<StateId, { label: string; detail: string }>,
  },
  zh: {
    kicker: "状态机",
    title: "运行的生命周期与重试路径",
    retryLabel: "手动重试 → 创建新运行",
    stopLabel: "手动停止",
    states: {
      queued: { label: "排队中", detail: "由分配、@提及、对话或自动化创建，等待智能体绑定的运行时在线并空闲后领取。" },
      dispatched: { label: "启动中", detail: "在线运行时已领取运行，正在准备本地工作目录并启动 AI 编程工具。" },
      running: { label: "正在工作", detail: "工具正在执行，步骤实时写入执行日志；期间可以插话纠偏，也可以随时停止。" },
      completed: { label: "已完成", detail: "本次运行正常结束。运行完成不等于任务完成，任务状态由工作流或人工验收决定。" },
      failed: { label: "失败", detail: "因限流、凭据失效等原因中止；失败记录保留在「最近」中，点击重试会创建新运行。" },
      cancelled: { label: "已取消", detail: "由成员手动停止；停止运行不会修改任务状态，已取消的运行同样支持重试。" },
    } as Record<StateId, { label: string; detail: string }>,
  },
  "zh-Hant": {
    kicker: "狀態機",
    title: "執行的生命週期與重試路徑",
    retryLabel: "手動重試 → 建立新執行",
    stopLabel: "手動停止",
    states: {
      queued: { label: "排隊中", detail: "由指派、@提及、對話或自動化建立，等待綁定的執行環境上線領取。" },
      dispatched: { label: "啟動中", detail: "執行環境已領取，正在準備工作目錄並啟動 AI 程式設計工具。" },
      running: { label: "正在工作", detail: "工具執行中，步驟即時寫入執行日誌；期間可插話或手動停止。" },
      completed: { label: "已完成", detail: "本次執行正常結束，不等於任務本身完成。" },
      failed: { label: "失敗", detail: "因速率限制或憑證錯誤中止；重試會建立一條新的執行記錄。" },
      cancelled: { label: "已取消", detail: "由成員手動停止，不改變任務狀態，且可隨時重試。" },
    } as Record<StateId, { label: string; detail: string }>,
  },
  ja: {
    kicker: "STATE MACHINE",
    title: "実行のライフサイクルと再試行",
    retryLabel: "手動リトライ → 新規実行",
    stopLabel: "停止",
    states: {
      queued: { label: "待機中", detail: "割り当て・@メンション・チャット・自動化で作成され、ランタイムの取得を待ちます。" },
      dispatched: { label: "開始中", detail: "オンラインのランタイムが取得し、AI コーディングツールを起動しています。" },
      running: { label: "作業中", detail: "実行ログに手順がリアルタイム記録されます。途中で指示を追加したり停止できます。" },
      completed: { label: "完了", detail: "この実行は正常終了しました。タスク完了とは別管理です。" },
      failed: { label: "失敗", detail: "レート制限などで中断。履歴は残り、リトライすると新しい実行が作成されます。" },
      cancelled: { label: "キャンセル", detail: "メンバーが手動停止。タスクのステータスは変更されず、再試行も可能です。" },
    } as Record<StateId, { label: string; detail: string }>,
  },
  ko: {
    kicker: "STATE MACHINE",
    title: "실행 생명주기와 재시도 경로",
    retryLabel: "수동 재시도 → 새 실행",
    stopLabel: "중지",
    states: {
      queued: { label: "대기 중", detail: "할당·@멘션·채팅·오토파일럿으로 생성되어 연결된 런타임이 수락하기를 기다립니다." },
      dispatched: { label: "시작 중", detail: "온라인 런타임이 실행을 받아 작업 디렉터리와 AI 코딩 도구를 준비합니다." },
      running: { label: "작업 중", detail: "실시간 단계가 실행 로그에 기록됩니다. 중간에 방향을 수정하거나 중지할 수 있습니다." },
      completed: { label: "완료됨", detail: "이번 실행이 정상 종료되었습니다. 태스크 완료 여부와는 별개입니다." },
      failed: { label: "실패", detail: "속도 제한 등으로 중단되었습니다. 기록은 유지되며 재시도 시 새 실행을 만듭니다." },
      cancelled: { label: "취소됨", detail: "멤버가 수동 중지했습니다. 태스크 상태는 유지되며 언제든 재시도할 수 있습니다." },
    } as Record<StateId, { label: string; detail: string }>,
  },
  fr: {
    kicker: "MACHINE D'ÉTATS",
    title: "Cycle de vie d'une exécution et relance",
    retryLabel: "Relance manuelle → nouvelle exécution",
    stopLabel: "Arrêter",
    states: {
      queued: { label: "En file", detail: "Créée par assignation, @mention, chat ou automatisation ; attend le runtime lié." },
      dispatched: { label: "Démarrage", detail: "Le runtime en ligne récupère l'exécution et lance l'outil de codage IA." },
      running: { label: "En cours", detail: "Les étapes s'affichent en direct ; vous pouvez réorienter ou arrêter l'exécution." },
      completed: { label: "Terminée", detail: "L'exécution s'est achevée. Cela ne marque pas automatiquement la tâche comme finie." },
      failed: { label: "Échec", detail: "Interrompue par une erreur (ex. limite de débit). Relancer crée une nouvelle exécution." },
      cancelled: { label: "Annulée", detail: "Arrêtée manuellement. Le statut de la tâche ne change pas et la relance reste possible." },
    } as Record<StateId, { label: string; detail: string }>,
  },
};

export function RunLifecycleDemo() {
  const t = useDemoCopy(copy);
  const [selected, setSelected] = useState<StateId>("running");

  const accentFor = (id: StateId) => {
    if (id === "completed") return "var(--success)";
    if (id === "failed") return "var(--destructive)";
    if (id === "cancelled") return "var(--muted-foreground)";
    return "var(--primary)";
  };

  const box = (id: StateId, x: number, y: number, w: number, h: number) => {
    const active = selected === id;
    const tone = accentFor(id);
    return (
      <g key={id} style={{ cursor: "pointer" }} onClick={() => setSelected(id)}>
        <rect
          x={x}
          y={y}
          width={w}
          height={h}
          rx="4"
          fill={active ? "var(--accent)" : "var(--card)"}
          stroke={active ? tone : "var(--border)"}
          strokeWidth={active ? 1.6 : 1}
        />
        <circle cx={x + 15} cy={y + h / 2} r="3.5" fill={tone} />
        <text
          x={x + 26}
          y={y + h / 2 + 4}
          fill="var(--foreground)"
          fontSize="12"
          fontWeight={active ? "600" : "500"}
        >
          {t.states[id].label}
        </text>
      </g>
    );
  };

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      caption={
        <span>
          <strong className="font-semibold text-foreground">{t.states[selected].label}</strong>
          {" — "}
          {t.states[selected].detail}
        </span>
      }
    >
      <svg viewBox="0 0 640 218" className="block h-auto w-full select-none" role="img" aria-label={t.title}>
        <defs>
          <marker id="rl-arr" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
          <marker id="rl-arr-p" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Main forward spine */}
        <path d="M120 96 H162" fill="none" stroke="var(--primary)" strokeWidth="1.5" markerEnd="url(#rl-arr-p)" />
        <path d="M270 96 H312" fill="none" stroke="var(--primary)" strokeWidth="1.5" markerEnd="url(#rl-arr-p)" />

        {/* Fan-out from Working to Completed / Failed / Cancelled */}
        <path d="M424 84 H456 V40 H492" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" markerEnd="url(#rl-arr)" />
        <path d="M424 96 H492" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" markerEnd="url(#rl-arr)" />
        <path d="M424 108 H456 V152 H492" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" markerEnd="url(#rl-arr)" />
        <text x="456" y="145" textAnchor="middle" fill="var(--muted-foreground)" fontSize="9.5">
          {t.stopLabel}
        </text>

        {/* Manual retry loop from Failed & Cancelled back to Queued */}
        <path
          d="M558 116 V136 M614 96 H626 V196 H68 V118"
          fill="none"
          stroke={selected === "failed" || selected === "cancelled" ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={selected === "failed" || selected === "cancelled" ? 1.5 : 1.2}
          strokeDasharray="4 3"
          markerEnd={selected === "failed" || selected === "cancelled" ? "url(#rl-arr-p)" : "url(#rl-arr)"}
        />
        <path
          d="M614 152 H626"
          fill="none"
          stroke={selected === "cancelled" ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth="1.2"
          strokeDasharray="4 3"
        />
        <rect x="244" y="186" width="176" height="20" rx="3" fill="var(--card)" />
        <text x="332" y="199" textAnchor="middle" fill="var(--muted-foreground)" fontSize="10" fontWeight="500">
          {t.retryLabel}
        </text>

        {box("queued", 16, 76, 104, 40)}
        {box("dispatched", 164, 76, 106, 40)}
        {box("running", 314, 76, 110, 40)}
        {box("completed", 494, 20, 120, 40)}
        {box("failed", 494, 76, 120, 40)}
        {box("cancelled", 494, 132, 120, 40)}
      </svg>
    </EditorialFigure>
  );
}
