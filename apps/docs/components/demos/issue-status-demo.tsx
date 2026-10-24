"use client";

import { useRef, useState } from "react";
import { ChevronDown } from "lucide-react";
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

type Status = "backlog" | "todo" | "in_progress" | "in_review" | "blocked" | "done" | "cancelled";
type Category = "unstarted" | "started" | "done" | "closed";

const CATEGORIES: { key: Category; statuses: Status[] }[] = [
  { key: "unstarted", statuses: ["backlog", "todo"] },
  { key: "started", statuses: ["in_progress", "in_review", "blocked"] },
  { key: "done", statuses: ["done"] },
  { key: "closed", statuses: ["cancelled"] },
];

const MENU_ORDER: Status[] = ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"];

const categoryOf = (status: Status) => CATEGORIES.find((c) => c.statuses.includes(status))!.key;

const statusDot: Record<Status, string> = {
  backlog: "border-dashed border-muted-foreground",
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
  blocked: "border-destructive bg-destructive/30",
  done: "border-success bg-success",
  cancelled: "border-muted-foreground bg-muted-foreground/50",
};

const copy = {
  en: {
    title: "OPS-21 · Draft next quarter's team outing plan",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    agent: "Ops helper",
    you: "You",
    status: {
      backlog: "Backlog",
      todo: "Todo",
      in_progress: "In Progress",
      in_review: "In Review",
      blocked: "Blocked",
      done: "Done",
      cancelled: "Cancelled",
    } as Record<Status, string>,
    category: { unstarted: "Unstarted", started: "Started", done: "Done", closed: "Closed" } as Record<Category, string>,
    meaning: {
      backlog: "Parked. Assigning an issue here never starts an agent run.",
      todo: "Queued. Moving an issue here starts the assigned agent.",
      in_progress: "Actively being worked on.",
      in_review: "Delivered, waiting on human review.",
      blocked: "Stalled on an external dependency.",
      done: "Completed.",
      cancelled: "Decided not to do.",
    } as Record<Status, string>,
    note: {
      backlog: "Ops helper is already the assignee, but it waits while the issue is here.",
      todo: "A run is created for Ops helper.",
      in_progress: "The agent set this as it started work.",
      in_review: "The run has finished; the issue has not. Whether the work is done is decided by the status.",
      blocked: "",
      done: "A terminal status: the work finished as planned.",
      cancelled: "A terminal status, not counted as finished well. The discussion and results stay readable.",
    } as Record<Status, string>,
    changed: (from: string, to: string) => `changed status from ${from} to ${to}`,
    queued: "Queued",
    running: "Running",
    completed: "Completed",
    steps: [
      "Read the issue description and discussion",
      "Set the status to In Progress",
      "Compare two venues on budget, distance and capacity",
      "Draft a one-day schedule",
      "Set the status to In Review",
    ],
    reply: "The outing plan is ready: two venue options with budgets and a one-day schedule. Please review.",
    captions: {
      idle: "An issue assigned to an agent, parked in Backlog.",
      backlog: "Backlog parks the issue: the assignee is Ops helper, but it does not start. Click Status.",
      pick: "Move it to Todo.",
      running: "Leaving Backlog starts the assigned agent. It moves the issue to In Progress as it begins.",
      review: "Delivered: the agent set In Review. Check the result, then click Status.",
      finish: "Pick Done if the plan works. Cancelled also ends the issue, but not as finished.",
      done: "Done is terminal. Use Replay to try Cancelled instead.",
      cancelled: "Cancelled ends the issue without counting it as finished. Use Replay to watch again.",
    },
  },
  zh: {
    title: "OPS-21 · 整理下季度团建方案",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    agent: "运营助手",
    you: "你",
    status: {
      backlog: "待规划",
      todo: "待办",
      in_progress: "进行中",
      in_review: "审核中",
      blocked: "已阻塞",
      done: "已完成",
      cancelled: "已取消",
    } as Record<Status, string>,
    category: { unstarted: "未开始", started: "已开始", done: "已完成", closed: "已关闭" } as Record<Category, string>,
    meaning: {
      backlog: "搁置。把任务移到这里不会启动智能体。",
      todo: "排队中。把任务移到这里会启动指派的智能体。",
      in_progress: "正在进行。",
      in_review: "已交付，等待人工审核。",
      blocked: "被外部依赖阻塞。",
      done: "已完成。",
      cancelled: "决定不做。",
    } as Record<Status, string>,
    note: {
      backlog: "负责人已经是运营助手，但任务停在这里时它不会开始。",
      todo: "为运营助手创建了一次运行。",
      in_progress: "智能体开始工作时自己改成了进行中。",
      in_review: "运行已经结束，任务还没有。任务是否完成，以状态为准。",
      blocked: "",
      done: "终态，工作顺利完成。",
      cancelled: "终态，但不算顺利完成。讨论和结果仍可查阅。",
    } as Record<Status, string>,
    changed: (from: string, to: string) => `状态从 ${from} 改为 ${to}`,
    queued: "排队中",
    running: "运行中",
    completed: "已完成",
    steps: [
      "读取任务描述和讨论",
      "把状态改为 进行中",
      "比较两个场地的预算、路程和容纳人数",
      "排出一天的日程",
      "把状态改为 审核中",
    ],
    reply: "团建方案已整理好：两个备选场地和各自预算，附一天的日程，请检查。",
    captions: {
      idle: "一条已分配给智能体、停在待规划的任务。",
      backlog: "待规划会把任务搁置：负责人是运营助手，但它不会开始。点击状态。",
      pick: "把任务移到待办。",
      running: "离开待规划后，被分配的智能体开始执行，并在开始时把任务改为进行中。",
      review: "交付完成，智能体把状态改成了审核中。检查结果后点击状态。",
      finish: "方案可用就选已完成。已取消同样会结束任务，但不算完成。",
      done: "已完成是终态。点右上角重新演示，可以改选已取消。",
      cancelled: "已取消结束了任务，但不计为完成。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-21 · 来期のチームイベント案をまとめる",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    agent: "運用アシスタント",
    you: "あなた",
    status: {
      backlog: "バックログ",
      todo: "未着手",
      in_progress: "進行中",
      in_review: "レビュー中",
      blocked: "ブロック中",
      done: "完了",
      cancelled: "キャンセル済み",
    } as Record<Status, string>,
    category: { unstarted: "未着手", started: "開始済み", done: "完了", closed: "終了" } as Record<Category, string>,
    meaning: {
      backlog: "保留。ここに移動してもエージェントは起動しません。",
      todo: "待機中。ここに移動すると担当エージェントが起動します。",
      in_progress: "作業中。",
      in_review: "納品済み、レビュー待ち。",
      blocked: "外部依存でブロック中。",
      done: "完了。",
      cancelled: "見送り。",
    } as Record<Status, string>,
    note: {
      backlog: "担当者はすでに運用アシスタントですが、タスクがここにある間は始めません。",
      todo: "運用アシスタントの実行が作成されました。",
      in_progress: "エージェントが作業を始めるときに自分で進行中に変えました。",
      in_review: "実行は終わりましたが、タスクはまだ終わっていません。完了したかどうかはステータスで決まります。",
      blocked: "",
      done: "終了状態。作業は予定どおり完了しました。",
      cancelled: "終了状態ですが、完了扱いにはなりません。議論と成果はそのまま読めます。",
    } as Record<Status, string>,
    changed: (from: string, to: string) => `ステータスを ${from} から ${to} に変更しました`,
    queued: "待機中",
    running: "実行中",
    completed: "完了",
    steps: [
      "タスクの説明と議論を読む",
      "ステータスを 進行中 に変更",
      "2 つの会場を予算、距離、収容人数で比べる",
      "1 日のスケジュール案を作る",
      "ステータスを レビュー中 に変更",
    ],
    reply: "イベント案をまとめました。会場の候補 2 つとそれぞれの予算、1 日のスケジュールです。確認をお願いします。",
    captions: {
      idle: "エージェントに割り当て済みで、バックログに置かれたタスク。",
      backlog: "バックログはタスクを保留にします。担当者は運用アシスタントですが、作業は始まりません。ステータスをクリックしてください。",
      pick: "未着手に移動してください。",
      running: "バックログを出ると、担当エージェントが動き出し、始めるときにタスクを進行中に変えます。",
      review: "納品されました。エージェントがレビュー中に変えています。結果を確認したら、ステータスをクリックしてください。",
      finish: "案に問題がなければ完了を選びます。キャンセル済みでもタスクは終わりますが、完了扱いにはなりません。",
      done: "完了は終了状態です。右上から最初に戻すと、キャンセル済みも試せます。",
      cancelled: "キャンセル済みでタスクは終わりましたが、完了には数えません。右上から最初に戻せます。",
    },
  },
  ko: {
    title: "OPS-21 · 다음 분기 팀 워크숍 계획 정리",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    agent: "운영 도우미",
    you: "나",
    status: {
      backlog: "백로그",
      todo: "할 일",
      in_progress: "진행 중",
      in_review: "리뷰 중",
      blocked: "막힘",
      done: "완료",
      cancelled: "취소됨",
    } as Record<Status, string>,
    category: { unstarted: "시작 전", started: "시작됨", done: "완료", closed: "종료" } as Record<Category, string>,
    meaning: {
      backlog: "보류. 여기로 옮겨도 에이전트가 시작되지 않습니다.",
      todo: "대기 중. 여기로 옮기면 담당 에이전트가 시작됩니다.",
      in_progress: "진행 중.",
      in_review: "전달 완료, 리뷰 대기.",
      blocked: "외부 의존성으로 중단됨.",
      done: "완료됨.",
      cancelled: "진행하지 않기로 함.",
    } as Record<Status, string>,
    note: {
      backlog: "담당자는 이미 운영 도우미지만, 태스크가 여기 있는 동안에는 시작하지 않습니다.",
      todo: "운영 도우미의 실행이 생성되었습니다.",
      in_progress: "에이전트가 작업을 시작하면서 직접 진행 중으로 바꿨습니다.",
      in_review: "실행은 끝났지만 태스크는 아직 끝나지 않았습니다. 완료 여부는 상태로 판단합니다.",
      blocked: "",
      done: "종료 상태입니다. 작업이 계획대로 끝났습니다.",
      cancelled: "종료 상태지만 완료로 보지 않습니다. 논의와 결과는 그대로 볼 수 있습니다.",
    } as Record<Status, string>,
    changed: (from: string, to: string) => `상태를 ${from}에서 ${to}로 변경했습니다`,
    queued: "대기 중",
    running: "실행 중",
    completed: "완료됨",
    steps: [
      "태스크 설명과 논의 읽기",
      "상태를 진행 중으로 변경",
      "장소 두 곳을 예산, 거리, 수용 인원으로 비교",
      "하루 일정 초안 작성",
      "상태를 리뷰 중으로 변경",
    ],
    reply: "워크숍 계획을 정리했습니다. 장소 후보 두 곳과 각각의 예산, 하루 일정입니다. 확인 부탁드립니다.",
    captions: {
      idle: "에이전트에게 할당된 채 백로그에 있는 태스크입니다.",
      backlog: "백로그는 태스크를 보류합니다. 담당자는 운영 도우미지만 시작하지 않습니다. 상태를 클릭하세요.",
      pick: "할 일로 옮기세요.",
      running: "백로그에서 나오면 담당 에이전트가 시작하고, 시작하면서 태스크를 진행 중으로 바꿉니다.",
      review: "전달이 끝나 에이전트가 리뷰 중으로 바꿨습니다. 결과를 확인한 뒤 상태를 클릭하세요.",
      finish: "계획이 괜찮으면 완료를 선택하세요. 취소됨도 태스크를 끝내지만 완료로 보지 않습니다.",
      done: "완료는 종료 상태입니다. 오른쪽 위에서 다시 시작하면 취소됨도 선택해 볼 수 있습니다.",
      cancelled: "취소됨으로 태스크는 끝났지만 완료로 세지 않습니다. 오른쪽 위에서 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "OPS-21 · Préparer la sortie d'équipe du prochain trimestre",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    agent: "Assistant ops",
    you: "Vous",
    status: {
      backlog: "À planifier",
      todo: "À faire",
      in_progress: "En cours",
      in_review: "En relecture",
      blocked: "Bloqué",
      done: "Terminé",
      cancelled: "Annulé",
    } as Record<Status, string>,
    category: { unstarted: "Non démarré", started: "Démarrage", done: "Terminé", closed: "Fermée" } as Record<Category, string>,
    meaning: {
      backlog: "En attente. Affecter une tâche ici ne démarre jamais un agent.",
      todo: "En file. Déplacer une tâche ici démarre l'agent qui lui est affecté.",
      in_progress: "En cours de traitement.",
      in_review: "Livré, en attente d'une relecture humaine.",
      blocked: "À l'arrêt, en attente d'une dépendance extérieure.",
      done: "Terminé.",
      cancelled: "Abandonné.",
    } as Record<Status, string>,
    note: {
      backlog: "Assistant ops est déjà assigné, mais il attend tant que la tâche reste ici.",
      todo: "Une exécution est créée pour Assistant ops.",
      in_progress: "L'agent a choisi ce statut en commençant le travail.",
      in_review: "L'exécution est terminée, pas la tâche. C'est le statut qui dit si le travail est fini.",
      blocked: "",
      done: "Statut final : le travail s'est terminé comme prévu.",
      cancelled: "Statut final, sans compter comme terminé. La discussion et les résultats restent consultables.",
    } as Record<Status, string>,
    changed: (from: string, to: string) => `a changé le statut de ${from} à ${to}`,
    queued: "En file d'attente",
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire la description et la discussion de la tâche",
      "Passer le statut à En cours",
      "Comparer deux lieux selon le budget, la distance et la capacité",
      "Rédiger un programme sur une journée",
      "Passer le statut à En relecture",
    ],
    reply: "Le plan de la sortie est prêt : deux lieux possibles avec leur budget et un programme sur une journée. Merci de relire.",
    captions: {
      idle: "Une tâche assignée à un agent, mise de côté dans À planifier.",
      backlog: "À planifier met la tâche de côté : Assistant ops est assigné, mais ne démarre pas. Cliquez sur Statut.",
      pick: "Passez-la à À faire.",
      running: "En quittant À planifier, la tâche démarre l'agent assigné, qui la passe à En cours en commençant.",
      review: "Livré : l'agent a choisi En relecture. Vérifiez le résultat, puis cliquez sur Statut.",
      finish: "Choisissez Terminé si le plan convient. Annulé clôt aussi la tâche, mais sans la compter comme terminée.",
      done: "Terminé est un statut final. Relancez la démo pour essayer Annulé.",
      cancelled: "Annulé clôt la tâche sans la compter comme terminée. Relancez la démo pour la revoir.",
    },
  },
};

type Phase = "idle" | "backlog" | "pick" | "running" | "review" | "finish" | "done" | "cancelled";

type Activity = { actor: string; agent: boolean; from: Status; to: Status };

// Run step index (1-based) at which the agent updates the status.
const STATUS_STEPS: Partial<Record<number, Status>> = { 2: "in_progress", 5: "in_review" };

export function IssueStatusDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("backlog");
  const [status, setStatus] = useState<Status>("backlog");
  const [activity, setActivity] = useState<Activity[]>([]);
  const [queued, setQueued] = useState(true);
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";
  const menuOpen = phase === "pick" || phase === "finish";
  // The one status each open menu accepts; the finish menu offers two endings.
  const choices: Status[] = phase === "pick" ? ["todo"] : phase === "finish" ? ["done", "cancelled"] : [];

  const statusRef = useRef<Status>("backlog");
  const move = (to: Status, actor: string, agent: boolean) => {
    const from = statusRef.current;
    statusRef.current = to;
    setStatus(to);
    setActivity((items) => [...items, { actor, agent, from, to }]);
  };

  const choose = (to: Status) => {
    move(to, t.you, false);
    if (to === "todo") {
      setPhase("running");
      after(900, () => setQueued(false));
      t.steps.forEach((_, i) =>
        after(900 + i * 1000, () => {
          setStepsShown(i + 1);
          const next = STATUS_STEPS[i + 1];
          if (next) move(next, t.agent, true);
        }),
      );
      after(900 + t.steps.length * 1000 + 500, () => setPhase("review"));
    } else {
      setPhase(to === "done" ? "done" : "cancelled");
    }
  };

  const replay = () => {
    clear();
    setPhase("backlog");
    statusRef.current = "backlog";
    setStatus("backlog");
    setActivity([]);
    setQueued(true);
    setStepsShown(0);
  };

  const statusEnabled = visible && (phase === "backlog" || phase === "review" || menuOpen);
  const coachOnStatus = visible && (phase === "backlog" || phase === "review");
  const runActive = phase === "running";
  const showRun = phase !== "backlog" && phase !== "pick";

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-96 flex-col gap-3 p-4">
        <ol className="flex flex-wrap gap-2">
          {CATEGORIES.map((group) => (
            <li
              key={group.key}
              className={cn(
                "m-0 max-w-full flex-none rounded-lg border px-2 py-1.5 transition-colors",
                categoryOf(status) === group.key ? "border-brand/40 bg-brand/5" : "bg-background",
              )}
            >
              <div className="flex items-baseline gap-1.5">
                <span className="text-micro font-medium">{t.category[group.key]}</span>
                <code className="text-micro text-muted-foreground">{group.key}</code>
              </div>
              <div className="mt-1 flex flex-wrap gap-1">
                {group.statuses.map((s) => (
                  <span
                    key={s}
                    className={cn(
                      "inline-flex items-center gap-1 rounded-sm px-1 py-0.5 text-micro transition-colors",
                      s === status ? "bg-foreground text-background" : "text-muted-foreground",
                    )}
                  >
                    <span aria-hidden className={cn("size-2 rounded-full border", s === status ? "border-background" : statusDot[s])} />
                    {t.status[s]}
                  </span>
                ))}
              </div>
            </li>
          ))}
        </ol>

        <dl className="grid w-fit grid-cols-[auto_auto] items-center gap-x-6 gap-y-2 text-body">
          <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
          <dd className="relative">
            <CoachMark
              active={coachOnStatus}
              side="right"
              ringClassName="rounded-md"
            >
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={menuOpen}
                disabled={!statusEnabled}
                onClick={() => {
                  if (phase === "backlog") setPhase("pick");
                  else if (phase === "pick") setPhase("backlog");
                  else if (phase === "review") setPhase("finish");
                  else if (phase === "finish") setPhase("review");
                }}
                className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-muted disabled:hover:bg-transparent"
              >
                <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", statusDot[status])} />
                <span key={status} className="motion-safe:animate-in motion-safe:fade-in">
                  {t.status[status]}
                </span>
                {statusEnabled && <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />}
              </button>
            </CoachMark>
            {menuOpen && (
              <div
                role="listbox"
                aria-label={t.statusLabel}
                className="absolute top-full left-0 z-10 mt-1 w-44 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                {MENU_ORDER.map((s) => {
                  const enabled = choices.includes(s);
                  return (
                    <CoachMark
                      quiet
                      key={s}
                      active={s === choices[0]}
                      ringClassName="rounded-md"
                      className="flex w-full"
                    >
                      <button
                        type="button"
                        role="option"
                        aria-selected={s === status}
                        disabled={!enabled}
                        onClick={() => choose(s)}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-body hover:bg-accent disabled:opacity-45 disabled:hover:bg-transparent"
                      >
                        <span aria-hidden className={cn("size-3 shrink-0 rounded-full border-2", statusDot[s])} />
                        <span className="min-w-0 flex-1 truncate">{t.status[s]}</span>
                        {s === status && <span className="text-caption text-muted-foreground">✓</span>}
                      </button>
                    </CoachMark>
                  );
                })}
              </div>
            )}
          </dd>
          <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
          <dd className="flex items-center gap-1.5">
            <DemoAvatar name={t.agent} agent />
            {t.agent}
          </dd>
        </dl>

        <div
          key={status}
          className="rounded-lg border bg-muted/40 px-3 py-2 motion-safe:animate-in motion-safe:fade-in"
        >
          <div className="flex flex-wrap items-baseline gap-x-2 text-label">
            <span className="font-medium">{t.status[status]}</span>
            <code className="text-micro text-muted-foreground">{status}</code>
            <span className="text-caption text-muted-foreground">· {t.category[categoryOf(status)]}</span>
          </div>
          <p className="mt-0.5 text-caption">{t.meaning[status]}</p>
          {t.note[status] && <p className="mt-0.5 text-caption text-muted-foreground">{t.note[status]}</p>}
        </div>

        {activity.length > 0 && (
          <ul className="flex flex-col gap-1">
            {activity.map((item, i) => (
              <li
                key={i}
                className="m-0 flex min-w-0 items-center gap-1.5 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in"
              >
                <DemoAvatar name={item.actor} agent={item.agent} />
                <span className="font-medium text-foreground">{item.actor}</span>
                <span className="min-w-0 truncate">{t.changed(t.status[item.from], t.status[item.to])}</span>
              </li>
            ))}
          </ul>
        )}

        {showRun && (
          <DemoRun
            agent={t.agent}
            status={runActive ? (queued ? t.queued : t.running) : t.completed}
            active={runActive}
          >
            {t.steps.slice(0, stepsShown).map((step, i) => (
              <DemoStep key={step} state={i === stepsShown - 1 && runActive ? "running" : "done"}>
                {step}
              </DemoStep>
            ))}
          </DemoRun>
        )}

        {(phase === "review" || phase === "finish" || phase === "done" || phase === "cancelled") && (
          <DemoComment author={t.agent} agent>
            {t.reply}
          </DemoComment>
        )}
      </div>
    </DemoFrame>
  );
}
