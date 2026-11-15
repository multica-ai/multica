"use client";

import { useState } from "react";
import { ChevronRight, CircleCheck, Loader2, Zap } from "@multica/ui/icons";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  DemoRun,
  DemoSendButton,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Action = { name: string; agent: string; description: string };

const copy = {
  en: {
    title: "OPS-31 · Prepare the November customer appreciation event",
    you: "You",
    assigneeLabel: "Assignee",
    assignee: "Lei Li",
    earlier: {
      author: "Lei Li",
      body: "The venue and catering are booked. The guest list is still missing 20 people from marketing.",
    },
    section: "Quick Actions",
    actions: [
      { name: "Weekly-meeting update", agent: "Ops helper", description: "Sum up progress for the weekly meeting" },
      { name: "Draft an invitation", agent: "Writing helper", description: "Write a guest invitation" },
    ] as Action[],
    note: "/note",
    noteDescription: "Add a note — won't trigger any agents",
    prompt:
      "Turn this issue's progress so far into a weekly-meeting update: what's done, what's in progress and what needs help, a few lines each. Reply here.",
    slash: "/ Quick action",
    willStart: "Will start when sent",
    send: "Send",
    started: (name: string) => `${name} started working`,
    running: "Running",
    completed: "Completed",
    steps: ["Read the issue description and discussion", "Collect what's done and what's open", "Write the update"],
    reply:
      "Weekly-meeting update. Done: venue and catering booked. In progress: guest list. Needs help: marketing still owes 20 names.",
    captions: {
      idle: "An issue with a discussion already under way.",
      start:
        "Type / in the comment box to pick a saved action. Or click an action in the sidebar: it posts right away.",
      pick: "These are the workspace's quick actions. Pick Weekly-meeting update.",
      draft:
        "The saved request is in the draft, with @Ops helper in front of it. You can still edit it. Click send.",
      runningDraft: "Sent as your comment. Ops helper starts working on it.",
      runningSidebar: "The sidebar action posted the request at once, without a draft. Ops helper starts working on it.",
      done: "Done. The assignee and the status did not change. Use Replay to try the other entry point.",
    },
  },
  zh: {
    title: "OPS-31 · 筹备 11 月客户答谢会",
    you: "你",
    assigneeLabel: "负责人",
    assignee: "李雷",
    earlier: {
      author: "李雷",
      body: "场地和餐饮已经订好，邀请名单还差市场部的 20 人。",
    },
    section: "快捷操作",
    actions: [
      { name: "整理成周会汇报", agent: "运营助手", description: "把进展整理成周会汇报" },
      { name: "起草邀请函", agent: "文案助手", description: "给来宾写一封邀请函" },
    ] as Action[],
    note: "/note",
    noteDescription: "添加备注——不触发任何智能体",
    prompt: "把这条任务目前的进展整理成周会汇报：已完成、进行中、需要协调的事项各列几条，回复在这里。",
    slash: "/ 快捷操作",
    willStart: "发送后开始",
    send: "发送",
    started: (name: string) => `${name} 已开始处理`,
    running: "运行中",
    completed: "已完成",
    steps: ["读取任务描述和讨论", "汇总已完成和未完成的事项", "写成周会汇报"],
    reply: "周会汇报：已完成，场地和餐饮已订好；进行中，邀请名单；需要协调，市场部还差 20 人的名单。",
    captions: {
      idle: "一条已经有讨论的任务。",
      start: "在评论框输入 / 选择保存好的操作。也可以直接点侧栏里的操作，它会立刻发布。",
      pick: "这里列出工作区的快捷操作。选择\"整理成周会汇报\"。",
      draft: "保存好的请求连同 @运营助手 放进了草稿，发送前还能修改。点击发送。",
      runningDraft: "已作为你的评论发布，运营助手开始处理。",
      runningSidebar: "侧栏里的操作不经过草稿，点击后立即发布，运营助手开始处理。",
      done: "处理完成，负责人和状态都没有变。点右上角可以换另一个入口再试。",
    },
  },
  ja: {
    title: "OPS-31 · 11 月の顧客感謝イベントを準備する",
    you: "あなた",
    assigneeLabel: "担当者",
    assignee: "李雷",
    earlier: {
      author: "李雷",
      body: "会場とケータリングは予約済みです。招待リストはマーケティング部の 20 人分がまだ足りません。",
    },
    section: "クイックアクション",
    actions: [
      {
        name: "週次ミーティング用にまとめる",
        agent: "運用アシスタント",
        description: "進捗を週次ミーティングの報告にまとめる",
      },
      { name: "招待状を下書き", agent: "ライティングアシスタント", description: "来賓向けの招待状を書く" },
    ] as Action[],
    note: "/note",
    noteDescription: "メモを追加 — エージェントをトリガーしません",
    prompt:
      "このタスクのこれまでの進捗を週次ミーティングの報告にまとめてください。完了したこと、進行中のこと、調整が必要なことをそれぞれ数行で。返信はここにお願いします。",
    slash: "/ クイックアクション",
    willStart: "送信後に作業を開始します",
    send: "送信",
    started: (name: string) => `${name} が作業を開始しました`,
    running: "実行中",
    completed: "完了",
    steps: ["タスクの説明とコメントを読む", "完了した項目と残っている項目を集める", "報告を書く"],
    reply:
      "週次報告です。完了：会場とケータリングの予約。進行中：招待リスト。要調整：マーケティング部の 20 人分の名前がまだ届いていません。",
    captions: {
      idle: "すでにコメントのやり取りが進んでいるタスクです。",
      start:
        "コメント欄に / を入力すると、保存済みのアクションを選べます。サイドバーのアクションをクリックした場合は、その場で投稿されます。",
      pick: "ワークスペースのクイックアクションが並んでいます。「週次ミーティング用にまとめる」を選びます。",
      draft:
        "保存済みの依頼が @運用アシスタント 付きで下書きに入りました。送信前に編集できます。送信をクリックします。",
      runningDraft: "あなたのコメントとして投稿され、運用アシスタントが作業を始めます。",
      runningSidebar: "サイドバーのアクションは下書きを経由せず、すぐに依頼を投稿しました。運用アシスタントが作業を始めます。",
      done: "完了しました。担当者もステータスも変わっていません。「もう一度」で別の入口も試せます。",
    },
  },
  ko: {
    title: "OPS-31 · 11월 고객 감사 행사 준비",
    you: "나",
    assigneeLabel: "담당자",
    assignee: "리레이",
    earlier: {
      author: "리레이",
      body: "장소와 케이터링은 예약했습니다. 초대 명단에는 아직 마케팅팀 20명이 빠져 있어요.",
    },
    section: "퀵 액션",
    actions: [
      { name: "주간 회의 보고로 정리", agent: "운영 도우미", description: "진행 상황을 주간 회의 보고로 정리" },
      { name: "초대장 초안 작성", agent: "글쓰기 도우미", description: "손님에게 보낼 초대장 작성" },
    ] as Action[],
    note: "/note",
    noteDescription: "메모 추가 — 에이전트를 트리거하지 않음",
    prompt:
      "이 태스크의 지금까지 진행 상황을 주간 회의 보고로 정리해 주세요. 완료된 일, 진행 중인 일, 조율이 필요한 일을 각각 몇 줄씩 쓰고, 답변은 여기에 남겨 주세요.",
    slash: "/ 퀵 액션",
    willStart: "전송 후 작업을 시작합니다",
    send: "보내기",
    started: (name: string) => `${name}이(가) 작업을 시작했습니다`,
    running: "실행 중",
    completed: "완료됨",
    steps: ["태스크 설명과 댓글 읽기", "완료된 일과 남은 일 정리", "보고 작성"],
    reply:
      "주간 회의 보고입니다. 완료: 장소와 케이터링 예약. 진행 중: 초대 명단. 조율 필요: 마케팅팀 20명의 명단이 아직 없습니다.",
    captions: {
      idle: "이미 댓글로 논의가 진행 중인 태스크입니다.",
      start: "댓글 입력란에 / 를 입력하면 저장된 액션을 고를 수 있습니다. 사이드바의 액션을 클릭하면 바로 게시됩니다.",
      pick: "워크스페이스의 퀵 액션 목록입니다. '주간 회의 보고로 정리'를 선택하세요.",
      draft: "저장된 요청이 @운영 도우미와 함께 초안에 들어갔습니다. 보내기 전에 고칠 수 있습니다. 보내기를 클릭하세요.",
      runningDraft: "내 댓글로 게시되었고, 운영 도우미가 작업을 시작합니다.",
      runningSidebar: "사이드바의 액션은 초안 없이 바로 요청을 게시했습니다. 운영 도우미가 작업을 시작합니다.",
      done: "완료되었습니다. 담당자와 상태는 바뀌지 않았습니다. '다시 보기'로 다른 진입점도 해 보세요.",
    },
  },
  fr: {
    title: "OPS-31 · Préparer l'événement clients de novembre",
    you: "Vous",
    assigneeLabel: "Assigné à",
    assignee: "Lei Li",
    earlier: {
      author: "Lei Li",
      body: "Le lieu et le traiteur sont réservés. Il manque encore 20 personnes du marketing sur la liste des invités.",
    },
    section: "Actions rapides",
    actions: [
      {
        name: "Point pour la réunion hebdo",
        agent: "Assistant ops",
        description: "Résumer l'avancement pour la réunion hebdo",
      },
      { name: "Rédiger une invitation", agent: "Assistant rédaction", description: "Écrire une invitation pour les invités" },
    ] as Action[],
    note: "/note",
    noteDescription: "Ajouter une note — ne déclenchera aucun agent",
    prompt:
      "Faites le point sur l'avancement de cette tâche pour la réunion hebdo : ce qui est fait, ce qui est en cours et ce qui demande de l'aide, quelques lignes chacun. Répondez ici.",
    slash: "/ Action rapide",
    willStart: "Démarrera à l'envoi",
    send: "Envoyer",
    started: (name: string) => `${name} a commencé à travailler`,
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire la description et la discussion de la tâche",
      "Rassembler ce qui est fait et ce qui reste",
      "Rédiger le point",
    ],
    reply:
      "Point pour la réunion hebdo. Fait : lieu et traiteur réservés. En cours : liste des invités. Besoin d'aide : le marketing doit encore fournir 20 noms.",
    captions: {
      idle: "Une tâche dont la discussion a déjà commencé.",
      start:
        "Tapez / dans la zone de commentaire pour choisir une action enregistrée. Ou cliquez sur une action dans la barre latérale : elle est publiée aussitôt.",
      pick: "Voici les actions rapides de l'espace de travail. Choisissez « Point pour la réunion hebdo ».",
      draft:
        "La demande enregistrée est dans le brouillon, précédée de @Assistant ops. Vous pouvez encore la modifier. Cliquez sur envoyer.",
      runningDraft: "Publiée comme votre commentaire. Assistant ops commence à la traiter.",
      runningSidebar:
        "L'action de la barre latérale a publié la demande aussitôt, sans brouillon. Assistant ops commence à la traiter.",
      done: "Terminé. Ni l'assigné ni le statut n'ont changé. Utilisez Rejouer pour essayer l'autre point d'entrée.",
    },
  },
};

type Phase = "idle" | "start" | "pick" | "draft" | "runningDraft" | "runningSidebar" | "done";

export function QuickActionDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");
  const [posting, setPosting] = useState(false);
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";
  const action = t.actions[0]!;
  const posted = phase === "runningDraft" || phase === "runningSidebar" || phase === "done";
  const runActive = phase !== "done";

  const startRun = () => {
    t.steps.forEach((_, i) => after(600 + i * 1000, () => setStepsShown(i + 1)));
    after(600 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const send = () => {
    setPhase("runningDraft");
    startRun();
  };

  const runFromSidebar = () => {
    setPosting(true);
    after(500, () => {
      setPosting(false);
      setPhase("runningSidebar");
      startRun();
    });
  };

  const replay = () => {
    clear();
    setPhase("start");
    setPosting(false);
    setStepsShown(0);
  };

  const mention = <span className="font-medium text-brand">@{action.agent}</span>;
  const requestBody = (
    <>
      {mention}
      <span className="mt-1 block">{t.prompt}</span>
    </>
  );

  const sidebar = (
    <div className="sm:w-44 sm:shrink-0">
      <div className="mb-1 flex items-center gap-1 px-2 py-1 text-caption font-medium">
        {t.section}
        <ChevronRight aria-hidden className="size-3 rotate-90 text-muted-foreground" />
      </div>
      <div className="flex flex-col gap-0.5 pl-2">
        {t.actions.map((item, i) => (
          <button
            key={item.name}
            type="button"
            disabled={!visible || phase !== "start" || posting || i !== 0}
            onClick={runFromSidebar}
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent/70 disabled:cursor-default disabled:hover:bg-transparent"
          >
            {posting && i === 0 ? (
              <Loader2 aria-hidden className="size-3.5 shrink-0 text-muted-foreground motion-safe:animate-spin" />
            ) : (
              <Zap aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
            )}
            <span className="min-w-0 flex-1 truncate">{item.name}</span>
            <DemoAvatar name={item.agent} agent />
          </button>
        ))}
      </div>
    </div>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-80 flex-col gap-4 p-4 sm:flex-row-reverse">
        {sidebar}

        <div className="flex min-w-0 flex-1 flex-col gap-3">
          <div className="flex items-center gap-2 text-caption text-muted-foreground">
            <span>{t.assigneeLabel}</span>
            <DemoAvatar name={t.assignee} />
            <span className="text-label text-foreground">{t.assignee}</span>
          </div>

          <DemoComment author={t.earlier.author}>{t.earlier.body}</DemoComment>

          {posted && (
            <>
              <DemoComment author={t.you}>{requestBody}</DemoComment>
              <DemoRun agent={action.agent} status={runActive ? t.running : t.completed} active={runActive}>
                {t.steps.slice(0, stepsShown).map((step, i) => (
                  <DemoStep key={step} state={i === stepsShown - 1 && runActive ? "running" : "done"}>
                    {step}
                  </DemoStep>
                ))}
              </DemoRun>
            </>
          )}

          {phase === "done" && (
            <DemoComment author={action.agent} agent>
              {t.reply}
            </DemoComment>
          )}

          <div className="relative mt-auto rounded-lg border bg-background p-2">
            {phase === "pick" && (
              <div
                role="listbox"
                aria-label={t.section}
                className="absolute bottom-full left-2 z-10 mb-1 w-64 max-w-[calc(100%-1rem)] rounded-md border bg-popover py-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                {[...t.actions.map((a) => ({ label: `/${a.name}`, description: a.description })), { label: t.note, description: t.noteDescription }].map(
                  (item, i) => (
                    <CoachMark
                      quiet
                      key={item.label}
                      active={i === 0}
                      ringClassName="rounded-md"
                      className="flex w-full"
                    >
                      <button
                        type="button"
                        role="option"
                        aria-selected={false}
                        disabled={i !== 0}
                        onClick={() => setPhase("draft")}
                        className="flex w-full flex-col gap-0.5 px-3 py-1.5 text-left text-caption hover:bg-accent disabled:opacity-50 disabled:hover:bg-transparent"
                      >
                        <span className="font-medium">{item.label}</span>
                        <span className="truncate text-muted-foreground">{item.description}</span>
                      </button>
                    </CoachMark>
                  ),
                )}
              </div>
            )}

            <div className="min-h-12 px-1 text-body">
              {visible && (phase === "start" || phase === "pick") && (
                <CoachMark active={phase === "start"} side="right" ringClassName="rounded-md">
                  <button
                    type="button"
                    disabled={phase !== "start"}
                    onClick={() => setPhase("pick")}
                    className="rounded-md border border-dashed border-brand/50 px-1.5 text-label font-medium text-brand hover:bg-brand/7 disabled:opacity-70"
                  >
                    {t.slash}
                  </button>
                </CoachMark>
              )}
              {phase === "draft" && <div className="motion-safe:animate-in motion-safe:fade-in">{requestBody}</div>}
            </div>

            <div className="mt-1.5 flex items-center gap-2">
              {phase === "draft" && (
                <span className="inline-flex h-6 min-w-0 items-center gap-1.5 rounded-md bg-muted px-1.5 text-caption motion-safe:animate-in motion-safe:fade-in">
                  <DemoAvatar name={action.agent} agent />
                  <span className="truncate">{action.agent}</span>
                  <span className="text-muted-foreground">· {t.willStart}</span>
                </span>
              )}
              <span className="ml-auto" />
              <CoachMark active={phase === "draft"} side="left" ringClassName="rounded-full">
                <DemoSendButton label={t.send} disabled={phase !== "draft"} onClick={send} />
              </CoachMark>
            </div>
          </div>
        </div>

        {phase === "runningSidebar" && (
          <div
            className="absolute right-4 bottom-4 z-20 flex items-center gap-2 rounded-lg border bg-popover px-3 py-2 text-caption text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2"
          >
            <CircleCheck aria-hidden className="size-3.5 text-success" />
            {t.started(action.agent)}
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
