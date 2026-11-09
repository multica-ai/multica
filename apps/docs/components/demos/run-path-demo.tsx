"use client";

import { useState, type ReactNode } from "react";
import { ArrowRight, Laptop, Loader2 } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

// Run statuses in the order the execution log shows them.
type RunStatus = "queued" | "dispatched" | "running" | "completed";

const copy = {
  en: {
    title: "OPS-31 · Compile this week's customer support FAQ",
    multicaLane: "Multica",
    multicaRole: "Records and coordinates",
    computerLane: "Your computer",
    computerRole: "Executes",
    issueTitle: "OPS-31 · Compile this week's customer support FAQ",
    issueDesc: "Turn the questions that came up again and again in last week's support tickets into an FAQ document.",
    assigneeLabel: "Assignee",
    agent: "Ops helper",
    configLabel: "Configuration used",
    config: ["Instructions", "Model", "Skills", "Runtime"],
    executionLog: "Execution log",
    status: { queued: "Queued", dispatched: "Starting", running: "Working", completed: "Completed" } as Record<RunStatus, string>,
    toolCount: (n: number) => (n === 1 ? "1 tool" : `${n} tools`),
    tool: "Claude Code",
    online: "Online",
    claimed: "Claimed the OPS-31 run and started Claude Code",
    actions: [
      "Read last week's ticket export in the working directory",
      "Count the questions that came up repeatedly",
      "Write the FAQ document",
    ],
    wroteBack: "Wrote the results back to the issue",
    reply: "The FAQ is ready: 12 questions grouped by account, orders and refunds. Please review.",
    stepOf: (n: number, total: number) => `Step ${n} of ${total}`,
    steps: [
      "The issue provides the context",
      "Multica creates a run",
      "A runtime claims the run",
      "The AI coding tool executes locally",
      "Results are written back to the issue",
    ],
    next: "Next",
    captions: {
      idle: "An issue just assigned to an agent.",
      steps: [
        "The issue holds the work description, the discussion, and the assignee; the run uses the agent's instructions, model, skills, and runtime. Click Next.",
        "The run enters the queue; if no runtime is online, it waits there. Click Next.",
        "An online runtime on your computer claims the run and invokes the AI coding tool the agent is configured to use. Click Next.",
        "The tool reads the working directory, runs commands, and produces results; progress streams back to the execution log.",
        "Results appear in the issue's timeline and execution log. Completed only means this run has ended. Use Replay to watch again.",
      ],
      working: "The tool reads the working directory, runs commands, and produces results; progress streams back to the execution log. Click Next.",
    },
  },
  zh: {
    title: "OPS-31 · 整理本周客服常见问题",
    multicaLane: "Multica",
    multicaRole: "记录与协调",
    computerLane: "你的电脑",
    computerRole: "负责执行",
    issueTitle: "OPS-31 · 整理本周客服常见问题",
    issueDesc: "把上周客服工单里反复出现的问题整理成一份常见问题文档。",
    assigneeLabel: "负责人",
    agent: "运营助手",
    configLabel: "使用的配置",
    config: ["指令", "模型", "skill", "运行时"],
    executionLog: "执行日志",
    status: { queued: "排队中", dispatched: "启动中", running: "正在工作", completed: "已完成" } as Record<RunStatus, string>,
    toolCount: (n: number) => `${n} 次工具调用`,
    tool: "Claude Code",
    online: "在线",
    claimed: "领取 OPS-31 的运行，启动 Claude Code",
    actions: ["读取工作目录里的上周工单导出文件", "统计反复出现的问题", "写出常见问题文档"],
    wroteBack: "把结果写回任务",
    reply: "常见问题文档已整理好：共 12 个问题，按账号、订单、退款分组，请检查。",
    stepOf: (n: number, total: number) => `第 ${n} 步，共 ${total} 步`,
    steps: ["任务提供上下文", "Multica 创建运行", "运行时领取运行", "AI 编程工具在本地执行", "结果写回任务"],
    next: "下一步",
    captions: {
      idle: "一条刚分配给智能体的任务。",
      steps: [
        "任务保存工作说明、讨论和负责人；运行会使用智能体的指令、模型、skill 和运行时配置。点下一步。",
        "运行进入队列；没有在线运行时时，它会在队列中等待。点下一步。",
        "你电脑上的在线运行时领取运行，调用智能体配置的 AI 编程工具。点下一步。",
        "工具读取工作目录、运行命令并生成结果，进度实时流回执行日志。",
        "结果显示在任务的时间线和执行日志中。已完成只表示这一次运行结束。点右上角可以重新演示。",
      ],
      working: "工具读取工作目录、运行命令并生成结果，进度实时流回执行日志。点下一步。",
    },
  },
  ja: {
    title: "OPS-31 · 今週のカスタマーサポートのよくある質問をまとめる",
    multicaLane: "Multica",
    multicaRole: "記録と調整",
    computerLane: "あなたのコンピュータ",
    computerRole: "実行を担当",
    issueTitle: "OPS-31 · 今週のカスタマーサポートのよくある質問をまとめる",
    issueDesc: "先週のサポートチケットで繰り返し出た質問を、よくある質問のドキュメントにまとめる。",
    assigneeLabel: "担当者",
    agent: "運用アシスタント",
    configLabel: "使われる設定",
    config: ["指示", "モデル", "スキル", "ランタイム"],
    executionLog: "実行ログ",
    status: { queued: "待機中", dispatched: "開始中", running: "作業中", completed: "完了" } as Record<RunStatus, string>,
    toolCount: (n: number) => `ツール ${n} 件`,
    tool: "Claude Code",
    online: "オンライン",
    claimed: "OPS-31 の実行を引き受け、Claude Code を起動",
    actions: [
      "作業ディレクトリにある先週のチケットのエクスポートを読む",
      "繰り返し出た質問を集計する",
      "よくある質問のドキュメントを書く",
    ],
    wroteBack: "結果をタスクに書き戻す",
    reply: "よくある質問のドキュメントができました。全 12 問を、アカウント、注文、返金に分けています。確認をお願いします。",
    stepOf: (n: number, total: number) => `ステップ ${n} / ${total}`,
    steps: [
      "タスクがコンテキストを提供する",
      "Multica が実行を作成する",
      "ランタイムが実行を引き受ける",
      "AI コーディングツールがローカルで実行する",
      "結果がタスクに書き戻される",
    ],
    next: "次へ",
    captions: {
      idle: "エージェントに割り当てたばかりのタスクです。",
      steps: [
        "タスクには作業の説明、議論、担当者が保存されています。実行にはエージェントの指示、モデル、スキル、ランタイム設定が使われます。「次へ」をクリックしてください。",
        "実行はキューに入ります。オンラインのランタイムがなければ、キューで待機します。「次へ」をクリックしてください。",
        "あなたのコンピュータ上のオンラインのランタイムが実行を引き受け、エージェントに設定された AI コーディングツールを呼び出します。「次へ」をクリックしてください。",
        "ツールは作業ディレクトリを読み、コマンドを実行して結果を生成します。進捗はリアルタイムで実行ログに流れます。",
        "結果はタスクのタイムラインと実行ログに表示されます。「完了」は、この 1 回の実行が終わったことを示すだけです。「もう一度」で最初から見られます。",
      ],
      working: "ツールは作業ディレクトリを読み、コマンドを実行して結果を生成します。進捗はリアルタイムで実行ログに流れます。「次へ」をクリックしてください。",
    },
  },
  ko: {
    title: "OPS-31 · 이번 주 고객 지원 자주 묻는 질문 정리",
    multicaLane: "Multica",
    multicaRole: "기록과 조율",
    computerLane: "내 컴퓨터",
    computerRole: "실행 담당",
    issueTitle: "OPS-31 · 이번 주 고객 지원 자주 묻는 질문 정리",
    issueDesc: "지난주 고객 지원 티켓에서 반복해서 나온 질문을 자주 묻는 질문 문서로 정리합니다.",
    assigneeLabel: "담당자",
    agent: "운영 도우미",
    configLabel: "사용하는 설정",
    config: ["지침", "모델", "스킬", "런타임"],
    executionLog: "실행 로그",
    status: { queued: "대기 중", dispatched: "시작 중", running: "작업 중", completed: "완료됨" } as Record<RunStatus, string>,
    toolCount: (n: number) => `도구 ${n}개`,
    tool: "Claude Code",
    online: "온라인",
    claimed: "OPS-31의 실행을 가져와 Claude Code 시작",
    actions: [
      "작업 디렉터리에서 지난주 티켓 내보내기 파일 읽기",
      "반복해서 나온 질문 집계",
      "자주 묻는 질문 문서 작성",
    ],
    wroteBack: "결과를 태스크에 다시 기록",
    reply: "자주 묻는 질문 문서를 정리했습니다. 질문 12개를 계정, 주문, 환불로 나눴으니 확인해 주세요.",
    stepOf: (n: number, total: number) => `${total}단계 중 ${n}단계`,
    steps: [
      "태스크가 컨텍스트 제공",
      "Multica가 실행 생성",
      "런타임이 실행을 가져감",
      "AI 코딩 도구가 로컬에서 실행",
      "결과를 태스크에 기록",
    ],
    next: "다음",
    captions: {
      idle: "에이전트에게 방금 할당한 태스크입니다.",
      steps: [
        "태스크에는 작업 설명, 논의, 담당자가 저장됩니다. 실행에는 에이전트의 지침, 모델, 스킬, 런타임 설정이 사용됩니다. 다음을 클릭하세요.",
        "실행이 큐에 들어갑니다. 온라인 런타임이 없으면 큐에서 기다립니다. 다음을 클릭하세요.",
        "내 컴퓨터의 온라인 런타임이 실행을 가져와 에이전트에 설정된 AI 코딩 도구를 호출합니다. 다음을 클릭하세요.",
        "도구가 작업 디렉터리를 읽고 명령을 실행해 결과를 만듭니다. 진행 상황은 실행 로그로 실시간 전송됩니다.",
        "결과가 태스크 타임라인과 실행 로그에 표시됩니다. 완료됨은 이번 실행이 끝났다는 뜻일 뿐입니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
      ],
      working: "도구가 작업 디렉터리를 읽고 명령을 실행해 결과를 만듭니다. 진행 상황은 실행 로그로 실시간 전송됩니다. 다음을 클릭하세요.",
    },
  },
  fr: {
    title: "OPS-31 · Compiler la FAQ du support client de la semaine",
    multicaLane: "Multica",
    multicaRole: "Consigne et coordonne",
    computerLane: "Votre ordinateur",
    computerRole: "Exécute",
    issueTitle: "OPS-31 · Compiler la FAQ du support client de la semaine",
    issueDesc: "Transformer les questions revenues sans cesse dans les tickets de support de la semaine dernière en un document FAQ.",
    assigneeLabel: "Assigné à",
    agent: "Assistant ops",
    configLabel: "Configuration utilisée",
    config: ["Instructions", "Modèle", "Skills", "Runtime"],
    executionLog: "Journal d'exécution",
    status: { queued: "En file d'attente", dispatched: "Démarrage", running: "Au travail", completed: "Terminée" } as Record<RunStatus, string>,
    toolCount: (n: number) => (n === 1 ? "1 outil" : `${n} outils`),
    tool: "Claude Code",
    online: "En ligne",
    claimed: "A pris en charge l'exécution d'OPS-31 et lancé Claude Code",
    actions: [
      "Lire l'export des tickets de la semaine dernière dans le répertoire de travail",
      "Compter les questions qui reviennent",
      "Rédiger le document FAQ",
    ],
    wroteBack: "A réécrit les résultats dans la tâche",
    reply: "La FAQ est prête : 12 questions regroupées par compte, commandes et remboursements. Merci de la relire.",
    stepOf: (n: number, total: number) => `Étape ${n} sur ${total}`,
    steps: [
      "La tâche fournit le contexte",
      "Multica crée une exécution",
      "Un runtime prend en charge l'exécution",
      "L'outil de codage IA s'exécute localement",
      "Les résultats sont réécrits dans la tâche",
    ],
    next: "Suivant",
    captions: {
      idle: "Une tâche qui vient d'être assignée à un agent.",
      steps: [
        "La tâche contient la description du travail, la discussion et l'assigné ; l'exécution utilise les instructions, le modèle, les skills et le runtime de l'agent. Cliquez sur Suivant.",
        "L'exécution entre dans la file d'attente ; si aucun runtime n'est en ligne, elle y patiente. Cliquez sur Suivant.",
        "Un runtime en ligne sur votre ordinateur prend en charge l'exécution et appelle l'outil de codage IA configuré pour l'agent. Cliquez sur Suivant.",
        "L'outil lit le répertoire de travail, exécute des commandes et produit des résultats ; l'avancement remonte en temps réel dans le journal d'exécution.",
        "Les résultats apparaissent dans le fil d'activité et le journal d'exécution de la tâche. Terminée signifie seulement que cette exécution est finie. Cliquez sur Rejouer pour revoir la démo.",
      ],
      working: "L'outil lit le répertoire de travail, exécute des commandes et produit des résultats ; l'avancement remonte en temps réel dans le journal d'exécution. Cliquez sur Suivant.",
    },
  },
};

const STATUS_AT_STEP: RunStatus[] = ["queued", "queued", "dispatched", "running", "completed"];
const LAST = 4;

function Lane({
  icon,
  name,
  role,
  active,
  children,
}: {
  icon: ReactNode;
  name: string;
  role: string;
  active: boolean;
  children: ReactNode;
}) {
  return (
    <section
      className={cn(
        "flex min-w-0 flex-col gap-2 rounded-lg border p-3 transition-colors",
        active ? "border-brand/60 bg-brand/5" : "bg-muted/30",
      )}
    >
      <div className="flex items-center gap-1.5">
        {icon}
        <span className="text-label font-medium">{name}</span>
        <span className="text-caption text-muted-foreground">· {role}</span>
      </div>
      {children}
    </section>
  );
}

/** The piece of the lane the current step is about. */
function Focus({ on, children }: { on: boolean; children: ReactNode }) {
  return (
    <div
      className={cn(
        "rounded-md border bg-background px-2.5 py-2 transition-shadow motion-safe:animate-in motion-safe:fade-in",
        on && "ring-2 ring-brand/60",
      )}
    >
      {children}
    </div>
  );
}

export function RunPathDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [step, setStep] = useState(0);
  const [actionsShown, setActionsShown] = useState(0);

  const executing = step === 3 && actionsShown < t.actions.length;
  const status = STATUS_AT_STEP[step] ?? "queued";

  const next = () => {
    const to = step + 1;
    setStep(to);
    if (to === 3) {
      t.actions.forEach((_, i) => after(700 + i * 900, () => setActionsShown(i + 1)));
    }
  };

  const replay = () => {
    clear();
    setStep(0);
    setActionsShown(0);
  };

  const caption = !visible
    ? t.captions.idle
    : step === 3 && !executing
      ? t.captions.working
      : t.captions.steps[step];

  const onComputer = step === 2 || step === 3;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="flex flex-col gap-3 p-4">
        <ol className="hidden grid-cols-5 gap-2 sm:grid" aria-label={t.stepOf(step + 1, t.steps.length)}>
          {t.steps.map((label, i) => (
            <li
              key={label}
              aria-current={i === step ? "step" : undefined}
              className={cn(
                "flex flex-col gap-1 border-t-2 pt-1.5 text-caption transition-colors",
                i < step && "border-brand/50 text-muted-foreground",
                i === step && "border-brand font-medium text-foreground",
                i > step && "border-border text-muted-foreground",
              )}
            >
              <span className="text-micro">{i + 1}</span>
              {label}
            </li>
          ))}
        </ol>
        <div className="text-caption sm:hidden">
          <span className="text-muted-foreground">{t.stepOf(step + 1, t.steps.length)} · </span>
          <span className="font-medium">{t.steps[step]}</span>
        </div>

        <div className="grid gap-3 sm:grid-cols-2">
          <Lane
            icon={<span aria-hidden className="size-2 rounded-full bg-brand" />}
            name={t.multicaLane}
            role={t.multicaRole}
            active={!onComputer}
          >
            <Focus on={step === 0}>
              <div className="text-body font-medium">{t.issueTitle}</div>
              <p className="mt-0.5 text-caption text-muted-foreground">{t.issueDesc}</p>
              <div className="mt-2 flex items-center gap-1.5 text-caption">
                <span className="text-muted-foreground">{t.assigneeLabel}</span>
                <DemoAvatar name={t.agent} agent />
                <span>{t.agent}</span>
              </div>
              {step === 0 && (
                <div className="mt-2 flex flex-wrap items-center gap-1 motion-safe:animate-in motion-safe:fade-in">
                  <span className="mr-0.5 text-micro text-muted-foreground">{t.configLabel}</span>
                  {t.config.map((item) => (
                    <span key={item} className="rounded-sm bg-brand/10 px-1.5 py-0.5 text-micro font-medium text-brand">
                      {item}
                    </span>
                  ))}
                </div>
              )}
            </Focus>

            {step >= 1 && (
              <Focus on={step === 1}>
                <div className="text-micro text-muted-foreground">{t.executionLog}</div>
                <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
                  <DemoAvatar name={t.agent} agent />
                  <span className="text-label font-medium">{t.agent}</span>
                  <span
                    key={status}
                    className={cn(
                      "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-micro font-medium motion-safe:animate-in motion-safe:fade-in",
                      status === "completed" ? "bg-success/12 text-success" : "bg-muted text-muted-foreground",
                    )}
                  >
                    {status !== "completed" && (
                      <Loader2 aria-hidden className="size-3 motion-safe:animate-spin" />
                    )}
                    {t.status[status]}
                  </span>
                  {actionsShown > 0 && (
                    <span className="text-caption text-muted-foreground">{t.toolCount(actionsShown)}</span>
                  )}
                </div>
              </Focus>
            )}

            {step === LAST && (
              <div className="ring-2 ring-brand/60 rounded-lg">
                <DemoComment author={t.agent} agent>
                  {t.reply}
                </DemoComment>
              </div>
            )}
          </Lane>

          <Lane
            icon={<Laptop aria-hidden className="size-3.5 text-muted-foreground" />}
            name={t.computerLane}
            role={t.computerRole}
            active={onComputer}
          >
            <Focus on={step === 2}>
              <div className="flex items-center gap-2 text-label">
                <span className="font-medium">{t.tool}</span>
                <span className="inline-flex items-center gap-1 text-caption text-muted-foreground">
                  <span aria-hidden className="size-1.5 rounded-full bg-success" />
                  {t.online}
                </span>
              </div>
              {step >= 2 && (
                <ul className="mt-1.5">
                  <DemoStep state="done">{t.claimed}</DemoStep>
                </ul>
              )}
            </Focus>

            {step >= 3 && (
              <Focus on={step === 3}>
                <ul className="flex flex-col gap-0.5">
                  {t.actions.slice(0, actionsShown).map((action, i) => (
                    <DemoStep key={action} state={i === actionsShown - 1 && executing ? "running" : "done"}>
                      {action}
                    </DemoStep>
                  ))}
                  {actionsShown === 0 && (
                    <li className="flex items-center gap-2 py-0.5 text-caption text-muted-foreground">
                      <Loader2 aria-hidden className="size-3.5 text-brand motion-safe:animate-spin" />
                      {t.status.running}
                    </li>
                  )}
                </ul>
              </Focus>
            )}

            {step === LAST && (
              <ul className="px-1">
                <DemoStep state="done">
                  <span className="inline-flex items-center gap-1">
                    {t.wroteBack}
                    <ArrowRight aria-hidden className="size-3 max-sm:-rotate-90" />
                  </span>
                </DemoStep>
              </ul>
            )}
          </Lane>
        </div>

        {step < LAST && (
          <div className="flex justify-end">
            <CoachMark active={visible && !executing} side="left" ringClassName="rounded-lg">
              <Button type="button" size="sm" disabled={!visible || executing} onClick={next}>
                {t.next}
                <ArrowRight />
              </Button>
            </CoachMark>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
