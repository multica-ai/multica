"use client";

import { useState } from "react";
import {
  ArrowLeftRight,
  ChevronDown,
  ChevronRight,
  FolderKanban,
  Maximize2,
  MoreHorizontal,
  Paperclip,
  Users,
  X,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoFrame,
  DemoRun,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Actor = { name: string; squad: boolean };

const copy = {
  en: {
    title: "Create with agent · Book the team-dinner venue",
    workspace: "Ops team",
    breadcrumb: "Create with agent",
    expand: "Expand",
    close: "Close",
    createdBy: "Created by",
    agentsGroup: "Agents",
    squadsGroup: "Squads",
    actors: [
      { name: "Ops helper", squad: false },
      { name: "Writing helper", squad: false },
      { name: "Admin squad", squad: true },
    ] as Actor[],
    prompt:
      "Team dinner next Wednesday. Book a restaurant for 12 people, under $40 a head, close to the office, a private room if possible.",
    project: "Team events",
    projectLabel: "Project",
    moreFields: "More fields",
    attach: "Attach file",
    switchToManual: "Switch to Manual",
    createAnother: "Create another",
    submit: "Create",
    sending: "Sending…",
    toastSent: "Sent to agent — you'll get an inbox notification when it's done",
    inbox: "Inbox",
    notificationType: "Created with agent",
    issueId: "OPS-51",
    issueTitle: "Book a restaurant for next Wednesday's team dinner (12 people)",
    issueDescription:
      "Team dinner next Wednesday: book a restaurant for 12 people, under $40 a head, close to the office, a private room if possible.",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    status: "Todo",
    queued: "Queued",
    running: "Running",
    firstStep: "Read the issue description and discussion",
    captions: {
      idle: "Say what needs doing in a sentence or two, and an agent turns it into an issue.",
      actor: "The request is written. Created by starts with the agent you picked last time; click it to choose another.",
      picking: "Pick Ops helper. A squad works too: its leader drafts the issue.",
      ready: "The project carries over from the page you opened the dialog on. Click Create.",
      sending: "Sending the request to Ops helper…",
      waiting: "Ops helper drafts it in the background, so the dialog has already closed. The inbox tells you when it's done.",
      inbox: "The notification arrived. Open the new issue.",
      done: "Ops helper wrote the title and description and kept itself as the assignee. The issue is in Todo, so it starts working on it next.",
    },
  },
  zh: {
    title: "通过智能体创建：团建订餐厅",
    workspace: "运营部",
    breadcrumb: "通过智能体创建",
    expand: "展开",
    close: "关闭",
    createdBy: "创建者",
    agentsGroup: "智能体",
    squadsGroup: "小队",
    actors: [
      { name: "运营助手", squad: false },
      { name: "文案助手", squad: false },
      { name: "行政小队", squad: true },
    ] as Actor[],
    prompt: "下周三团建，帮忙订一家餐厅：12 个人，人均 150 元以内，离公司近一点，最好有包间。",
    project: "团队活动",
    projectLabel: "项目",
    moreFields: "更多字段",
    attach: "添加附件",
    switchToManual: "切换到手动",
    createAnother: "继续创建",
    submit: "创建",
    sending: "发送中...",
    toastSent: "已交给智能体——完成后会在收件箱通知你",
    inbox: "收件箱",
    notificationType: "通过智能体创建",
    issueId: "OPS-51",
    issueTitle: "预订下周三团建餐厅（12 人）",
    issueDescription: "下周三团建，订一家餐厅：12 人，人均 150 元以内，离公司近，最好有包间。",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    status: "待办",
    queued: "排队中",
    running: "运行中",
    firstStep: "读取任务描述和讨论",
    captions: {
      idle: "一两句话说清要做的事，由智能体写成任务。",
      actor: "需求已经写好。创建者默认是上次选的智能体，点它换一个。",
      picking: "选运营助手。也可以选小队，由队长来起草。",
      ready: "项目沿用了打开对话框时所在的项目。点击创建。",
      sending: "正在把需求交给运营助手...",
      waiting: "运营助手在后台起草，对话框已经关了，不用守着。写好后收件箱会通知你。",
      inbox: "通知到了。点开这条新任务。",
      done: "运营助手写好了标题和描述，负责人默认是它自己。任务处于待办，所以它接着就开始处理。",
    },
  },
  ja: {
    title: "エージェントで作成：チームの食事会の店を予約",
    workspace: "運用チーム",
    breadcrumb: "エージェントで作成",
    expand: "展開",
    close: "閉じる",
    createdBy: "作成者",
    agentsGroup: "エージェント",
    squadsGroup: "スクワッド",
    actors: [
      { name: "運用アシスタント", squad: false },
      { name: "ライティングアシスタント", squad: false },
      { name: "総務スクワッド", squad: true },
    ] as Actor[],
    prompt: "来週水曜のチームの食事会、お店を予約してほしい。12 人、1 人 5,000 円以内、会社の近くで、できれば個室。",
    project: "チームイベント",
    projectLabel: "プロジェクト",
    moreFields: "その他のフィールド",
    attach: "ファイルを添付",
    switchToManual: "手動に切り替え",
    createAnother: "もう 1 つ作成",
    submit: "作成",
    sending: "送信中…",
    toastSent: "エージェントに送信しました。完了するとインボックス通知が届きます",
    inbox: "インボックス",
    notificationType: "エージェントで作成",
    issueId: "OPS-51",
    issueTitle: "来週水曜のチーム食事会の店を予約（12 人）",
    issueDescription: "来週水曜のチームの食事会：12 人、1 人 5,000 円以内、会社の近く、できれば個室のお店を予約する。",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    status: "未着手",
    queued: "待機中",
    running: "実行中",
    firstStep: "タスクの説明とコメントを読む",
    captions: {
      idle: "やりたいことを一言二言で書けば、エージェントがタスクにまとめます。",
      actor: "依頼文は書けています。作成者には前回選んだエージェントが入っています。クリックして選び直します。",
      picking: "運用アシスタントを選びます。スクワッドを選ぶと、リーダーが下書きします。",
      ready: "プロジェクトは、ダイアログを開いたページのものが引き継がれています。作成をクリックします。",
      sending: "依頼を運用アシスタントに送っています…",
      waiting: "運用アシスタントがバックグラウンドで下書きするので、ダイアログはもう閉じています。終わるとインボックスに通知が届きます。",
      inbox: "通知が届きました。新しいタスクを開きます。",
      done: "運用アシスタントがタイトルと説明を書き、担当者には自分を設定しました。タスクは未着手なので、続けて作業を始めます。",
    },
  },
  ko: {
    title: "에이전트로 만들기: 팀 회식 식당 예약",
    workspace: "운영팀",
    breadcrumb: "에이전트로 만들기",
    expand: "펼치기",
    close: "닫기",
    createdBy: "생성자",
    agentsGroup: "에이전트",
    squadsGroup: "스쿼드",
    actors: [
      { name: "운영 도우미", squad: false },
      { name: "글쓰기 도우미", squad: false },
      { name: "총무 스쿼드", squad: true },
    ] as Actor[],
    prompt: "다음 주 수요일 팀 회식, 식당 하나 예약해 줘. 12명, 1인 4만 원 이내, 회사 근처, 가능하면 룸.",
    project: "팀 행사",
    projectLabel: "프로젝트",
    moreFields: "추가 필드",
    attach: "파일 첨부",
    switchToManual: "직접 만들기로 전환",
    createAnother: "하나 더 만들기",
    submit: "만들기",
    sending: "보내는 중...",
    toastSent: "에이전트에게 보냈습니다. 완료되면 인박스 알림을 받습니다",
    inbox: "인박스",
    notificationType: "에이전트로 생성됨",
    issueId: "OPS-51",
    issueTitle: "다음 주 수요일 팀 회식 식당 예약 (12명)",
    issueDescription: "다음 주 수요일 팀 회식: 12명, 1인 4만 원 이내, 회사 근처, 가능하면 룸이 있는 식당을 예약한다.",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    status: "할 일",
    queued: "대기 중",
    running: "실행 중",
    firstStep: "태스크 설명과 댓글 읽기",
    captions: {
      idle: "할 일을 한두 문장으로 적으면 에이전트가 태스크로 만듭니다.",
      actor: "요청은 이미 적혀 있습니다. 생성자에는 지난번에 고른 에이전트가 들어 있습니다. 클릭해서 바꿉니다.",
      picking: "운영 도우미를 고르세요. 스쿼드를 고르면 리더가 초안을 씁니다.",
      ready: "프로젝트는 대화상자를 연 페이지의 것이 그대로 들어갑니다. 만들기를 클릭하세요.",
      sending: "요청을 운영 도우미에게 보내는 중...",
      waiting: "운영 도우미가 백그라운드에서 초안을 쓰므로 대화상자는 이미 닫혔습니다. 끝나면 인박스로 알려 줍니다.",
      inbox: "알림이 왔습니다. 새 태스크를 여세요.",
      done: "운영 도우미가 제목과 설명을 쓰고 담당자는 자기 자신으로 두었습니다. 태스크가 할 일 상태라서 이어서 작업을 시작합니다.",
    },
  },
  fr: {
    title: "Créer avec un agent : réserver le restaurant du dîner d'équipe",
    workspace: "Équipe ops",
    breadcrumb: "Créer avec un agent",
    expand: "Agrandir",
    close: "Fermer",
    createdBy: "Créé par",
    agentsGroup: "Agents",
    squadsGroup: "Squads",
    actors: [
      { name: "Assistant ops", squad: false },
      { name: "Assistant rédaction", squad: false },
      { name: "Squad services généraux", squad: true },
    ] as Actor[],
    prompt:
      "Dîner d'équipe mercredi prochain : réserve un restaurant pour 12 personnes, moins de 35 € par tête, près du bureau, avec un salon privé si possible.",
    project: "Événements d'équipe",
    projectLabel: "Projet",
    moreFields: "Plus de champs",
    attach: "Joindre un fichier",
    switchToManual: "Passer en mode manuel",
    createAnother: "En créer un autre",
    submit: "Créer",
    sending: "Envoi…",
    toastSent: "Envoyé à l'agent — vous recevrez une notification dans votre boîte de réception une fois terminé",
    inbox: "Boîte de réception",
    notificationType: "Créé avec un agent",
    issueId: "OPS-51",
    issueTitle: "Réserver le restaurant du dîner d'équipe de mercredi prochain (12 personnes)",
    issueDescription:
      "Dîner d'équipe mercredi prochain : réserver un restaurant pour 12 personnes, moins de 35 € par tête, près du bureau, avec un salon privé si possible.",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    status: "À faire",
    queued: "En file d'attente",
    running: "En cours",
    firstStep: "Lire la description et la discussion de la tâche",
    captions: {
      idle: "Décrivez le travail en une ou deux phrases ; un agent en fait une tâche.",
      actor: "La demande est rédigée. Créé par reprend l'agent choisi la dernière fois ; cliquez dessus pour en changer.",
      picking: "Choisissez Assistant ops. Un squad convient aussi : son chef rédige la tâche.",
      ready: "Le projet est repris de la page depuis laquelle vous avez ouvert la fenêtre. Cliquez sur Créer.",
      sending: "Envoi de la demande à Assistant ops…",
      waiting: "Assistant ops rédige en arrière-plan, la fenêtre est donc déjà fermée. La boîte de réception vous prévient quand c'est fait.",
      inbox: "La notification est arrivée. Ouvrez la nouvelle tâche.",
      done: "Assistant ops a écrit le titre et la description, et s'est laissé comme assigné. La tâche est À faire, il commence donc à la traiter.",
    },
  },
};

type Phase = "idle" | "actor" | "picking" | "ready" | "sending" | "waiting" | "inbox" | "done";

export function IssueDraftDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("actor");
  // The picker opens on the agent used last time; the scenario switches to the first one.
  const [actorIndex, setActorIndex] = useState(1);
  const [queued, setQueued] = useState(true);

  const shownPhase: Phase = visible ? phase : "idle";
  const actor = t.actors[actorIndex]!;
  const dialogOpen = phase === "actor" || phase === "picking" || phase === "ready" || phase === "sending";

  const pick = (index: number) => {
    setActorIndex(index);
    setPhase("ready");
  };

  const create = () => {
    setPhase("sending");
    after(800, () => setPhase("waiting"));
    after(3200, () => setPhase("inbox"));
  };

  const open = () => {
    setPhase("done");
    after(1100, () => setQueued(false));
  };

  const replay = () => {
    clear();
    setPhase("actor");
    setActorIndex(1);
    setQueued(true);
  };

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-96 flex-col bg-muted/30 p-3 sm:p-4">
        {dialogOpen && (
          <div
            role="dialog"
            aria-label={t.breadcrumb}
            className="flex flex-1 flex-col rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:fade-in"
          >
            <div className="flex items-center justify-between gap-2 px-4 pt-3 pb-2">
              <div className="flex min-w-0 items-center gap-1.5 text-caption">
                <span className="truncate text-muted-foreground">{t.workspace}</span>
                <ChevronRight aria-hidden className="size-3 shrink-0 text-muted-foreground" />
                <span className="truncate font-medium">{t.breadcrumb}</span>
              </div>
              <div className="flex shrink-0 items-center gap-1 text-muted-foreground">
                <button type="button" disabled aria-label={t.expand} className="rounded-sm p-1 opacity-60">
                  <Maximize2 className="size-3.5" />
                </button>
                <button type="button" disabled aria-label={t.close} className="rounded-sm p-1 opacity-60">
                  <X className="size-3.5" />
                </button>
              </div>
            </div>

            <div className="relative px-4 pb-2">
              <CoachMark active={shownPhase === "actor"} side="right" ringClassName="rounded-md">
                <button
                  type="button"
                  aria-haspopup="listbox"
                  aria-expanded={phase === "picking"}
                  disabled={!visible || (phase !== "actor" && phase !== "picking")}
                  onClick={() => setPhase(phase === "picking" ? "actor" : "picking")}
                  className="-mx-1 inline-flex h-7 items-center gap-2 rounded-md px-1 text-caption text-muted-foreground hover:bg-muted disabled:hover:bg-transparent"
                >
                  <span>{t.createdBy}</span>
                  <span className="flex items-center gap-1.5 text-label text-foreground">
                    <ActorAvatar actor={actor} />
                    {actor.name}
                  </span>
                  {(phase === "actor" || phase === "picking") && (
                    <ChevronDown aria-hidden className="size-3.5" />
                  )}
                </button>
              </CoachMark>
              {phase === "picking" && (
                <div
                  role="listbox"
                  aria-label={t.createdBy}
                  className="absolute top-full left-3 z-10 mt-1 w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
                >
                  {[false, true].map((squad) => (
                    <div key={String(squad)}>
                      <div className="px-2 pt-1 pb-0.5 text-micro text-muted-foreground">
                        {squad ? t.squadsGroup : t.agentsGroup}
                      </div>
                      {t.actors.map((candidate, i) =>
                        candidate.squad === squad ? (
                          <CoachMark
                            key={candidate.name}
                            active={i === 0}
                            side="right"
                            ringClassName="rounded-md"
                            className="flex w-full"
                          >
                            <button
                              type="button"
                              role="option"
                              aria-selected={i === actorIndex}
                              disabled={i !== 0}
                              onClick={() => pick(i)}
                              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:opacity-50 disabled:hover:bg-transparent"
                            >
                              <ActorAvatar actor={candidate} />
                              <span className="min-w-0 truncate">{candidate.name}</span>
                            </button>
                          </CoachMark>
                        ) : null,
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>

            <p className="min-h-20 flex-1 px-4 pb-3 text-body">{t.prompt}</p>

            <div className="flex flex-wrap items-center gap-1.5 px-3 pb-2">
              <span
                aria-label={t.projectLabel}
                className="inline-flex h-6 items-center gap-1 rounded-full border px-2 text-caption"
              >
                <FolderKanban aria-hidden className="size-3.5 text-muted-foreground" />
                {t.project}
              </span>
              <button
                type="button"
                disabled
                aria-label={t.moreFields}
                className="inline-flex h-6 items-center rounded-full border px-1.5 text-muted-foreground opacity-60"
              >
                <MoreHorizontal className="size-3.5" />
              </button>
            </div>

            <div className="flex flex-wrap items-center gap-x-2 gap-y-2 border-t px-3 py-2.5">
              <button
                type="button"
                disabled
                aria-label={t.attach}
                className="mr-auto rounded-md p-1 text-muted-foreground opacity-60"
              >
                <Paperclip className="size-3.5" />
              </button>
              <button
                type="button"
                disabled
                className="inline-flex items-center gap-1.5 rounded-sm px-2 py-1 text-caption text-muted-foreground opacity-60"
              >
                <ArrowLeftRight aria-hidden className="size-3.5" />
                {t.switchToManual}
              </button>
              <span className="inline-flex items-center gap-1.5 text-caption text-muted-foreground opacity-60">
                <span aria-hidden className="inline-flex h-3.5 w-6 items-center rounded-full bg-input px-0.5">
                  <span className="size-2.5 rounded-full bg-background shadow-sm" />
                </span>
                {t.createAnother}
              </span>
              <CoachMark active={shownPhase === "ready"} side="top" align="end" ringClassName="rounded-lg" className="ml-auto">
                <Button
                  type="button"
                  size="sm"
                  className="min-w-20"
                  disabled={phase !== "ready"}
                  onClick={create}
                >
                  {phase === "sending" ? t.sending : t.submit}
                </Button>
              </CoachMark>
            </div>
          </div>
        )}

        {(phase === "waiting" || phase === "inbox") && (
          <div className="flex flex-1 flex-col rounded-xl border bg-background motion-safe:animate-in motion-safe:fade-in">
            <div className="border-b px-3 py-2 text-label font-medium">{t.inbox}</div>
            <div className="flex flex-col gap-1 p-2">
              {phase === "inbox" ? (
                <CoachMark active side="bottom" align="start" ringClassName="rounded-md" className="flex w-full">
                  <button
                    type="button"
                    onClick={open}
                    className="flex w-full items-start gap-2 rounded-md bg-brand/5 px-2 py-2 text-left hover:bg-accent motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-top-1"
                  >
                    <DemoAvatar name={t.actors[0]!.name} agent />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
                        <span className="truncate font-medium text-foreground">{t.actors[0]!.name}</span>
                        <span className="truncate">· {t.notificationType}</span>
                      </span>
                      <span className="mt-0.5 block text-body break-words">
                        <span className="text-muted-foreground">{t.issueId}</span> {t.issueTitle}
                      </span>
                    </span>
                    <span aria-hidden className="mt-1.5 size-1.5 shrink-0 rounded-full bg-brand" />
                  </button>
                </CoachMark>
              ) : (
                <div aria-hidden className="flex flex-col gap-2 px-2 py-2">
                  <span className="h-2.5 w-2/3 rounded-full bg-muted" />
                  <span className="h-2.5 w-1/2 rounded-full bg-muted" />
                </div>
              )}
            </div>
          </div>
        )}

        {phase === "done" && (
          <div className="flex flex-1 flex-col gap-3 rounded-xl border bg-background p-4 motion-safe:animate-in motion-safe:fade-in">
            <div>
              <div className="text-caption text-muted-foreground">{t.issueId}</div>
              <div className="mt-0.5 text-title-sm font-medium break-words">{t.issueTitle}</div>
              <p className="mt-1.5 text-body text-muted-foreground">{t.issueDescription}</p>
            </div>
            <dl className="grid w-fit grid-cols-[auto_minmax(0,1fr)] items-center gap-x-6 gap-y-1.5 text-body">
              <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
              <dd className="flex items-center gap-1.5">
                <span aria-hidden className="size-3 rounded-full border-2 border-muted-foreground" />
                {t.status}
              </dd>
              <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
              <dd className="flex min-w-0 items-center gap-1.5">
                <DemoAvatar name={t.actors[0]!.name} agent />
                <span className="truncate">{t.actors[0]!.name}</span>
              </dd>
              <dt className="text-caption text-muted-foreground">{t.projectLabel}</dt>
              <dd className="flex min-w-0 items-center gap-1.5">
                <FolderKanban aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="truncate">{t.project}</span>
              </dd>
            </dl>
            <DemoRun agent={t.actors[0]!.name} status={queued ? t.queued : t.running} active>
              {!queued && <DemoStep state="running">{t.firstStep}</DemoStep>}
            </DemoRun>
          </div>
        )}

        {phase === "waiting" && (
          <div
            className={cn(
              "pointer-events-none absolute inset-x-3 bottom-3 rounded-lg border bg-popover px-3 py-2 text-caption shadow-md sm:inset-x-auto sm:right-4 sm:max-w-80",
              "motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1",
            )}
          >
            {t.toastSent}
          </div>
        )}
      </div>
    </DemoFrame>
  );
}

function ActorAvatar({ actor }: { actor: Actor }) {
  return actor.squad ? (
    <span
      aria-hidden
      className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-muted text-foreground"
    >
      <Users className="size-3" />
    </span>
  ) : (
    <DemoAvatar name={actor.name} agent />
  );
}
