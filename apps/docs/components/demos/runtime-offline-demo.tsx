"use client";

import { useEffect, useState } from "react";
import { Laptop, Loader2, Power } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  DemoRun,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type IssueStatus = "todo" | "in_progress" | "in_review";
type Machine = "offline" | "connecting" | "online";

const copy = {
  en: {
    title: "OPS-23 · Collect this month's supply requests from every store",
    agent: "Ops helper",
    statusLabel: "Status",
    issueStatus: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<IssueStatus, string>,
    isQueued: (name: string) => `${name} is queued`,
    queuedFor: "queued for 42m",
    runtimesTitle: "Runtimes",
    office: "Office computer",
    lei: "Lei Li's computer",
    tool: "Claude Code",
    boundAgent: "Ops helper runs here",
    online: "Online",
    offline: "Offline",
    connecting: "Connecting to workspace...",
    bringOnline: "Bring this computer online",
    bringOnlineHint: "Same as opening Multica Desktop or running multica daemon start",
    queued: "Queued",
    starting: "Starting",
    working: "Working",
    completed: "Completed",
    steps: [
      "Read the issue description and discussion",
      "Set the status to In Progress",
      "Open the supply request sheets from 12 stores",
      "Total the requests by item and flag unusual quantities",
      "Set the status to In Review",
    ],
    reply: "The summary is ready: 12 stores, 37 items, and 3 unusually large requests marked for a second look. Please review.",
    captions: {
      idle: "You assigned the issue to Ops helper, whose runtime is on the office computer.",
      waiting:
        "The office computer is offline, so the run waits in the queue. Lei Li's computer is online, but the run does not move there. Click \"Bring this computer online\".",
      connecting: "The daemon starts and registers the computer's runtimes again.",
      claimed: "Back online, the runtime claims the queued run and starts the AI coding tool.",
      running: "Ops helper is working on the office computer.",
      done: "The run completed and the issue moved to In Review. Use Replay to watch again.",
    },
  },
  zh: {
    title: "OPS-23 · 汇总本月各门店的物料申请",
    agent: "运营助手",
    statusLabel: "状态",
    issueStatus: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<IssueStatus, string>,
    isQueued: (name: string) => `${name} 排队中`,
    queuedFor: "已排队 42m",
    runtimesTitle: "运行时",
    office: "办公室电脑",
    lei: "李雷的电脑",
    tool: "Claude Code",
    boundAgent: "运营助手 在这里运行",
    online: "在线",
    offline: "离线",
    connecting: "正在连接工作区...",
    bringOnline: "让这台电脑上线",
    bringOnlineHint: "相当于打开 Multica Desktop，或运行 multica daemon start",
    queued: "排队中",
    starting: "启动中",
    working: "正在工作",
    completed: "已完成",
    steps: [
      "读取任务描述和讨论",
      "把状态改为 进行中",
      "打开 12 家门店的物料申请表",
      "按物料汇总数量，标出异常的申请",
      "把状态改为 审核中",
    ],
    reply: "汇总已完成：12 家门店共 37 种物料，其中 3 笔申请量明显偏大，已标出待复核。请检查。",
    captions: {
      idle: "你把任务分配给了 运营助手，它的运行时在办公室电脑上。",
      waiting: "办公室电脑离线，运行留在队列里等待。李雷的电脑在线，运行也不会转过去。点击\"让这台电脑上线\"。",
      connecting: "守护进程启动，重新注册这台电脑的运行时。",
      claimed: "运行时上线后领取了排队的运行，开始启动 AI 编程工具。",
      running: "运营助手 正在办公室电脑上工作。",
      done: "运行完成，任务改为审核中。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-23 · 今月の各店舗の備品申請を集計する",
    agent: "運用アシスタント",
    statusLabel: "ステータス",
    issueStatus: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<IssueStatus, string>,
    isQueued: (name: string) => `${name} が待機中`,
    queuedFor: "42m 待機中",
    runtimesTitle: "ランタイム",
    office: "オフィスのパソコン",
    lei: "李雷のパソコン",
    tool: "Claude Code",
    boundAgent: "運用アシスタント はここで動きます",
    online: "オンライン",
    offline: "オフライン",
    connecting: "ワークスペースに接続中...",
    bringOnline: "このパソコンをオンラインにする",
    bringOnlineHint: "Multica Desktop を開くか、multica daemon start を実行するのと同じです",
    queued: "待機中",
    starting: "開始中",
    working: "作業中",
    completed: "完了",
    steps: [
      "タスクの説明と議論を読む",
      "ステータスを 進行中 にする",
      "12 店舗の備品申請シートを開く",
      "品目ごとに数量を集計し、異常な申請に印を付ける",
      "ステータスを レビュー中 にする",
    ],
    reply: "集計が終わりました。12 店舗で 37 品目、数量が明らかに多い申請 3 件に再確認の印を付けています。確認をお願いします。",
    captions: {
      idle: "タスクを 運用アシスタント に割り当てました。そのランタイムはオフィスのパソコンにあります。",
      waiting:
        "オフィスのパソコンがオフラインなので、実行はキューで待ちます。李雷のパソコンはオンラインですが、実行はそちらへ移りません。「このパソコンをオンラインにする」をクリックしてください。",
      connecting: "デーモンが起動し、このパソコンのランタイムを登録し直します。",
      claimed: "オンラインに戻ったランタイムが待機中の実行を受け取り、AI コーディングツールを起動します。",
      running: "運用アシスタント がオフィスのパソコンで作業しています。",
      done: "実行が完了し、タスクはレビュー中になりました。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "OPS-23 · 이번 달 매장별 비품 신청 취합",
    agent: "운영 도우미",
    statusLabel: "상태",
    issueStatus: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<IssueStatus, string>,
    isQueued: (name: string) => `${name} 대기 중`,
    queuedFor: "42m 동안 대기 중",
    runtimesTitle: "런타임",
    office: "사무실 컴퓨터",
    lei: "리레이의 컴퓨터",
    tool: "Claude Code",
    boundAgent: "운영 도우미가 여기서 실행됨",
    online: "온라인",
    offline: "오프라인",
    connecting: "워크스페이스에 연결 중...",
    bringOnline: "이 컴퓨터를 온라인으로",
    bringOnlineHint: "Multica Desktop을 열거나 multica daemon start를 실행하는 것과 같습니다",
    queued: "대기 중",
    starting: "시작 중",
    working: "작업 중",
    completed: "완료됨",
    steps: [
      "태스크 설명과 논의 읽기",
      "상태를 진행 중으로 변경",
      "매장 12곳의 비품 신청서 열기",
      "품목별로 수량을 합산하고 이상한 신청 표시",
      "상태를 리뷰 중으로 변경",
    ],
    reply: "취합을 마쳤습니다. 매장 12곳, 품목 37개이며 수량이 눈에 띄게 많은 신청 3건은 재확인 대상으로 표시했습니다. 확인해 주세요.",
    captions: {
      idle: "태스크를 운영 도우미에게 할당했습니다. 이 에이전트의 런타임은 사무실 컴퓨터에 있습니다.",
      waiting:
        "사무실 컴퓨터가 오프라인이라 실행은 대기열에서 기다립니다. 리레이의 컴퓨터는 온라인이지만 실행이 그쪽으로 넘어가지 않습니다. \"이 컴퓨터를 온라인으로\"를 클릭하세요.",
      connecting: "데몬이 시작되면서 이 컴퓨터의 런타임을 다시 등록합니다.",
      claimed: "다시 온라인이 된 런타임이 대기 중인 실행을 가져가 AI 코딩 도구를 시작합니다.",
      running: "운영 도우미가 사무실 컴퓨터에서 작업하고 있습니다.",
      done: "실행이 완료되어 태스크가 리뷰 중으로 바뀌었습니다. \"다시 보기\"로 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "OPS-23 · Rassembler les demandes de fournitures du mois de chaque magasin",
    agent: "Assistant ops",
    statusLabel: "Statut",
    issueStatus: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<IssueStatus, string>,
    isQueued: (name: string) => `${name} est en file d'attente`,
    queuedFor: "en file d'attente depuis 42m",
    runtimesTitle: "Runtimes",
    office: "Ordinateur du bureau",
    lei: "Ordinateur de Lei Li",
    tool: "Claude Code",
    boundAgent: "Assistant ops s'exécute ici",
    online: "En ligne",
    offline: "Hors ligne",
    connecting: "Connexion à l’espace de travail...",
    bringOnline: "Mettre cet ordinateur en ligne",
    bringOnlineHint: "Revient à ouvrir Multica Desktop ou à lancer multica daemon start",
    queued: "En file d'attente",
    starting: "Démarrage",
    working: "Au travail",
    completed: "Terminée",
    steps: [
      "Lire la description et la discussion de la tâche",
      "Passer le statut à En cours",
      "Ouvrir les fiches de demande de fournitures des 12 magasins",
      "Totaliser les demandes par article et signaler les quantités inhabituelles",
      "Passer le statut à En relecture",
    ],
    reply:
      "La synthèse est prête : 12 magasins, 37 articles, et 3 demandes anormalement élevées marquées pour une seconde vérification. Merci de relire.",
    captions: {
      idle: "Vous avez assigné la tâche à Assistant ops, dont le runtime se trouve sur l'ordinateur du bureau.",
      waiting:
        "L'ordinateur du bureau est hors ligne, l'exécution attend donc dans la file. L'ordinateur de Lei Li est en ligne, mais l'exécution n'y est pas transférée. Cliquez sur « Mettre cet ordinateur en ligne ».",
      connecting: "Le daemon démarre et réenregistre les runtimes de cet ordinateur.",
      claimed: "De retour en ligne, le runtime prend l'exécution en attente et démarre l'outil de code IA.",
      running: "Assistant ops travaille sur l'ordinateur du bureau.",
      done: "L'exécution est terminée et la tâche est passée à En relecture. Rejouer recommence depuis le début.",
    },
  },
};

type Phase = "idle" | "waiting" | "connecting" | "claimed" | "running" | "done";

// Index of the step at which the agent updates the issue status.
const STATUS_STEPS: Partial<Record<number, IssueStatus>> = { 2: "in_progress", 5: "in_review" };

const issueDot: Record<IssueStatus, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

function StatusDot({ state }: { state: Machine }) {
  return (
    <span
      aria-hidden
      className={cn(
        "size-2 shrink-0 rounded-full",
        state === "online" && "bg-success",
        state === "offline" && "bg-muted-foreground/50",
        state === "connecting" && "bg-warning",
      )}
    />
  );
}

export function RuntimeOfflineDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [cycle, setCycle] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [issueStatus, setIssueStatus] = useState<IssueStatus>("todo");
  const [stepsShown, setStepsShown] = useState(0);

  useEffect(() => {
    if (!visible) return;
    clear();
    after(600, () => setPhase("waiting"));
  }, [visible, cycle, after, clear]);

  const bringOnline = () => {
    setPhase("connecting");
    after(1800, () => setPhase("claimed"));
    after(3200, () => setPhase("running"));
    t.steps.forEach((_, i) =>
      after(3200 + i * 1000, () => {
        setStepsShown(i + 1);
        const next = STATUS_STEPS[i + 1];
        if (next) setIssueStatus(next);
      }),
    );
    after(3200 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("idle");
    setIssueStatus("todo");
    setStepsShown(0);
    setCycle((c) => c + 1);
  };

  const shownPhase: Phase = visible ? phase : "idle";
  const office: Machine =
    shownPhase === "connecting" ? "connecting" : shownPhase === "idle" || shownPhase === "waiting" ? "offline" : "online";
  const queued = shownPhase === "idle" || shownPhase === "waiting" || shownPhase === "connecting";
  const runStatus =
    shownPhase === "done" ? t.completed : shownPhase === "claimed" ? t.starting : shownPhase === "running" ? t.working : t.queued;

  const machines = [
    { name: t.office, state: office, bound: true },
    { name: t.lei, state: "online" as Machine, bound: false },
  ];

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="grid min-h-80 gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_15rem]">
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <span className="flex items-center gap-2 text-body">
              <span className="text-caption text-muted-foreground">{t.statusLabel}</span>
              <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", issueDot[issueStatus])} />
              <span key={issueStatus} className="motion-safe:animate-in motion-safe:fade-in">
                {t.issueStatus[issueStatus]}
              </span>
            </span>
            {queued && (
              <span className="inline-flex h-6 min-w-0 items-center gap-1.5 rounded-full border px-2 text-caption">
                <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-warning" />
                <span className="truncate">{t.isQueued(t.agent)}</span>
                <span className="shrink-0 text-muted-foreground">· {t.queuedFor}</span>
              </span>
            )}
          </div>

          <DemoRun agent={t.agent} status={runStatus} active={shownPhase !== "done"}>
            {t.steps.slice(0, stepsShown).map((step, i) => (
              <DemoStep key={step} state={i === stepsShown - 1 && shownPhase !== "done" ? "running" : "done"}>
                {step}
              </DemoStep>
            ))}
          </DemoRun>

          {shownPhase === "done" && (
            <DemoComment author={t.agent} agent>
              {t.reply}
            </DemoComment>
          )}
        </div>

        <section aria-label={t.runtimesTitle} className="flex min-w-0 flex-col gap-2 sm:self-start">
          <div className="px-1 text-caption font-medium text-muted-foreground">{t.runtimesTitle}</div>
          {machines.map((m) => (
            <div
              key={m.name}
              className={cn(
                "min-w-0 rounded-lg border bg-background p-2.5",
                m.bound && m.state === "offline" && "border-dashed",
              )}
            >
              <div className="flex min-w-0 items-center gap-2">
                <Laptop aria-hidden className="size-4 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate text-label font-medium">{m.name}</span>
              </div>
              <div className="mt-1.5 flex min-w-0 items-center gap-2 pl-6 text-caption">
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{t.tool}</span>
                <span className="inline-flex shrink-0 items-center gap-1.5">
                  {m.state === "connecting" ? (
                    <Loader2 aria-hidden className="size-3 text-warning motion-safe:animate-spin" />
                  ) : (
                    <StatusDot state={m.state} />
                  )}
                  <span key={m.state} className="motion-safe:animate-in motion-safe:fade-in">
                    {m.state === "online" ? t.online : m.state === "offline" ? t.offline : t.connecting}
                  </span>
                </span>
              </div>
              {m.bound && (
                <div className="mt-1 flex items-center gap-1.5 pl-6 text-micro text-muted-foreground">
                  <DemoAvatar name={t.agent} agent />
                  {t.boundAgent}
                </div>
              )}
              {m.bound && (shownPhase === "idle" || shownPhase === "waiting") && (
                <div className="mt-2.5 flex flex-col items-start gap-1.5 pl-6">
                  <p className="text-micro text-muted-foreground">{t.bringOnlineHint}</p>
                  <CoachMark active={shownPhase === "waiting"} side="left" ringClassName="rounded-lg">
                    <Button type="button" size="sm" disabled={shownPhase !== "waiting"} onClick={bringOnline}>
                      <Power />
                      {t.bringOnline}
                    </Button>
                  </CoachMark>
                </div>
              )}
            </div>
          ))}
        </section>
      </div>
    </DemoFrame>
  );
}
