"use client";

import { useEffect, useState } from "react";
import { CheckCircle2, Clock, FastForward, Loader2, Pencil, Play } from "lucide-react";
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
type Source = "manual" | "schedule";

const copy = {
  en: {
    title: "Autopilot · Weekly customer feedback roundup",
    breadcrumb: "Autopilot",
    name: "Weekly customer feedback roundup",
    active: "Active",
    edit: "Edit",
    runNow: "Run now",
    running: "Running...",
    toast: "Autopilot triggered",
    properties: "Properties",
    agentLabel: "Agent",
    agent: "Ops helper",
    modeLabel: "Output Mode",
    mode: "Create Issue",
    triggers: "Triggers",
    schedule: "Schedule",
    scheduleText: "09:00 · Mon (Asia/Shanghai)",
    cron: "0 9 * * 1",
    nextBefore: "Next: Oct 26, 09:00",
    nextAfter: "Next: Nov 2, 09:00",
    fastForward: "Skip ahead to Monday 09:00",
    history: "Run History",
    noRuns: "No runs yet. Click \"Run now\" to trigger manually.",
    runStatus: { created: "Issue Created", completed: "Completed" },
    source: { manual: "Manual", schedule: "Schedule" } as Record<Source, string>,
    issueLinked: "Issue linked",
    runTime: { manual: "Oct 24, 15:42", schedule: "Oct 26, 09:00" } as Record<Source, string>,
    issueTitle: "OPS-31 · Weekly customer feedback roundup",
    statusLabel: "Status",
    issueStatus: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<IssueStatus, string>,
    assigneeLabel: "Assignee",
    queued: "Queued",
    workingStatus: "Running",
    completed: "Completed",
    steps: [
      "Read the issue description and discussion",
      "Set the status to In Progress",
      "Collect last week's support tickets, survey answers and app reviews",
      "Group them by problem type and count each group",
      "Write a one-page weekly report",
      "Set the status to In Review",
    ],
    reply:
      "Last week's feedback is in: 64 items in five groups. Slow delivery is up the most, with 21 mentions. The one-page report is ready; please review.",
    captions: {
      idle: "An autopilot that runs every Monday at 09:00 and creates an issue each time.",
      ready: "Click \"Run now\" to run the whole flow once. Or skip ahead to Monday 09:00 to watch the schedule fire.",
      triggering: "Triggering the autopilot...",
      created: {
        manual: "The autopilot created issue OPS-31 and assigned it to Ops helper. Run History records a manual run.",
        schedule: "Monday 09:00 arrived and the schedule fired: the autopilot created issue OPS-31 and assigned it to Ops helper.",
      } as Record<Source, string>,
      running: "Ops helper follows the runbook and reports progress on the issue.",
      done: "The issue moved to In Review, so this autopilot run is marked Completed. Use Replay to try the other trigger.",
    },
  },
  zh: {
    title: "自动化 · 每周客户反馈整理",
    breadcrumb: "自动化",
    name: "每周客户反馈整理",
    active: "启用",
    edit: "编辑",
    runNow: "立即运行",
    running: "运行中...",
    toast: "已触发自动化",
    properties: "属性",
    agentLabel: "智能体",
    agent: "运营助手",
    modeLabel: "输出模式",
    mode: "创建任务",
    triggers: "触发器",
    schedule: "时间表",
    scheduleText: "09:00 · 周一 (Asia/Shanghai)",
    cron: "0 9 * * 1",
    nextBefore: "下次：10月26日 09:00",
    nextAfter: "下次：11月2日 09:00",
    fastForward: "快进到周一 09:00",
    history: "运行历史",
    noRuns: "暂无运行记录。点击\"立即运行\"手动触发。",
    runStatus: { created: "已创建任务", completed: "已完成" },
    source: { manual: "手动", schedule: "时间表" } as Record<Source, string>,
    issueLinked: "已关联任务",
    runTime: { manual: "10月24日 15:42", schedule: "10月26日 09:00" } as Record<Source, string>,
    issueTitle: "OPS-31 · 每周客户反馈整理",
    statusLabel: "状态",
    issueStatus: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<IssueStatus, string>,
    assigneeLabel: "负责人",
    queued: "排队中",
    workingStatus: "运行中",
    completed: "已完成",
    steps: [
      "读取任务描述和讨论",
      "把状态改为 进行中",
      "汇总上周的客服工单、问卷回复和应用商店评论",
      "按问题类型归类，统计每类数量",
      "写成一页周报",
      "把状态改为 审核中",
    ],
    reply: "上周反馈已整理好：共 64 条，分成 5 类，其中\"配送慢\"增加最多，有 21 条。一页周报已写好，请检查。",
    captions: {
      idle: "一条每周一 09:00 运行的自动化，每次触发都会创建一条任务。",
      ready: "点击\"立即运行\"，手动执行一次完整流程；也可以快进到周一 09:00，看时间表怎么触发。",
      triggering: "正在触发自动化...",
      created: {
        manual: "自动化创建了任务 OPS-31，交给 运营助手。运行历史记下一次手动运行。",
        schedule: "到了周一 09:00，时间表触发：自动化创建了任务 OPS-31，交给 运营助手。",
      } as Record<Source, string>,
      running: "运营助手 按 Runbook 处理任务，进度写在任务里。",
      done: "任务进入审核中，这次自动化运行记为已完成。点右上角可以换另一种触发方式再试。",
    },
  },
  ja: {
    title: "オートパイロット · 毎週の顧客フィードバックまとめ",
    breadcrumb: "オートパイロット",
    name: "毎週の顧客フィードバックまとめ",
    active: "アクティブ",
    edit: "編集",
    runNow: "今すぐ実行",
    running: "実行中...",
    toast: "オートパイロットを実行しました",
    properties: "プロパティ",
    agentLabel: "エージェント",
    agent: "運用アシスタント",
    modeLabel: "出力モード",
    mode: "タスクを作成",
    triggers: "トリガー",
    schedule: "スケジュール",
    scheduleText: "09:00 · 月 (Asia/Shanghai)",
    cron: "0 9 * * 1",
    nextBefore: "次回: 10月26日 09:00",
    nextAfter: "次回: 11月2日 09:00",
    fastForward: "月曜 09:00 まで早送り",
    history: "実行履歴",
    noRuns: "まだ実行履歴がありません。手動で実行するには「今すぐ実行」をクリックしてください。",
    runStatus: { created: "タスク作成済み", completed: "完了" },
    source: { manual: "手動", schedule: "スケジュール" } as Record<Source, string>,
    issueLinked: "タスクをリンク済み",
    runTime: { manual: "10月24日 15:42", schedule: "10月26日 09:00" } as Record<Source, string>,
    issueTitle: "OPS-31 · 毎週の顧客フィードバックまとめ",
    statusLabel: "ステータス",
    issueStatus: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<IssueStatus, string>,
    assigneeLabel: "担当者",
    queued: "待機中",
    workingStatus: "実行中",
    completed: "完了",
    steps: [
      "タスクの説明と議論を読む",
      "ステータスを 進行中 にする",
      "先週のサポートチケット、アンケート回答、アプリレビューを集める",
      "問題の種類ごとに分類し、件数を数える",
      "1 ページの週次レポートにまとめる",
      "ステータスを レビュー中 にする",
    ],
    reply:
      "先週のフィードバックをまとめました。全 64 件を 5 つに分類し、最も増えたのは「配送が遅い」で 21 件です。1 ページのレポートができたので確認をお願いします。",
    captions: {
      idle: "毎週月曜 09:00 に動き、そのたびにタスクを作成するオートパイロットです。",
      ready: "「今すぐ実行」をクリックすると、一連の流れを 1 回手動で実行します。月曜 09:00 まで早送りして、スケジュールでの起動を見ることもできます。",
      triggering: "オートパイロットを実行しています...",
      created: {
        manual: "オートパイロットがタスク OPS-31 を作成し、運用アシスタント に割り当てました。実行履歴には手動の実行が記録されます。",
        schedule: "月曜 09:00 になりスケジュールが発火しました。オートパイロットがタスク OPS-31 を作成し、運用アシスタント に割り当てました。",
      } as Record<Source, string>,
      running: "運用アシスタント が Runbook に沿って作業し、進捗をタスクに書き込みます。",
      done: "タスクがレビュー中になり、このオートパイロットの実行は完了と記録されました。「もう一度」でもう一方のトリガーも試せます。",
    },
  },
  ko: {
    title: "오토파일럿 · 주간 고객 피드백 정리",
    breadcrumb: "오토파일럿",
    name: "주간 고객 피드백 정리",
    active: "활성",
    edit: "수정",
    runNow: "지금 실행",
    running: "실행 중...",
    toast: "오토파일럿을 실행했습니다",
    properties: "속성",
    agentLabel: "에이전트",
    agent: "운영 도우미",
    modeLabel: "출력 모드",
    mode: "태스크 생성",
    triggers: "트리거",
    schedule: "일정",
    scheduleText: "09:00 · 월 (Asia/Shanghai)",
    cron: "0 9 * * 1",
    nextBefore: "다음: 10월 26일 09:00",
    nextAfter: "다음: 11월 2일 09:00",
    fastForward: "월요일 09:00으로 건너뛰기",
    history: "실행 기록",
    noRuns: "아직 실행 기록이 없습니다. 수동으로 실행하려면 \"지금 실행\"을 클릭하세요.",
    runStatus: { created: "태스크 생성됨", completed: "완료됨" },
    source: { manual: "수동", schedule: "일정" } as Record<Source, string>,
    issueLinked: "태스크 연결됨",
    runTime: { manual: "10월 24일 15:42", schedule: "10월 26일 09:00" } as Record<Source, string>,
    issueTitle: "OPS-31 · 주간 고객 피드백 정리",
    statusLabel: "상태",
    issueStatus: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<IssueStatus, string>,
    assigneeLabel: "담당자",
    queued: "대기 중",
    workingStatus: "실행 중",
    completed: "완료됨",
    steps: [
      "태스크 설명과 논의 읽기",
      "상태를 진행 중으로 변경",
      "지난주 고객 지원 티켓, 설문 응답, 앱 리뷰 모으기",
      "문제 유형별로 묶고 유형마다 개수 세기",
      "한 페이지짜리 주간 보고서 작성",
      "상태를 리뷰 중으로 변경",
    ],
    reply:
      "지난주 피드백을 정리했습니다. 총 64건을 5개 유형으로 나눴고, 가장 많이 늘어난 것은 \"배송 지연\"으로 21건입니다. 한 페이지 보고서를 작성했으니 확인해 주세요.",
    captions: {
      idle: "매주 월요일 09:00에 실행되고, 실행할 때마다 태스크를 만드는 오토파일럿입니다.",
      ready: "\"지금 실행\"을 클릭하면 전체 흐름을 한 번 수동으로 실행합니다. 월요일 09:00으로 건너뛰어 일정이 실행되는 모습을 볼 수도 있습니다.",
      triggering: "오토파일럿을 실행하는 중...",
      created: {
        manual: "오토파일럿이 태스크 OPS-31을 만들고 운영 도우미에게 할당했습니다. 실행 기록에 수동 실행이 남습니다.",
        schedule: "월요일 09:00이 되어 일정이 실행되었습니다. 오토파일럿이 태스크 OPS-31을 만들고 운영 도우미에게 할당했습니다.",
      } as Record<Source, string>,
      running: "운영 도우미가 Runbook에 따라 작업하고 진행 상황을 태스크에 남깁니다.",
      done: "태스크가 리뷰 중으로 바뀌어 이번 오토파일럿 실행은 완료됨으로 기록되었습니다. \"다시 보기\"로 다른 트리거도 시도해 보세요.",
    },
  },
  fr: {
    title: "Automatisation · Synthèse hebdomadaire des retours clients",
    breadcrumb: "Automatisation",
    name: "Synthèse hebdomadaire des retours clients",
    active: "Active",
    edit: "Modifier",
    runNow: "Exécuter maintenant",
    running: "Exécution...",
    toast: "Automatisation déclenchée",
    properties: "Propriétés",
    agentLabel: "Agent",
    agent: "Assistant ops",
    modeLabel: "Mode de sortie",
    mode: "Créer une tâche",
    triggers: "Déclencheurs",
    schedule: "Planification",
    scheduleText: "09:00 · lun. (Asia/Shanghai)",
    cron: "0 9 * * 1",
    nextBefore: "Prochaine : 26 oct., 09:00",
    nextAfter: "Prochaine : 2 nov., 09:00",
    fastForward: "Avancer jusqu'à lundi 09:00",
    history: "Historique des exécutions",
    noRuns: "Aucune exécution pour l'instant. Cliquez sur « Exécuter maintenant » pour déclencher manuellement.",
    runStatus: { created: "Tâche créée", completed: "Terminée" },
    source: { manual: "Manuelle", schedule: "Planification" } as Record<Source, string>,
    issueLinked: "Tâche liée",
    runTime: { manual: "24 oct., 15:42", schedule: "26 oct., 09:00" } as Record<Source, string>,
    issueTitle: "OPS-31 · Synthèse hebdomadaire des retours clients",
    statusLabel: "Statut",
    issueStatus: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<IssueStatus, string>,
    assigneeLabel: "Assigné à",
    queued: "En file d'attente",
    workingStatus: "En cours",
    completed: "Terminée",
    steps: [
      "Lire la description et la discussion de la tâche",
      "Passer le statut à En cours",
      "Rassembler les tickets support, les réponses au questionnaire et les avis de l'app de la semaine dernière",
      "Les regrouper par type de problème et compter chaque groupe",
      "Rédiger un rapport hebdomadaire d'une page",
      "Passer le statut à En relecture",
    ],
    reply:
      "Les retours de la semaine dernière sont regroupés : 64 éléments en cinq groupes. La lenteur de livraison progresse le plus, avec 21 mentions. Le rapport d'une page est prêt ; merci de le relire.",
    captions: {
      idle: "Une automatisation qui tourne chaque lundi à 09:00 et crée une tâche à chaque fois.",
      ready: "Cliquez sur « Exécuter maintenant » pour dérouler le flux complet une fois. Ou avancez jusqu'à lundi 09:00 pour voir la planification se déclencher.",
      triggering: "Déclenchement de l'automatisation...",
      created: {
        manual: "L'automatisation a créé la tâche OPS-31 et l'a assignée à Assistant ops. L'historique des exécutions enregistre une exécution manuelle.",
        schedule: "Lundi 09:00 est arrivé et la planification s'est déclenchée : l'automatisation a créé la tâche OPS-31 et l'a assignée à Assistant ops.",
      } as Record<Source, string>,
      running: "Assistant ops suit le runbook et rend compte de l'avancement dans la tâche.",
      done: "La tâche est passée à En relecture, donc cette exécution de l'automatisation est marquée Terminée. Rejouer permet d'essayer l'autre déclencheur.",
    },
  },
};

type Phase = "idle" | "ready" | "triggering" | "created" | "running" | "done";

// Index of the step at which the agent updates the issue status.
const STATUS_STEPS: Partial<Record<number, IssueStatus>> = { 2: "in_progress", 6: "in_review" };

const issueDot: Record<IssueStatus, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

export function AutopilotDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [cycle, setCycle] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [source, setSource] = useState<Source>("manual");
  const [issueStatus, setIssueStatus] = useState<IssueStatus>("todo");
  const [stepsShown, setStepsShown] = useState(0);
  const [toast, setToast] = useState(false);

  useEffect(() => {
    if (!visible) return;
    clear();
    after(500, () => setPhase("ready"));
  }, [visible, cycle, after, clear]);

  const startRun = (from: number) => {
    after(from, () => setPhase("running"));
    t.steps.forEach((_, i) =>
      after(from + 1000 + i * 1000, () => {
        setStepsShown(i + 1);
        const next = STATUS_STEPS[i + 1];
        if (next) setIssueStatus(next);
      }),
    );
    after(from + 1000 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const runNow = () => {
    setSource("manual");
    setPhase("triggering");
    after(700, () => {
      setPhase("created");
      setToast(true);
    });
    after(2700, () => setToast(false));
    startRun(2600);
  };

  const fastForward = () => {
    setSource("schedule");
    setPhase("created");
    startRun(2200);
  };

  const replay = () => {
    clear();
    setPhase("idle");
    setSource("manual");
    setIssueStatus("todo");
    setStepsShown(0);
    setToast(false);
    setCycle((c) => c + 1);
  };

  const shownPhase: Phase = visible ? phase : "idle";
  const fired = shownPhase === "created" || shownPhase === "running" || shownPhase === "done";
  const caption =
    shownPhase === "created" ? t.captions.created[source] : t.captions[shownPhase];
  const runStatus = shownPhase === "done" ? t.completed : shownPhase === "running" ? t.workingStatus : t.queued;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="relative flex min-h-96 flex-col gap-4 p-4">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-caption text-muted-foreground">{t.breadcrumb} /</span>
          <span className="min-w-0 truncate text-body font-medium">{t.name}</span>
          <span className="inline-flex items-center gap-1.5 text-caption font-medium text-success">
            <span aria-hidden className="inline-flex h-4 w-7 items-center rounded-full bg-success/80 p-0.5">
              <span className="ml-auto size-3 rounded-full bg-background" />
            </span>
            {t.active}
          </span>
          <span className="ml-auto flex items-center gap-1.5">
            <Button type="button" size="sm" variant="outline" disabled>
              <Pencil />
              {t.edit}
            </Button>
            <CoachMark active={shownPhase === "ready"} side="bottom" align="end" ringClassName="rounded-lg">
              <Button type="button" size="sm" disabled={shownPhase !== "ready"} onClick={runNow}>
                {shownPhase === "triggering" ? <Loader2 className="motion-safe:animate-spin" /> : <Play />}
                {shownPhase === "triggering" ? t.running : t.runNow}
              </Button>
            </CoachMark>
          </span>
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <section className="flex min-w-0 flex-col gap-2">
            <div className="text-caption font-medium text-muted-foreground">{t.properties}</div>
            <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 gap-y-1.5 text-body">
              <dt className="text-caption text-muted-foreground">{t.agentLabel}</dt>
              <dd className="flex min-w-0 items-center gap-1.5">
                <DemoAvatar name={t.agent} agent />
                <span className="truncate">{t.agent}</span>
              </dd>
              <dt className="text-caption text-muted-foreground">{t.modeLabel}</dt>
              <dd>{t.mode}</dd>
            </dl>
          </section>

          <section className="flex min-w-0 flex-col gap-2">
            <div className="text-caption font-medium text-muted-foreground">{t.triggers}</div>
            <div className="flex items-start gap-2.5 rounded-md border bg-background px-3 py-2">
              <Clock aria-hidden className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="text-body font-medium">{t.schedule}</div>
                <div className="text-caption text-muted-foreground">{t.scheduleText}</div>
                <div className="font-mono text-micro text-muted-foreground">{t.cron}</div>
                <div key={String(fired && source === "schedule")} className="text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
                  {fired && source === "schedule" ? t.nextAfter : t.nextBefore}
                </div>
              </div>
            </div>
            <Button
              type="button"
              size="xs"
              variant="ghost"
              className="self-start text-muted-foreground"
              disabled={shownPhase !== "ready"}
              onClick={fastForward}
            >
              <FastForward />
              {t.fastForward}
            </Button>
          </section>
        </div>

        <section className="flex min-w-0 flex-col gap-2">
          <div className="text-caption font-medium text-muted-foreground">{t.history}</div>
          <div className="overflow-hidden rounded-md border bg-background">
            {fired ? (
              <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-body motion-safe:animate-in motion-safe:fade-in">
                {shownPhase === "done" ? (
                  <CheckCircle2 aria-hidden className="size-4 shrink-0 text-success" />
                ) : (
                  <Clock aria-hidden className="size-4 shrink-0 text-info" />
                )}
                <span className={cn("w-20 shrink-0 text-caption font-medium", shownPhase === "done" ? "text-success" : "text-info")}>
                  {shownPhase === "done" ? t.runStatus.completed : t.runStatus.created}
                </span>
                <span className="shrink-0 text-caption text-muted-foreground">{t.source[source]}</span>
                <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">{t.issueLinked}</span>
                <span className="shrink-0 text-caption text-muted-foreground tabular-nums">{t.runTime[source]}</span>
              </div>
            ) : (
              <div className="px-3 py-3 text-center text-caption text-muted-foreground">{t.noRuns}</div>
            )}
          </div>
        </section>

        {fired && (
          <section className="flex min-w-0 flex-col gap-3 rounded-lg border border-dashed p-3 motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1">
            <div className="min-w-0 truncate text-label font-medium">{t.issueTitle}</div>
            <dl className="flex flex-wrap items-center gap-x-5 gap-y-1.5 text-body">
              <div className="flex items-center gap-2">
                <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
                <dd className="flex items-center gap-1.5">
                  <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", issueDot[issueStatus])} />
                  <span key={issueStatus} className="motion-safe:animate-in motion-safe:fade-in">
                    {t.issueStatus[issueStatus]}
                  </span>
                </dd>
              </div>
              <div className="flex items-center gap-2">
                <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
                <dd className="flex items-center gap-1.5">
                  <DemoAvatar name={t.agent} agent />
                  {t.agent}
                </dd>
              </div>
            </dl>
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
          </section>
        )}

        {toast && (
          <div
            aria-hidden
            className="absolute right-4 bottom-4 z-10 flex items-center gap-2 rounded-lg border bg-popover px-3 py-2 text-caption text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2"
          >
            <CheckCircle2 className="size-4 text-success" />
            {t.toast}
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
