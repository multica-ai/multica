"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Ban, CheckCircle2, ChevronRight, Loader2, RotateCcw, Square, XCircle } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type RunStatus = "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";
type IssueStatus = "todo" | "in_progress" | "in_review";
type Run = { id: number; status: RunStatus; runningSince?: number };

const copy = {
  en: {
    title: "OPS-21 · Compare the office-supply quotes from five suppliers",
    agent: "Ops helper",
    you: "You",
    statusLabel: "Status",
    issueStatus: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<IssueStatus, string>,
    isWorking: (name: string) => `${name} is working`,
    isQueued: (name: string) => `${name} is queued`,
    request: "@Ops helper Put the five suppliers' quotes into one comparison table.",
    trigger: "@Ops helper Put the five suppliers' quotes into one comparison table.",
    section: "Execution log",
    latest: "Latest",
    runStatus: {
      queued: "Queued",
      dispatched: "Starting",
      running: "Working",
      completed: "Completed",
      failed: "Failed",
      cancelled: "Cancelled",
    } as Record<RunStatus, string>,
    failedReason: "Rate limited by provider",
    cancelledBy: "Cancelled by You",
    duration: "1m 48s",
    retry: "Retry run",
    cancel: "Cancel run",
    dialogTitle: "Stop this run?",
    dialogBody: "The run will be cancelled and cannot be resumed. Running work may take a few seconds to fully halt.",
    keep: "Keep running",
    stop: "Stop run",
    reply:
      "The comparison table is ready: five suppliers side by side on unit price, minimum order and delivery time. Please review.",
    captions: {
      idle: "You asked Ops helper in a comment to compare the quotes.",
      queued: "The comment created a run. It waits in the queue for the runtime to claim it.",
      dispatched: "The runtime claimed the run and is starting the AI coding tool.",
      running: "Ops helper is working and moved the issue to In Progress.",
      failed:
        "The run failed: the model provider was rate limiting requests. This kind of failure is not retried automatically, so the issue went back to Todo. Click Retry run.",
      retryQueued: "Retrying created a new run. The failed one stays under Latest.",
      retryRunning: "The new run is working. To stop it midway, click the stop button; otherwise let it finish.",
      confirm: "Stopping cancels this run, and it cannot be resumed.",
      cancelled:
        "The run was cancelled. Stopping a run does not change the issue status, and a cancelled run can be retried too. Use Replay to start over.",
      done: "The new run completed and Ops helper moved the issue to In Review. Each attempt kept its own record. Use Replay to try another path.",
    },
  },
  zh: {
    title: "OPS-21 · 整理五家供应商的办公用品报价",
    agent: "运营助手",
    you: "你",
    statusLabel: "状态",
    issueStatus: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<IssueStatus, string>,
    isWorking: (name: string) => `${name} 在工作`,
    isQueued: (name: string) => `${name} 排队中`,
    request: "@运营助手 把五家供应商的报价整理成一张对比表。",
    trigger: "@运营助手 把五家供应商的报价整理成一张对比表。",
    section: "执行日志",
    latest: "最近",
    runStatus: {
      queued: "排队中",
      dispatched: "启动中",
      running: "正在工作",
      completed: "已完成",
      failed: "失败",
      cancelled: "已取消",
    } as Record<RunStatus, string>,
    failedReason: "受到提供商速率限制",
    cancelledBy: "已由 你 取消",
    duration: "1m 48s",
    retry: "重试运行",
    cancel: "取消运行",
    dialogTitle: "确定停止本次运行？",
    dialogBody: "本次运行会被取消且无法恢复。进行中的运行可能需要几秒才能完全停止。",
    keep: "保留运行",
    stop: "停止运行",
    reply: "对比表已整理好：五家供应商按单价、起订量和交货周期并排列出，请检查。",
    captions: {
      idle: "你在评论里请 运营助手 整理报价。",
      queued: "评论触发了一次运行，先进入队列，等运行时领取。",
      dispatched: "运行时已领取，正在启动 AI 编程工具。",
      running: "运营助手 开始工作，把任务改为进行中。",
      failed: "运行失败：模型服务限流了。这类失败不会自动重试，任务回到待办。点击重试运行。",
      retryQueued: "重试创建了一条新的运行，失败的那条留在\"最近\"里。",
      retryRunning: "新的运行正在工作。想中途停下，可以点停止按钮；不停就等它完成。",
      confirm: "停止后，这次运行会被取消，无法恢复。",
      cancelled: "运行已取消。停止运行不会改变任务状态；已取消的运行同样可以重试。点右上角可以重新演示。",
      done: "新的运行完成，运营助手 把任务改为审核中。两次运行各留一条记录。点右上角可以换一条路径再试。",
    },
  },
  ja: {
    title: "OPS-21 · 5 社の事務用品の見積もりを比較する",
    agent: "運用アシスタント",
    you: "あなた",
    statusLabel: "ステータス",
    issueStatus: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<IssueStatus, string>,
    isWorking: (name: string) => `${name} が作業中`,
    isQueued: (name: string) => `${name} が待機中`,
    request: "@運用アシスタント 5 社の見積もりを 1 枚の比較表にまとめてください。",
    trigger: "@運用アシスタント 5 社の見積もりを 1 枚の比較表にまとめてください。",
    section: "実行ログ",
    latest: "最新",
    runStatus: {
      queued: "待機中",
      dispatched: "開始中",
      running: "作業中",
      completed: "完了",
      failed: "失敗",
      cancelled: "キャンセル済み",
    } as Record<RunStatus, string>,
    failedReason: "プロバイダーによるレート制限",
    cancelledBy: "あなた がキャンセル",
    duration: "1m 48s",
    retry: "実行を再試行",
    cancel: "実行をキャンセル",
    dialogTitle: "この実行を停止しますか？",
    dialogBody: "実行はキャンセルされ、再開できません。実行中の処理が完全に停止するまで数秒かかる場合があります。",
    keep: "実行を続ける",
    stop: "実行を停止",
    reply: "比較表ができました。5 社を単価、最小発注数、納期で並べています。確認をお願いします。",
    captions: {
      idle: "コメントで 運用アシスタント に見積もりの比較を頼みました。",
      queued: "コメントから実行が 1 件作られ、ランタイムが受け取るまでキューで待ちます。",
      dispatched: "ランタイムが実行を受け取り、AI コーディングツールを起動しています。",
      running: "運用アシスタント が作業を始め、タスクを進行中にしました。",
      failed:
        "実行が失敗しました。モデルプロバイダーのレート制限です。この種の失敗は自動では再試行されず、タスクは未着手に戻りました。「実行を再試行」をクリックしてください。",
      retryQueued: "再試行で新しい実行が作られました。失敗した実行は「最新」に残ります。",
      retryRunning: "新しい実行が作業中です。途中で止めたいときは停止ボタンを、止めなければ完了を待ちます。",
      confirm: "停止するとこの実行はキャンセルされ、再開できません。",
      cancelled:
        "実行がキャンセルされました。実行を止めてもタスクのステータスは変わらず、キャンセルした実行も再試行できます。「もう一度」で最初から見られます。",
      done: "新しい実行が完了し、運用アシスタント がタスクをレビュー中にしました。2 回の実行はそれぞれ記録に残ります。「もう一度」で別の流れも試せます。",
    },
  },
  ko: {
    title: "OPS-21 · 공급업체 다섯 곳의 사무용품 견적 비교",
    agent: "운영 도우미",
    you: "나",
    statusLabel: "상태",
    issueStatus: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<IssueStatus, string>,
    isWorking: (name: string) => `${name} 작업 중`,
    isQueued: (name: string) => `${name} 대기 중`,
    request: "@운영 도우미 공급업체 다섯 곳의 견적을 비교표 하나로 정리해 주세요.",
    trigger: "@운영 도우미 공급업체 다섯 곳의 견적을 비교표 하나로 정리해 주세요.",
    section: "실행 로그",
    latest: "최근",
    runStatus: {
      queued: "대기 중",
      dispatched: "시작 중",
      running: "작업 중",
      completed: "완료됨",
      failed: "실패함",
      cancelled: "취소됨",
    } as Record<RunStatus, string>,
    failedReason: "공급자 요청 한도에 도달함",
    cancelledBy: "내가 취소함",
    duration: "1m 48s",
    retry: "실행 다시 시도",
    cancel: "실행 취소",
    dialogTitle: "이 실행을 중지할까요?",
    dialogBody: "실행이 취소되며 재개할 수 없습니다. 진행 중인 실행이 완전히 멈추기까지 몇 초가 걸릴 수 있습니다.",
    keep: "계속 실행",
    stop: "실행 중지",
    reply: "비교표를 정리했습니다. 다섯 곳을 단가, 최소 주문량, 납기 기준으로 나란히 놓았으니 확인해 주세요.",
    captions: {
      idle: "댓글로 운영 도우미에게 견적 비교를 요청했습니다.",
      queued: "댓글이 실행을 하나 만들었습니다. 런타임이 가져갈 때까지 대기열에서 기다립니다.",
      dispatched: "런타임이 실행을 가져가 AI 코딩 도구를 시작하고 있습니다.",
      running: "운영 도우미가 작업을 시작하고 태스크를 진행 중으로 바꿨습니다.",
      failed:
        "실행이 실패했습니다. 모델 공급자가 요청 한도를 걸었습니다. 이런 실패는 자동으로 다시 시도되지 않아 태스크가 할 일로 돌아갔습니다. 실행 다시 시도를 클릭하세요.",
      retryQueued: "다시 시도하면 새 실행이 만들어집니다. 실패한 실행은 \"최근\"에 남습니다.",
      retryRunning: "새 실행이 작업 중입니다. 중간에 멈추려면 중지 버튼을 누르고, 아니면 끝날 때까지 기다리세요.",
      confirm: "중지하면 이 실행은 취소되고 재개할 수 없습니다.",
      cancelled:
        "실행이 취소되었습니다. 실행을 중지해도 태스크 상태는 바뀌지 않으며, 취소된 실행도 다시 시도할 수 있습니다. \"다시 보기\"로 처음부터 볼 수 있습니다.",
      done: "새 실행이 완료되었고 운영 도우미가 태스크를 리뷰 중으로 바꿨습니다. 두 번의 실행이 각각 기록으로 남습니다. \"다시 보기\"로 다른 흐름도 시도해 보세요.",
    },
  },
  fr: {
    title: "OPS-21 · Comparer les devis de fournitures de cinq fournisseurs",
    agent: "Assistant ops",
    you: "Vous",
    statusLabel: "Statut",
    issueStatus: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<IssueStatus, string>,
    isWorking: (name: string) => `${name} travaille`,
    isQueued: (name: string) => `${name} est en file d'attente`,
    request: "@Assistant ops Regroupe les devis des cinq fournisseurs dans un seul tableau comparatif.",
    trigger: "@Assistant ops Regroupe les devis des cinq fournisseurs dans un seul tableau comparatif.",
    section: "Journal d'exécution",
    latest: "Récentes",
    runStatus: {
      queued: "En file d'attente",
      dispatched: "Démarrage",
      running: "Au travail",
      completed: "Terminée",
      failed: "En échec",
      cancelled: "Annulée",
    } as Record<RunStatus, string>,
    failedReason: "Limite de requêtes du fournisseur atteinte",
    cancelledBy: "Annulée par Vous",
    duration: "1m 48s",
    retry: "Relancer l'exécution",
    cancel: "Annuler l'exécution",
    dialogTitle: "Arrêter cette exécution ?",
    dialogBody: "L'exécution sera annulée et ne pourra pas être reprise. Le travail en cours peut mettre quelques secondes à s'arrêter complètement.",
    keep: "Laisser tourner",
    stop: "Arrêter l'exécution",
    reply:
      "Le tableau comparatif est prêt : les cinq fournisseurs côte à côte selon le prix unitaire, la quantité minimale et le délai de livraison. Merci de vérifier.",
    captions: {
      idle: "Dans un commentaire, vous avez demandé à Assistant ops de comparer les devis.",
      queued: "Le commentaire a créé une exécution. Elle attend dans la file qu'un runtime la prenne.",
      dispatched: "Le runtime a pris l'exécution et démarre l'outil de code IA.",
      running: "Assistant ops travaille et a passé la tâche à En cours.",
      failed:
        "L'exécution a échoué : le fournisseur du modèle limitait les requêtes. Ce type d'échec n'est pas relancé automatiquement, la tâche est donc revenue à À faire. Cliquez sur Relancer l'exécution.",
      retryQueued: "La relance a créé une nouvelle exécution. Celle en échec reste sous Récentes.",
      retryRunning: "La nouvelle exécution travaille. Pour l'arrêter en cours de route, cliquez sur le bouton d'arrêt ; sinon, laissez-la finir.",
      confirm: "Arrêter annule cette exécution, sans possibilité de reprise.",
      cancelled:
        "L'exécution est annulée. Arrêter une exécution ne change pas le statut de la tâche, et une exécution annulée peut elle aussi être relancée. Rejouer recommence depuis le début.",
      done: "La nouvelle exécution est terminée et Assistant ops a passé la tâche à En relecture. Chaque tentative garde sa propre trace. Rejouer permet d'essayer un autre chemin.",
    },
  },
};

type Phase = "idle" | "first" | "failed" | "second" | "confirm" | "cancelled" | "done";

const ACTIVE: RunStatus[] = ["queued", "dispatched", "running"];

const issueDot: Record<IssueStatus, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

const activeTone: Partial<Record<RunStatus, string>> = {
  queued: "text-warning",
  dispatched: "text-warning",
};

export function RunLifecycleDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [cycle, setCycle] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [runs, setRuns] = useState<Run[]>([]);
  const [issueStatus, setIssueStatus] = useState<IssueStatus>("todo");
  const [now, setNow] = useState(0);
  // Index of the next scripted step of the retried run, so closing the stop
  // dialog with "Keep running" resumes where the script paused.
  const secondStep = useRef(0);

  const setRun = useCallback((id: number, status: RunStatus) => {
    setRuns((rs) =>
      rs.map((r) => (r.id === id ? { ...r, status, runningSince: status === "running" ? Date.now() : r.runningSince } : r)),
    );
  }, []);

  // Opening: the first run moves through the queue and fails.
  useEffect(() => {
    if (!visible) return;
    clear();
    setPhase("first");
    setIssueStatus("todo");
    setRuns([{ id: 1, status: "queued" }]);
    after(1400, () => setRun(1, "dispatched"));
    after(2500, () => {
      setRun(1, "running");
      setIssueStatus("in_progress");
    });
    after(5500, () => {
      setRun(1, "failed");
      setIssueStatus("todo");
      setPhase("failed");
    });
  }, [visible, cycle, after, clear, setRun]);

  const anyRunning = runs.some((r) => r.status === "running");
  useEffect(() => {
    if (!anyRunning) return;
    setNow(Date.now());
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [anyRunning]);

  const secondScript: Array<[number, () => void]> = [
    [1400, () => setRun(2, "dispatched")],
    [1100, () => {
      setRun(2, "running");
      setIssueStatus("in_progress");
    }],
    [6500, () => {
      setRun(2, "completed");
      setIssueStatus("in_review");
      setPhase("done");
    }],
  ];

  const playSecond = (from: number) => {
    const step = secondScript[from];
    if (!step) return;
    after(step[0], () => {
      step[1]();
      secondStep.current = from + 1;
      playSecond(from + 1);
    });
  };

  const retry = () => {
    setRuns((rs) => [...rs, { id: 2, status: "queued" }]);
    setPhase("second");
    secondStep.current = 0;
    playSecond(0);
  };

  const openStop = () => {
    clear();
    setPhase("confirm");
  };

  const keepRunning = () => {
    setPhase("second");
    playSecond(secondStep.current);
  };

  const stopRun = () => {
    setRun(2, "cancelled");
    setPhase("cancelled");
  };

  const replay = () => {
    clear();
    secondStep.current = 0;
    setRuns([]);
    setIssueStatus("todo");
    setPhase("idle");
    setCycle((c) => c + 1);
  };

  const active = runs.filter((r) => ACTIVE.includes(r.status));
  const past = runs.filter((r) => !ACTIVE.includes(r.status)).reverse();
  const second = runs.find((r) => r.id === 2);
  const first = runs.find((r) => r.id === 1);

  const shownPhase: Phase = visible ? phase : "idle";
  let caption: string = t.captions.idle;
  if (shownPhase === "first" && first) {
    caption = first.status === "queued" ? t.captions.queued : first.status === "dispatched" ? t.captions.dispatched : t.captions.running;
  } else if (shownPhase === "second") {
    caption = second?.status === "running" ? t.captions.retryRunning : t.captions.retryQueued;
  } else if (shownPhase !== "idle" && shownPhase !== "first") {
    caption = t.captions[shownPhase];
  }

  const liveRun = active[0];

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="relative grid min-h-80 gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_15rem]">
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <span className="flex items-center gap-2 text-body">
              <span className="text-caption text-muted-foreground">{t.statusLabel}</span>
              <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", issueDot[issueStatus])} />
              <span key={issueStatus} className="motion-safe:animate-in motion-safe:fade-in">
                {t.issueStatus[issueStatus]}
              </span>
            </span>
            {liveRun && (
              <span className="inline-flex h-6 items-center gap-1.5 rounded-full border px-2 text-caption motion-safe:animate-in motion-safe:fade-in">
                {liveRun.status === "running" ? (
                  <Loader2 aria-hidden className="size-3 text-info motion-safe:animate-spin" />
                ) : (
                  <span aria-hidden className="size-1.5 rounded-full bg-warning" />
                )}
                {liveRun.status === "running" ? t.isWorking(t.agent) : t.isQueued(t.agent)}
              </span>
            )}
          </div>
          <DemoComment author={t.you}>{t.request}</DemoComment>
          {phase === "done" && (
            <DemoComment author={t.agent} agent>
              {t.reply}
            </DemoComment>
          )}
        </div>

        <section aria-label={t.section} className="min-w-0 rounded-lg border bg-background p-2 sm:self-start">
          <div className="mb-1 flex items-center gap-1 px-1">
            <span className="text-caption font-medium">{t.section}</span>
            <ChevronRight aria-hidden className="size-3 rotate-90 text-muted-foreground" />
            {active.length > 0 && (
              <span className="ml-auto inline-flex items-center gap-1 text-info">
                <span aria-hidden className="size-1.5 animate-pulse rounded-full bg-info" />
                <span className="font-mono text-caption tabular-nums">{active.length}</span>
              </span>
            )}
          </div>

          {active.map((run) => (
            <div
              key={run.id}
              className="flex min-w-0 items-center gap-2 rounded-xs px-1 py-1.5 motion-safe:animate-in motion-safe:fade-in"
            >
              <DemoAvatar name={t.agent} agent />
              <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{t.trigger}</span>
              <span className={cn("shrink-0 text-caption tabular-nums", activeTone[run.status] ?? "text-info")}>
                {run.status === "running"
                  ? `${Math.max(0, Math.round((now - (run.runningSince ?? now)) / 1000))}s`
                  : t.runStatus[run.status]}
                {run.status === "running" && <span className="sr-only">{t.runStatus.running}</span>}
              </span>
              <button
                type="button"
                aria-label={t.cancel}
                title={t.cancel}
                disabled={run.id !== 2 || phase !== "second"}
                onClick={openStop}
                className="flex shrink-0 items-center justify-center rounded-xs p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
              >
                <Square className="size-3.5" />
              </button>
            </div>
          ))}

          {active.length > 0 && past.length > 0 && <div className="my-1.5 border-t border-border/60" />}

          {past.length > 0 && (
            <>
              <div className="px-1 pt-1 pb-0.5 text-micro text-muted-foreground">{t.latest}</div>
              {past.map((run) => (
                <div
                  key={run.id}
                  className="flex min-w-0 items-start gap-2 rounded-xs px-1 py-1.5 motion-safe:animate-in motion-safe:fade-in"
                >
                  <span className="mt-1 flex shrink-0">
                    <DemoAvatar name={t.agent} agent />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex min-h-7 min-w-0 items-center gap-2">
                      <span className="min-w-0 flex-1 truncate text-caption">{t.trigger}</span>
                      <span title={t.runStatus[run.status]} className="flex shrink-0 items-center">
                        {run.status === "completed" && <CheckCircle2 aria-hidden className="size-3.5 text-success" />}
                        {run.status === "failed" && <XCircle aria-hidden className="size-3.5 text-destructive" />}
                        {run.status === "cancelled" && <Ban aria-hidden className="size-3.5 text-muted-foreground" />}
                        <span className="sr-only">{t.runStatus[run.status]}</span>
                      </span>
                      {run.status !== "completed" && (
                        <CoachMark
                          quiet
                          active={shownPhase === "failed" && run.id === 1}
                          ringClassName="rounded-md"
                        >
                          <button
                            type="button"
                            aria-label={t.retry}
                            title={t.retry}
                            disabled={!(phase === "failed" && run.id === 1)}
                            onClick={retry}
                            className="flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent"
                          >
                            <RotateCcw className="size-3.5" />
                          </button>
                        </CoachMark>
                      )}
                    </div>
                    <p className="-mt-1 truncate text-micro text-muted-foreground">
                      {t.agent}
                      <span className="text-faint-foreground"> · </span>
                      {run.status === "failed" ? (
                        <span className="text-destructive">{t.failedReason}</span>
                      ) : run.status === "cancelled" ? (
                        t.cancelledBy
                      ) : (
                        t.duration
                      )}
                    </p>
                  </div>
                </div>
              ))}
            </>
          )}
        </section>

        {phase === "confirm" && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-background/60 p-4 backdrop-blur-[2px] motion-safe:animate-in motion-safe:fade-in">
            <div
              role="dialog"
              aria-modal="false"
              aria-labelledby="run-lifecycle-demo-title"
              className="w-full max-w-sm rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:zoom-in-95"
            >
              <div className="p-4 pb-3">
                <div id="run-lifecycle-demo-title" className="text-title-sm font-medium">
                  {t.dialogTitle}
                </div>
                <div className="mt-1 text-body text-muted-foreground">{t.dialogBody}</div>
              </div>
              <div className="flex justify-end gap-2 rounded-b-xl border-t bg-muted/40 px-4 py-3">
                <Button type="button" variant="outline" size="sm" onClick={keepRunning}>
                  {t.keep}
                </Button>
                <Button type="button" variant="destructive" size="sm" onClick={stopRun}>
                  {t.stop}
                </Button>
              </div>
            </div>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
