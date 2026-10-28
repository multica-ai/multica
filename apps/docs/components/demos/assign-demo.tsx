"use client";

import { useState } from "react";
import { ChevronDown, X } from "lucide-react";
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

type Candidate = { name: string; agent: boolean };
type Status = "todo" | "in_progress" | "in_review";

const copy = {
  en: {
    title: "OPS-14 · Put together the new-hire onboarding checklist",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    unassigned: "Unassigned",
    status: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<Status, string>,
    agentsGroup: "Agents",
    membersGroup: "Members",
    candidates: [
      { name: "Ops helper", agent: true },
      { name: "Writing helper", agent: true },
      { name: "You", agent: false },
      { name: "Lei Li", agent: false },
    ] as Candidate[],
    confirmTitle: "Confirm assignment?",
    confirmBody: (name: string) => `Assign this issue to ${name}. They'll start right away if the run conditions are met.`,
    close: "Close",
    dontStart: "Don't start yet",
    confirm: "Confirm assignment",
    queued: "Queued",
    running: "Running",
    completed: "Completed",
    steps: [
      "Read the issue description and discussion",
      "Set the status to In Progress",
      "Collect the onboarding items from IT, admin and HR",
      "Arrange them by first day and first week",
      "Set the status to In Review",
    ],
    reply: "The onboarding checklist is ready: 18 items grouped by first day and first week. Please review.",
    captions: {
      idle: "An issue with no assignee yet.",
      start: "Hand the whole issue to an agent: click Assignee.",
      pick: "Pick an agent. Choosing a member only changes the assignee.",
      confirm: "The confirmation names who will start. Confirm to start now, or keep the assignee without starting.",
      member: "Assigned to a member: the assignee changed, and no AI coding tool was called.",
      parked: "The agent is the assignee now, but no run was created. It starts once you ask it to.",
      running: "A run was created. The agent moves the issue to In Progress as it starts.",
      done: "Delivered: the agent moved the issue to In Review. Use Replay to try another path.",
    },
  },
  zh: {
    title: "OPS-14 · 整理新员工入职清单",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    unassigned: "未分配",
    status: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<Status, string>,
    agentsGroup: "智能体",
    membersGroup: "成员",
    candidates: [
      { name: "运营助手", agent: true },
      { name: "文案助手", agent: true },
      { name: "你", agent: false },
      { name: "李雷", agent: false },
    ] as Candidate[],
    confirmTitle: "确认指派？",
    confirmBody: (name: string) => `将这个任务指派给 ${name}；符合运行条件时会立即开始处理。`,
    close: "关闭",
    dontStart: "暂不开始",
    confirm: "确认指派",
    queued: "排队中",
    running: "运行中",
    completed: "已完成",
    steps: [
      "读取任务描述和讨论",
      "把状态改为 进行中",
      "汇总 IT、行政、人事三方的入职事项",
      "按入职第一天、第一周排好顺序",
      "把状态改为 审核中",
    ],
    reply: "入职清单已整理好：按第一天和第一周分组，共 18 项，请检查。",
    captions: {
      idle: "一条还没有负责人的任务。",
      start: "把整条任务交给智能体：点击负责人。",
      pick: "选择一个智能体。选择成员只会改变负责人。",
      confirm: "确认窗写明了谁会开始工作。确认后立即开始；暂不开始只保存负责人。",
      member: "负责人改成了成员：只改变负责人，不会调用 AI 编程工具。",
      parked: "负责人已经是智能体，但这次没有创建运行；之后向它提出要求时才会开始。",
      running: "已创建运行。智能体开始工作时把任务改为进行中。",
      done: "交付完成，智能体把任务改为审核中。点右上角可以换一条路径再试。",
    },
  },
  ja: {
    title: "OPS-14 · 新入社員のオンボーディングチェックリストを作る",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    unassigned: "未割り当て",
    status: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<Status, string>,
    agentsGroup: "エージェント",
    membersGroup: "メンバー",
    candidates: [
      { name: "運用アシスタント", agent: true },
      { name: "ライティングアシスタント", agent: true },
      { name: "あなた", agent: false },
      { name: "李雷", agent: false },
    ] as Candidate[],
    confirmTitle: "割り当てを確認しますか？",
    confirmBody: (name: string) =>
      `このタスクを ${name} に割り当てます。実行条件を満たす場合はすぐに作業を開始します。`,
    close: "閉じる",
    dontStart: "まだ開始しない",
    confirm: "割り当てを確認",
    queued: "待機中",
    running: "実行中",
    completed: "完了",
    steps: [
      "タスクの説明とコメントを読む",
      "ステータスを 進行中 に変更",
      "IT・総務・人事からオンボーディングの項目を集める",
      "初日と最初の 1 週間に分けて並べる",
      "ステータスを レビュー中 に変更",
    ],
    reply: "オンボーディングチェックリストができました。初日と最初の 1 週間に分けて全 18 項目です。確認をお願いします。",
    captions: {
      idle: "まだ担当者のいないタスクです。",
      start: "タスクをまるごとエージェントに任せます。担当者をクリックします。",
      pick: "エージェントを選びます。メンバーを選ぶと担当者が変わるだけです。",
      confirm:
        "確認ダイアログに、誰が作業を始めるかが書かれています。確認するとすぐに始まり、「まだ開始しない」なら担当者だけを保存します。",
      member: "担当者がメンバーに変わりました。変わったのは担当者だけで、AI コーディングツールは呼び出されていません。",
      parked: "担当者はエージェントになりましたが、今回は実行が作成されていません。あとで依頼したときに作業を始めます。",
      running: "実行が作成されました。エージェントは作業を始めるときにタスクを 進行中 にします。",
      done: "納品されました。エージェントがタスクを レビュー中 に移しました。「もう一度」で別の流れも試せます。",
    },
  },
  ko: {
    title: "OPS-14 · 신입 사원 온보딩 체크리스트 정리",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    unassigned: "담당자 없음",
    status: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<Status, string>,
    agentsGroup: "에이전트",
    membersGroup: "멤버",
    candidates: [
      { name: "운영 도우미", agent: true },
      { name: "글쓰기 도우미", agent: true },
      { name: "나", agent: false },
      { name: "리레이", agent: false },
    ] as Candidate[],
    confirmTitle: "할당을 확인할까요?",
    confirmBody: (name: string) => `이 태스크를 ${name}에게 할당합니다. 실행 조건을 충족하면 바로 시작합니다.`,
    close: "닫기",
    dontStart: "아직 시작 안 함",
    confirm: "할당 확인",
    queued: "대기 중",
    running: "실행 중",
    completed: "완료됨",
    steps: [
      "태스크 설명과 댓글 읽기",
      "상태를 진행 중으로 변경",
      "IT·총무·인사팀의 온보딩 항목 모으기",
      "첫날과 첫 주 기준으로 순서 정리",
      "상태를 리뷰 중으로 변경",
    ],
    reply: "온보딩 체크리스트를 정리했습니다. 첫날과 첫 주로 나눠 모두 18개 항목입니다. 확인해 주세요.",
    captions: {
      idle: "아직 담당자가 없는 태스크입니다.",
      start: "태스크 전체를 에이전트에게 맡기려면 담당자를 클릭하세요.",
      pick: "에이전트를 선택하세요. 멤버를 선택하면 담당자만 바뀝니다.",
      confirm:
        "확인 창에 누가 작업을 시작하는지 나옵니다. 확인하면 바로 시작하고, '아직 시작 안 함'을 고르면 담당자만 저장합니다.",
      member: "담당자가 멤버로 바뀌었습니다. 담당자만 바뀌고 AI 코딩 도구는 호출되지 않았습니다.",
      parked: "담당자는 이제 에이전트지만 이번에는 실행이 만들어지지 않았습니다. 나중에 요청하면 그때 시작합니다.",
      running: "실행이 만들어졌습니다. 에이전트가 작업을 시작하면서 태스크를 진행 중으로 바꿉니다.",
      done: "전달 완료: 에이전트가 태스크를 리뷰 중으로 옮겼습니다. '다시 보기'로 다른 경로도 해 보세요.",
    },
  },
  fr: {
    title: "OPS-14 · Préparer la checklist d'intégration des nouveaux arrivants",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    unassigned: "Non assigné",
    status: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<Status, string>,
    agentsGroup: "Agents",
    membersGroup: "Membres",
    candidates: [
      { name: "Assistant ops", agent: true },
      { name: "Assistant rédaction", agent: true },
      { name: "Vous", agent: false },
      { name: "Lei Li", agent: false },
    ] as Candidate[],
    confirmTitle: "Confirmer l'assignation ?",
    confirmBody: (name: string) =>
      `Assigner cette tâche à ${name}. Le travail démarrera immédiatement si les conditions d'exécution sont réunies.`,
    close: "Fermer",
    dontStart: "Ne pas démarrer maintenant",
    confirm: "Confirmer l'assignation",
    queued: "En file d'attente",
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire la description et la discussion de la tâche",
      "Passer le statut à En cours",
      "Rassembler les points d'intégration de l'IT, des services généraux et des RH",
      "Les classer entre premier jour et première semaine",
      "Passer le statut à En relecture",
    ],
    reply:
      "La checklist d'intégration est prête : 18 points répartis entre le premier jour et la première semaine. Merci de la relire.",
    captions: {
      idle: "Une tâche qui n'a pas encore d'assigné.",
      start: "Confiez toute la tâche à un agent : cliquez sur Assigné à.",
      pick: "Choisissez un agent. Choisir un membre change seulement l'assigné.",
      confirm:
        "La confirmation indique qui va démarrer. Confirmez pour démarrer maintenant, ou gardez l'assigné sans démarrer.",
      member: "Assignée à un membre : seul l'assigné a changé, aucun outil de codage IA n'a été appelé.",
      parked: "L'agent est maintenant l'assigné, mais aucune exécution n'a été créée. Il démarrera quand vous le lui demanderez.",
      running: "Une exécution a été créée. L'agent passe la tâche à En cours en démarrant.",
      done: "Livré : l'agent a passé la tâche à En relecture. Utilisez Rejouer pour essayer un autre chemin.",
    },
  },
};

type Phase = "idle" | "start" | "pick" | "confirm" | "member" | "parked" | "running" | "done";

// Index of the step at which the agent updates the issue status.
const STATUS_STEPS: Partial<Record<number, Status>> = { 2: "in_progress", 5: "in_review" };

const statusDot: Record<Status, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

export function AssignDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("start");
  const [assignee, setAssignee] = useState<Candidate | null>(null);
  const [status, setStatus] = useState<Status>("todo");
  const [queued, setQueued] = useState(true);
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";

  const pick = (candidate: Candidate) => {
    setAssignee(candidate);
    setPhase(candidate.agent ? "confirm" : "member");
  };

  const confirm = () => {
    setPhase("running");
    after(1000, () => setQueued(false));
    t.steps.forEach((_, i) =>
      after(1000 + i * 1000, () => {
        setStepsShown(i + 1);
        const next = STATUS_STEPS[i + 1];
        if (next) setStatus(next);
      }),
    );
    after(1000 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("start");
    setAssignee(null);
    setStatus("todo");
    setQueued(true);
    setStepsShown(0);
  };

  const runActive = phase === "running";
  const showRun = (phase === "running" || phase === "done") && assignee;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-80 flex-col gap-3 p-4">
        <dl className="grid w-fit grid-cols-[auto_auto] items-center gap-x-6 gap-y-2 text-body">
          <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
          <dd className="flex items-center gap-1.5">
            <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", statusDot[status])} />
            <span key={status} className="motion-safe:animate-in motion-safe:fade-in">
              {t.status[status]}
            </span>
          </dd>
          <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
          <dd className="relative">
            <CoachMark active={shownPhase === "start"} side="right" ringClassName="rounded-md">
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={phase === "pick"}
                disabled={!visible || (phase !== "start" && phase !== "pick")}
                onClick={() => setPhase(phase === "pick" ? "start" : "pick")}
                className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-muted disabled:hover:bg-transparent"
              >
                {assignee ? (
                  <>
                    <DemoAvatar name={assignee.name} agent={assignee.agent} />
                    {assignee.name}
                  </>
                ) : (
                  <span className="text-muted-foreground">{t.unassigned}</span>
                )}
                {(phase === "start" || phase === "pick") && (
                  <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
                )}
              </button>
            </CoachMark>
            {phase === "pick" && (
              <div
                role="listbox"
                aria-label={t.assigneeLabel}
                className="absolute top-full left-0 z-10 mt-1 w-52 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                {[true, false].map((isAgent) => (
                  <div key={String(isAgent)}>
                    <div className="px-2 pt-1 pb-0.5 text-micro text-muted-foreground">
                      {isAgent ? t.agentsGroup : t.membersGroup}
                    </div>
                    {t.candidates
                      .filter((c) => c.agent === isAgent)
                      .map((candidate, i) => (
                        <CoachMark
                          key={candidate.name}
                          active={isAgent && i === 0}
                          side="right"
                          ringClassName="rounded-md"
                          className="flex w-full"
                        >
                          <button
                            type="button"
                            role="option"
                            aria-selected={false}
                            onClick={() => pick(candidate)}
                            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent"
                          >
                            <DemoAvatar name={candidate.name} agent={candidate.agent} />
                            {candidate.name}
                          </button>
                        </CoachMark>
                      ))}
                  </div>
                ))}
              </div>
            )}
          </dd>
        </dl>

        {showRun && (
          <DemoRun
            agent={assignee.name}
            status={phase === "done" ? t.completed : queued ? t.queued : t.running}
            active={runActive}
          >
            {t.steps.slice(0, stepsShown).map((step, i) => (
              <DemoStep key={step} state={i === stepsShown - 1 && runActive ? "running" : "done"}>
                {step}
              </DemoStep>
            ))}
          </DemoRun>
        )}

        {phase === "done" && assignee && (
          <DemoComment author={assignee.name} agent>
            {t.reply}
          </DemoComment>
        )}

        {phase === "confirm" && assignee && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-background/60 p-4 backdrop-blur-[2px] motion-safe:animate-in motion-safe:fade-in">
            <div
              role="dialog"
              aria-modal="false"
              aria-labelledby="assign-demo-title"
              className="w-full max-w-sm rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:zoom-in-95"
            >
              <div className="flex items-start gap-2 p-4 pb-3">
                <div className="min-w-0 flex-1">
                  <div id="assign-demo-title" className="text-title-sm font-medium">
                    {t.confirmTitle}
                  </div>
                  <div className="mt-1 text-body text-muted-foreground">{t.confirmBody(assignee.name)}</div>
                </div>
                <button
                  type="button"
                  aria-label={t.close}
                  onClick={replay}
                  className="rounded-md p-1 text-muted-foreground hover:bg-muted"
                >
                  <X className="size-4" />
                </button>
              </div>
              <div className="flex justify-end gap-2 rounded-b-xl border-t bg-muted/40 px-4 py-3">
                <Button type="button" variant="outline" size="sm" onClick={() => setPhase("parked")}>
                  {t.dontStart}
                </Button>
                <CoachMark active side="bottom" align="end" ringClassName="rounded-lg">
                  <Button type="button" size="sm" onClick={confirm}>
                    {t.confirm}
                  </Button>
                </CoachMark>
              </div>
            </div>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
