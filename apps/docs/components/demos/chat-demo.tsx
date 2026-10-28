"use client";

import { type ReactNode, useState } from "react";
import { CircleDashed, Loader2 } from "lucide-react";
import { CoachMark, DemoAvatar, DemoFrame, DemoSendButton, useDemoCopy, useDemoTimers, useStartWhenVisible } from "./demo-kit";

type Suggestion = { label: string; prompt: string };
type DueIssue = { id: string; title: string; due: string; assignee: string };

const copy = {
  en: {
    title: "Chat · Ops helper",
    agent: "Ops helper",
    heading: "Chat with Ops helper",
    description: "Handles office and operations chores: checks progress, plans schedules, files issues.",
    startersLabel: "Conversation starters",
    starters: [
      {
        label: "What's due this week?",
        prompt: "List the issues due this week that aren't done yet, sorted by due date, with their assignees.",
      },
      { label: "Sum up yesterday's customer feedback", prompt: "Sum up yesterday's customer feedback in five points." },
      { label: "Plan my day", prompt: "Look at my open issues and suggest an order for today." },
    ] as Suggestion[],
    placeholder: "Message Ops helper...",
    send: "Send",
    thinking: "Thinking",
    runningCommand: "Running a command",
    repliedIn: (elapsed: string) => `Replied in ${elapsed}`,
    replyTimes: ["12s", "9s"],
    dueIntro: "Three issues are due this week and none is done yet:",
    due: [
      { id: "OPS-32", title: "Buy gifts for the annual party", due: "Wed", assignee: "Lei Li" },
      { id: "OPS-33", title: "Update the staff directory", due: "Thu", assignee: "You" },
      { id: "OPS-34", title: "Submit Q3 expense claims", due: "Fri", assignee: "Lei Li" },
    ] as DueIssue[],
    dueOutro: "OPS-34 is still missing two invoices.",
    followUpsHeading: "Follow-up questions",
    followUps: [
      {
        label: "Create an issue to chase the missing invoices",
        prompt:
          "Create an issue: collect the two invoices OPS-34 is missing, assign it to me, due this Thursday.",
      },
      { label: "Which issues are overdue?", prompt: "Which issues are overdue?" },
    ] as Suggestion[],
    createdReply: "Done. I created the issue, assigned it to you and set it due Thursday:",
    created: { id: "OPS-35", title: "Collect the missing invoices for OPS-34", status: "Todo" },
    captions: {
      idle: "A new chat with an agent. Nobody else can see it.",
      start: "Before the first message, the agent's suggestions are shown. Pick the first one.",
      filled: "The suggestion went into the composer and was not sent, so you can adjust it first. Click send.",
      replying: "The agent looks this up in the workspace with the Multica CLI. The chat isn't tied to any issue.",
      replied:
        "The agent answered without creating anything. Click the follow-up to ask it to create an issue: a follow-up is sent right away.",
      creating: "You asked it explicitly, so the agent acts with your workspace access and creates the issue.",
      done: "The issue is created, and the chat stays private to you. Use Replay to watch again.",
    },
  },
  zh: {
    title: "对话 · 运营助手",
    agent: "运营助手",
    heading: "与 运营助手 对话",
    description: "负责行政和运营杂务：查进度、排日程、建任务。",
    startersLabel: "对话开场建议",
    starters: [
      { label: "这周有哪些任务要到期？", prompt: "列出这周到期、还没完成的任务，按截止日期排序，并注明负责人。" },
      { label: "总结昨天的客户反馈", prompt: "把昨天的客户反馈总结成五条要点。" },
      { label: "规划我的今天", prompt: "看看我手上还没完成的任务，建议今天按什么顺序处理。" },
    ] as Suggestion[],
    placeholder: "给 运营助手 发消息...",
    send: "发送",
    thinking: "思考中",
    runningCommand: "执行命令",
    repliedIn: (elapsed: string) => `${elapsed} 内回复`,
    replyTimes: ["12s", "9s"],
    dueIntro: "这周有 3 个任务到期，都还没完成：",
    due: [
      { id: "OPS-32", title: "采购年会礼品", due: "周三", assignee: "李雷" },
      { id: "OPS-33", title: "更新员工通讯录", due: "周四", assignee: "你" },
      { id: "OPS-34", title: "提交三季度报销", due: "周五", assignee: "李雷" },
    ] as DueIssue[],
    dueOutro: "其中 OPS-34 还缺两张发票。",
    followUpsHeading: "后续提问",
    followUps: [
      {
        label: "建一个任务去催缺的发票",
        prompt: "帮我建一个任务：催齐 OPS-34 缺的两张发票，负责人设为我，本周四截止。",
      },
      { label: "哪些任务已经逾期？", prompt: "哪些任务已经逾期？" },
    ] as Suggestion[],
    createdReply: "已经建好，负责人是你，周四截止：",
    created: { id: "OPS-35", title: "催齐 OPS-34 缺的发票", status: "待办" },
    captions: {
      idle: "和智能体新开的一段对话，只有你能看到。",
      start: "发送第一条消息前，这里列出智能体的开场建议。选择第一条。",
      filled: "建议填进了输入框，没有发送，可以先改。点击发送。",
      replying: "智能体用 Multica CLI 查询工作区。这段对话不绑定任何任务。",
      replied: "智能体只回答了问题，没有建任何东西。点击后续提问请它建任务，后续提问点了就会发送。",
      creating: "你明确提出了要求，智能体以你的工作区权限代你建任务。",
      done: "任务建好了，这段对话仍然只有你能看到。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "チャット · 運用アシスタント",
    agent: "運用アシスタント",
    heading: "運用アシスタント とチャット",
    description: "総務と運用の雑務を担当します。進捗の確認、予定の調整、タスクの作成など。",
    startersLabel: "会話の開始例",
    starters: [
      {
        label: "今週が期限のタスクは？",
        prompt: "今週が期限でまだ完了していないタスクを、期限の早い順に、担当者も添えて一覧にしてください。",
      },
      { label: "昨日の顧客フィードバックをまとめる", prompt: "昨日の顧客フィードバックを 5 つの要点にまとめてください。" },
      { label: "今日の段取りを決める", prompt: "私の未完了のタスクを見て、今日取りかかる順番を提案してください。" },
    ] as Suggestion[],
    placeholder: "運用アシスタント にメッセージ…",
    send: "送信",
    thinking: "考え中",
    runningCommand: "コマンドを実行中",
    repliedIn: (elapsed: string) => `${elapsed}で返信`,
    replyTimes: ["12s", "9s"],
    dueIntro: "今週が期限のタスクは 3 件で、どれもまだ完了していません：",
    due: [
      { id: "OPS-32", title: "年次パーティーの記念品を購入", due: "水", assignee: "李雷" },
      { id: "OPS-33", title: "社員名簿を更新", due: "木", assignee: "あなた" },
      { id: "OPS-34", title: "第 3 四半期の経費精算を提出", due: "金", assignee: "李雷" },
    ] as DueIssue[],
    dueOutro: "OPS-34 はまだ領収書が 2 枚足りません。",
    followUpsHeading: "フォローアップ質問",
    followUps: [
      {
        label: "足りない領収書を集めるタスクを作成",
        prompt: "タスクを作成してください：OPS-34 で足りない領収書 2 枚を集める。担当は私、期限は今週の木曜日。",
      },
      { label: "期限を過ぎたタスクは？", prompt: "期限を過ぎたタスクは？" },
    ] as Suggestion[],
    createdReply: "作成しました。担当者はあなた、期限は木曜日です：",
    created: { id: "OPS-35", title: "OPS-34 で足りない領収書を集める", status: "未着手" },
    captions: {
      idle: "エージェントと新しく始めたチャットです。ほかの人には見えません。",
      start: "最初のメッセージを送る前は、エージェントの開始例が表示されます。最初の例を選びます。",
      filled: "開始例は入力欄に入っただけで、送信はされていません。先に手直しできます。送信をクリックします。",
      replying: "エージェントは Multica CLI でワークスペースを調べます。このチャットはどのタスクにも紐づいていません。",
      replied:
        "エージェントは質問に答えただけで、何も作成していません。フォローアップ質問をクリックしてタスクの作成を頼みます。フォローアップ質問はクリックするとすぐ送信されます。",
      creating: "はっきり頼んだので、エージェントはあなたのワークスペース権限でタスクを作成します。",
      done: "タスクができました。チャットは引き続きあなたにしか見えません。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "채팅 · 운영 도우미",
    agent: "운영 도우미",
    heading: "운영 도우미와 채팅",
    description: "사무와 운영 잡무를 맡습니다. 진행 상황 확인, 일정 조율, 태스크 생성 등.",
    startersLabel: "대화 시작 예시",
    starters: [
      {
        label: "이번 주에 마감되는 태스크는?",
        prompt: "이번 주가 마감이고 아직 끝나지 않은 태스크를 마감일 순으로, 담당자와 함께 보여 주세요.",
      },
      { label: "어제 고객 피드백 요약", prompt: "어제 들어온 고객 피드백을 다섯 가지 요점으로 요약해 주세요." },
      { label: "오늘 할 일 순서 정하기", prompt: "내가 맡은 미완료 태스크를 보고 오늘 처리할 순서를 제안해 주세요." },
    ] as Suggestion[],
    placeholder: "운영 도우미에게 메시지 보내기…",
    send: "보내기",
    thinking: "생각 중",
    runningCommand: "명령 실행 중",
    repliedIn: (elapsed: string) => `${elapsed} 만에 답변`,
    replyTimes: ["12s", "9s"],
    dueIntro: "이번 주에 마감되는 태스크가 3개 있고, 모두 아직 완료되지 않았습니다:",
    due: [
      { id: "OPS-32", title: "연례 파티 기념품 구매", due: "수", assignee: "리레이" },
      { id: "OPS-33", title: "직원 연락처 업데이트", due: "목", assignee: "나" },
      { id: "OPS-34", title: "3분기 경비 청구 제출", due: "금", assignee: "리레이" },
    ] as DueIssue[],
    dueOutro: "OPS-34는 아직 영수증 두 장이 빠져 있습니다.",
    followUpsHeading: "후속 질문",
    followUps: [
      {
        label: "빠진 영수증을 받을 태스크 만들기",
        prompt: "태스크를 만들어 주세요: OPS-34에 빠진 영수증 두 장 받기, 담당자는 나, 마감은 이번 주 목요일.",
      },
      { label: "기한이 지난 태스크는?", prompt: "기한이 지난 태스크는?" },
    ] as Suggestion[],
    createdReply: "태스크를 만들어 담당자로 지정했고, 마감은 목요일입니다:",
    created: { id: "OPS-35", title: "OPS-34의 빠진 영수증 받기", status: "할 일" },
    captions: {
      idle: "에이전트와 새로 시작한 채팅입니다. 다른 사람은 볼 수 없습니다.",
      start: "첫 메시지를 보내기 전에는 에이전트의 대화 시작 예시가 표시됩니다. 첫 번째 예시를 선택하세요.",
      filled: "예시는 입력란에 채워졌을 뿐 전송되지 않았으니 먼저 고칠 수 있습니다. 보내기를 클릭하세요.",
      replying: "에이전트가 Multica CLI로 워크스페이스를 조회합니다. 이 채팅은 어떤 태스크에도 묶여 있지 않습니다.",
      replied:
        "에이전트는 질문에 답하기만 했고 아무것도 만들지 않았습니다. 후속 질문을 클릭해 태스크 생성을 요청하세요. 후속 질문은 클릭하면 바로 전송됩니다.",
      creating: "명시적으로 요청했으므로 에이전트가 내 워크스페이스 권한으로 태스크를 만듭니다.",
      done: "태스크가 만들어졌고, 채팅은 여전히 나만 볼 수 있습니다. '다시 보기'로 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Discussion · Assistant ops",
    agent: "Assistant ops",
    heading: "Discuter avec Assistant ops",
    description: "S'occupe des tâches administratives et opérationnelles : suivi d'avancement, planning, création de tâches.",
    startersLabel: "Amorces de conversation",
    starters: [
      {
        label: "Qu'est-ce qui arrive à échéance cette semaine ?",
        prompt:
          "Listez les tâches à échéance cette semaine qui ne sont pas terminées, triées par date d'échéance, avec leurs assignés.",
      },
      { label: "Résumer les retours clients d'hier", prompt: "Résumez les retours clients d'hier en cinq points." },
      { label: "Planifier ma journée", prompt: "Regardez mes tâches en cours et proposez un ordre pour aujourd'hui." },
    ] as Suggestion[],
    placeholder: "Écrire à Assistant ops…",
    send: "Envoyer",
    thinking: "Réflexion",
    runningCommand: "Exécution d'une commande",
    repliedIn: (elapsed: string) => `Réponse en ${elapsed}`,
    replyTimes: ["12s", "9s"],
    dueIntro: "Trois tâches arrivent à échéance cette semaine, et aucune n'est terminée :",
    due: [
      { id: "OPS-32", title: "Acheter les cadeaux de la soirée annuelle", due: "mer.", assignee: "Lei Li" },
      { id: "OPS-33", title: "Mettre à jour l'annuaire du personnel", due: "jeu.", assignee: "Vous" },
      { id: "OPS-34", title: "Soumettre les notes de frais du T3", due: "ven.", assignee: "Lei Li" },
    ] as DueIssue[],
    dueOutro: "Il manque encore deux factures pour OPS-34.",
    followUpsHeading: "Questions de suivi",
    followUps: [
      {
        label: "Créer une tâche pour récupérer les factures manquantes",
        prompt:
          "Créez une tâche : récupérer les deux factures qui manquent à OPS-34, assignée à moi, à échéance ce jeudi.",
      },
      { label: "Quelles tâches sont en retard ?", prompt: "Quelles tâches sont en retard ?" },
    ] as Suggestion[],
    createdReply: "C'est fait. J'ai créé la tâche, je vous l'ai assignée, échéance jeudi :",
    created: { id: "OPS-35", title: "Récupérer les factures manquantes d'OPS-34", status: "À faire" },
    captions: {
      idle: "Une nouvelle discussion avec un agent. Personne d'autre ne la voit.",
      start: "Avant le premier message, les suggestions de l'agent s'affichent. Choisissez la première.",
      filled:
        "La suggestion est dans la zone de saisie, pas encore envoyée : vous pouvez l'ajuster d'abord. Cliquez sur envoyer.",
      replying:
        "L'agent consulte l'espace de travail avec la CLI Multica. La discussion n'est liée à aucune tâche.",
      replied:
        "L'agent a répondu sans rien créer. Cliquez sur la question de suivi pour lui demander de créer une tâche : une question de suivi part dès le clic.",
      creating:
        "Vous l'avez demandé explicitement : l'agent agit avec vos droits dans l'espace de travail et crée la tâche.",
      done: "La tâche est créée, et la discussion reste visible de vous seul. Utilisez Rejouer pour revoir la démo.",
    },
  },
};

type Phase = "idle" | "start" | "filled" | "replying" | "replied" | "creating" | "done";
type Pill = "thinking" | "command" | null;

export function ChatDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");
  const [pill, setPill] = useState<Pill>(null);

  const shownPhase: Phase = visible ? phase : "idle";
  const starter = t.starters[0]!;
  const followUp = t.followUps[0]!;
  const asked = phase !== "start" && phase !== "filled";
  const followedUp = phase === "creating" || phase === "done";

  // One agent turn: a short status pill, then the reply lands in `next`.
  const runTurn = (next: Exclude<Phase, "idle">) => {
    setPill("thinking");
    after(900, () => setPill("command"));
    after(2300, () => {
      setPill(null);
      setPhase(next);
    });
  };

  const send = () => {
    setPhase("replying");
    runTurn("replied");
  };

  const askFollowUp = () => {
    setPhase("creating");
    runTurn("done");
  };

  const replay = () => {
    clear();
    setPhase("start");
    setPill(null);
  };

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-96 flex-col gap-3 p-4">
        {!asked ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-4 py-2">
            <div className="max-w-sm space-y-1 text-center">
              <div className="flex justify-center pb-1">
                <span className="inline-flex size-9 items-center justify-center rounded-full bg-brand/12 ring-1 ring-border ring-inset">
                  <DemoAvatar name={t.agent} agent />
                </span>
              </div>
              <div className="text-title-sm font-semibold">{t.heading}</div>
              <p className="text-body text-muted-foreground">{t.description}</p>
            </div>
            <div aria-label={t.startersLabel} className="w-full max-w-sm space-y-2">
              {t.starters.map((item, i) => (
                <CoachMark
                  key={item.label}
                  active={visible && phase === "start" && i === 0}
                  side="right"
                  ringClassName="rounded-lg"
                  className="flex w-full"
                >
                  <button
                    type="button"
                    disabled={!visible || phase !== "start" || i !== 0}
                    onClick={() => setPhase("filled")}
                    className="w-full rounded-lg border bg-card px-3 py-2.5 text-left text-body hover:border-brand/40 hover:bg-accent disabled:opacity-60 disabled:hover:border-border disabled:hover:bg-card"
                  >
                    {item.label}
                  </button>
                </CoachMark>
              ))}
            </div>
          </div>
        ) : (
          <div className="flex flex-col gap-3">
            <UserMessage>{starter.prompt}</UserMessage>
            {phase === "replying" ? (
              <StatusPill pill={pill} t={t} />
            ) : (
              <AgentMessage name={t.agent} meta={t.repliedIn(t.replyTimes[0]!)}>
                <p>{t.dueIntro}</p>
                <ul className="mt-1 flex flex-col gap-0.5">
                  {t.due.map((issue) => (
                    <li key={issue.id} className="flex min-w-0 gap-2">
                      <span className="shrink-0 text-muted-foreground">{issue.id}</span>
                      <span className="min-w-0 truncate">{issue.title}</span>
                      <span className="ml-auto shrink-0 text-caption text-muted-foreground">
                        {issue.due} · {issue.assignee}
                      </span>
                    </li>
                  ))}
                </ul>
                <p className="mt-1">{t.dueOutro}</p>
              </AgentMessage>
            )}

            {phase === "replied" && (
              <div className="flex flex-col gap-1.5 pl-7 motion-safe:animate-in motion-safe:fade-in">
                <div className="text-caption text-muted-foreground">{t.followUpsHeading}</div>
                <div className="flex flex-wrap gap-1.5">
                  {t.followUps.map((item, i) => (
                    <CoachMark
                      quiet
                      key={item.label}
                      active={i === 0}
                      ringClassName="rounded-full"
                    >
                      <button
                        type="button"
                        disabled={i !== 0}
                        onClick={askFollowUp}
                        className="rounded-full border bg-background px-3 py-1 text-caption hover:bg-accent disabled:opacity-60 disabled:hover:bg-background"
                      >
                        {item.label}
                      </button>
                    </CoachMark>
                  ))}
                </div>
              </div>
            )}

            {followedUp && <UserMessage>{followUp.prompt}</UserMessage>}
            {phase === "creating" && <StatusPill pill={pill} t={t} />}
            {phase === "done" && (
              <AgentMessage name={t.agent} meta={t.repliedIn(t.replyTimes[1]!)}>
                <p>{t.createdReply}</p>
                <div className="mt-1.5 flex min-w-0 items-center gap-2 rounded-lg border bg-background px-3 py-2">
                  <CircleDashed aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                  <span className="shrink-0 text-caption text-muted-foreground">{t.created.id}</span>
                  <span className="min-w-0 truncate text-label font-medium">{t.created.title}</span>
                  <span className="ml-auto shrink-0 text-caption text-muted-foreground">{t.created.status}</span>
                </div>
              </AgentMessage>
            )}
          </div>
        )}

        <div className="mt-auto rounded-lg border bg-background p-2">
          <div className="min-h-10 px-1 text-body">
            {phase === "filled" ? (
              <span className="motion-safe:animate-in motion-safe:fade-in">{starter.prompt}</span>
            ) : (
              <span className="text-muted-foreground">{t.placeholder}</span>
            )}
          </div>
          <div className="mt-1.5 flex items-center">
            <span className="ml-auto" />
            <CoachMark active={phase === "filled"} side="left" ringClassName="rounded-full">
              <DemoSendButton label={t.send} disabled={phase !== "filled"} onClick={send} />
            </CoachMark>
          </div>
        </div>
      </div>
    </DemoFrame>
  );
}

function UserMessage({ children }: { children: ReactNode }) {
  return (
    <div className="flex justify-end motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1">
      <div className="max-w-[85%] rounded-2xl bg-muted px-3 py-2 text-body break-words">{children}</div>
    </div>
  );
}

function AgentMessage({ name, meta, children }: { name: string; meta: string; children: ReactNode }) {
  return (
    <div className="min-w-0 motion-safe:animate-in motion-safe:fade-in">
      <div className="flex items-center gap-2">
        <DemoAvatar name={name} agent />
        <span className="text-label font-medium">{name}</span>
        <span className="text-caption text-muted-foreground">{meta}</span>
      </div>
      <div className="mt-1 pl-7 text-body break-words">{children}</div>
    </div>
  );
}

function StatusPill({ pill, t }: { pill: Pill; t: { thinking: string; runningCommand: string } }) {
  if (!pill) return null;
  return (
    <div className="pl-7">
      <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
        <Loader2 aria-hidden className="size-3 text-brand motion-safe:animate-spin" />
        {pill === "thinking" ? t.thinking : t.runningCommand}
      </span>
    </div>
  );
}
