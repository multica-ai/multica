"use client";

import { useState, type ReactNode } from "react";
import { Users } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoFrame,
  DemoRun,
  DemoSendButton,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Status = "todo" | "in_progress";

const copy = {
  en: {
    title: "Ops team group chat · Ops helper connected",
    chatApp: "Chat app",
    multica: "Multica",
    chatName: "Ops team",
    botTag: "Bot",
    you: "You",
    lei: "Lei Li",
    bot: "Ops helper",
    leiMessage: "I'll hand this month's expense reports to finance by Friday.",
    mention: "@Ops helper",
    askText:
      " Team dinner next Wednesday, 12 people, up to 150 per person. What should I confirm before booking a restaurant?",
    askReply:
      "Before booking, confirm four things: dietary restrictions, lunch or dinner, whether the 150 includes drinks, and a private room or the main hall.",
    issueText: " /issue Book a restaurant for next Wednesday's team dinner\n12 people, up to 150 per person, private room needed.",
    created: "✅ Created OPS-21 — Book a restaurant for next Wednesday's team dinner",
    link: ".../issues/OPS-21",
    placeholder: "Message Ops team",
    send: "Send",
    issueTitle: "OPS-21 · Book a restaurant for next Wednesday's team dinner",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    creatorLabel: "Created by",
    status: { todo: "Todo", in_progress: "In Progress" } as Record<Status, string>,
    description: "12 people, up to 150 per person, private room needed.",
    queued: "Queued",
    running: "Running",
    steps: [
      "Read the issue description",
      "Set the status to In Progress",
      "Shortlist restaurants that fit the group size and budget",
    ],
    captions: {
      idle: "A group chat with the Ops helper Bot connected.",
      ask: "In a group, the Bot only handles messages that @ it, so Lei Li's message doesn't start it. Click send.",
      thinking: "The message goes into a Multica chat, and Ops helper starts a run.",
      replied:
        "The reply comes back to this group. Next, record the work as an issue with /issue: the first line is the title, the rest is the description. Click send.",
      creating: "/issue is a command: Multica creates the issue straight from the message and doesn't add it to the chat.",
      created: "The issue is created and assigned to the agent behind this Bot. Click the link to open it.",
      issue: "The issue is assigned to Ops helper and created as Todo. A run starts for it, and the agent moves the issue to In Progress as it begins.",
      done: "Ops helper is working on the issue, and its progress stays on the issue. Use Replay to start over.",
    },
  },
  zh: {
    title: "运营组群聊 · 已接入 运营助手",
    chatApp: "聊天工具",
    multica: "Multica",
    chatName: "运营组",
    botTag: "Bot",
    you: "你",
    lei: "李雷",
    bot: "运营助手",
    leiMessage: "这个月的报销单我周五前交给财务。",
    mention: "@运营助手",
    askText: " 下周三部门聚餐，12 个人，人均 150 元以内。订餐厅前还要确认哪些事？",
    askReply: "订之前建议确认四件事：有没有人忌口，吃午饭还是晚饭，150 元含不含酒水，要包间还是大厅。",
    issueText: " /issue 预订下周三部门聚餐的餐厅\n12 人，人均 150 元以内，需要包间。",
    created: "✅ 已创建 OPS-21 — 预订下周三部门聚餐的餐厅",
    link: ".../issues/OPS-21",
    placeholder: "发送给 运营组",
    send: "发送",
    issueTitle: "OPS-21 · 预订下周三部门聚餐的餐厅",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    creatorLabel: "创建者",
    status: { todo: "待办", in_progress: "进行中" } as Record<Status, string>,
    description: "12 人，人均 150 元以内，需要包间。",
    queued: "排队中",
    running: "运行中",
    steps: ["读取任务描述", "把状态改为 进行中", "按人数和预算筛选餐厅"],
    captions: {
      idle: "一个接入了 运营助手 Bot 的群聊。",
      ask: "群聊里只有 @ Bot 的消息才会交给它，李雷那条不会触发。点击发送。",
      thinking: "消息进入一段 Multica 对话，运营助手 开始一次运行。",
      replied: "回复发回了这个群。接下来用 /issue 把这件事记成任务：第一行是标题，其余是描述。点击发送。",
      creating: "/issue 是命令：Multica 直接按这条消息创建任务，不会把它加入对话。",
      created: "任务已创建，负责人是这个 Bot 绑定的智能体。点击链接打开任务。",
      issue: "任务的负责人是 运营助手，创建时为待办。Multica 为它创建了一次运行，智能体开始工作时把状态改为进行中。",
      done: "运营助手 正在处理这条任务，进展都记在任务里。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "運用チームのグループチャット · 運用アシスタント 接続済み",
    chatApp: "チャットツール",
    multica: "Multica",
    chatName: "運用チーム",
    botTag: "Bot",
    you: "あなた",
    lei: "李雷",
    bot: "運用アシスタント",
    leiMessage: "今月の経費精算は金曜までに経理へ出しておきます。",
    mention: "@運用アシスタント",
    askText:
      " 来週水曜にチームの食事会をします。12 人で、予算は 1 人 5,000 円まで。お店を予約する前に何を確認しておくべき？",
    askReply:
      "予約の前に 4 つ確認しておきましょう：食べられないものがある人はいるか、ランチかディナーか、5,000 円に飲み物代を含むか、個室かフロア席か。",
    issueText: " /issue 来週水曜のチーム食事会のお店を予約する\n12 人、1 人 5,000 円まで、個室希望。",
    created: "✅ OPS-21 を作成しました — 来週水曜のチーム食事会のお店を予約する",
    link: ".../issues/OPS-21",
    placeholder: "運用チーム にメッセージを送信",
    send: "送信",
    issueTitle: "OPS-21 · 来週水曜のチーム食事会のお店を予約する",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    creatorLabel: "作成者",
    status: { todo: "未着手", in_progress: "進行中" } as Record<Status, string>,
    description: "12 人、1 人 5,000 円まで、個室希望。",
    queued: "キュー待ち",
    running: "実行中",
    steps: ["タスクの説明を読む", "ステータスを 進行中 に変更", "人数と予算に合うお店を絞り込む"],
    captions: {
      idle: "運用アシスタント の Bot を接続したグループチャットです。",
      ask: "グループでは Bot を @メンションしたメッセージだけが Bot に渡るので、李雷 のメッセージでは動きません。送信をクリックしてください。",
      thinking: "メッセージが Multica のチャットに入り、運用アシスタント が実行を始めます。",
      replied:
        "返信はこのグループに届きます。次は /issue でこの件をタスクにします。1 行目がタイトル、残りが説明です。送信をクリックしてください。",
      creating: "/issue はコマンドです。Multica はこのメッセージからそのままタスクを作成し、チャットには追加しません。",
      created: "タスクが作成され、担当者はこの Bot に紐づいたエージェントです。リンクをクリックしてタスクを開きます。",
      issue: "担当者は 運用アシスタント、作成時のステータスは 未着手 です。Multica が実行を作成し、エージェントは作業を始めるときにステータスを 進行中 に変更します。",
      done: "運用アシスタント がタスクに取り組んでいて、進捗はタスクに記録されます。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "운영팀 그룹 채팅 · 운영 도우미 연결됨",
    chatApp: "채팅 도구",
    multica: "Multica",
    chatName: "운영팀",
    botTag: "Bot",
    you: "나",
    lei: "리레이",
    bot: "운영 도우미",
    leiMessage: "이번 달 경비 정산서는 금요일까지 재무팀에 낼게요.",
    mention: "@운영 도우미",
    askText: " 다음 주 수요일에 팀 회식이 있어요. 12명이고 1인당 3만 원 이하예요. 식당을 예약하기 전에 뭘 확인해야 할까요?",
    askReply:
      "예약 전에 네 가지를 확인하세요. 못 먹는 음식이 있는 사람이 있는지, 점심인지 저녁인지, 3만 원에 주류가 포함되는지, 룸이 필요한지 홀이면 되는지입니다.",
    issueText: " /issue 다음 주 수요일 팀 회식 식당 예약\n12명, 1인당 3만 원 이하, 룸 필요.",
    created: "✅ OPS-21 생성됨 — 다음 주 수요일 팀 회식 식당 예약",
    link: ".../issues/OPS-21",
    placeholder: "운영팀에 메시지 보내기",
    send: "보내기",
    issueTitle: "OPS-21 · 다음 주 수요일 팀 회식 식당 예약",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    creatorLabel: "작성자",
    status: { todo: "할 일", in_progress: "진행 중" } as Record<Status, string>,
    description: "12명, 1인당 3만 원 이하, 룸 필요.",
    queued: "대기열에 있음",
    running: "실행 중",
    steps: ["태스크 설명 읽기", "상태를 진행 중으로 변경", "인원과 예산에 맞는 식당 추리기"],
    captions: {
      idle: "운영 도우미 Bot이 연결된 그룹 채팅입니다.",
      ask: "그룹에서는 Bot을 @멘션한 메시지만 Bot에게 전달되므로 리레이의 메시지로는 시작되지 않습니다. 보내기를 클릭하세요.",
      thinking: "메시지가 Multica 대화에 들어가고 운영 도우미가 실행을 시작합니다.",
      replied:
        "답변이 이 그룹으로 돌아왔습니다. 이제 /issue로 이 일을 태스크로 남깁니다. 첫 줄은 제목, 나머지는 설명입니다. 보내기를 클릭하세요.",
      creating: "/issue는 명령입니다. Multica는 이 메시지로 바로 태스크를 만들고 대화에는 추가하지 않습니다.",
      created: "태스크가 생성되었고 담당자는 이 Bot에 연결된 에이전트입니다. 링크를 클릭해 태스크를 여세요.",
      issue: "담당자는 운영 도우미이고 태스크는 할 일 상태로 생성되었습니다. Multica가 실행을 만들고, 에이전트는 작업을 시작하면서 상태를 진행 중으로 바꿉니다.",
      done: "운영 도우미가 태스크를 처리하고 있으며 진행 상황은 태스크에 남습니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Groupe Équipe ops · Assistant ops connecté",
    chatApp: "Messagerie",
    multica: "Multica",
    chatName: "Équipe ops",
    botTag: "Bot",
    you: "Vous",
    lei: "Lei Li",
    bot: "Assistant ops",
    leiMessage: "Je transmets les notes de frais du mois à la compta d'ici vendredi.",
    mention: "@Assistant ops",
    askText:
      " Dîner d'équipe mercredi prochain, 12 personnes, 30 € maximum par personne. Que faut-il vérifier avant de réserver un restaurant ?",
    askReply:
      "Avant de réserver, vérifiez quatre points : les restrictions alimentaires, déjeuner ou dîner, si les 30 € incluent les boissons, et salle privée ou salle principale.",
    issueText:
      " /issue Réserver un restaurant pour le dîner d'équipe de mercredi prochain\n12 personnes, 30 € maximum par personne, salle privée nécessaire.",
    created: "✅ OPS-21 créée — Réserver un restaurant pour le dîner d'équipe de mercredi prochain",
    link: ".../issues/OPS-21",
    placeholder: "Écrire dans Équipe ops",
    send: "Envoyer",
    issueTitle: "OPS-21 · Réserver un restaurant pour le dîner d'équipe de mercredi prochain",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    creatorLabel: "Créé par",
    status: { todo: "À faire", in_progress: "En cours" } as Record<Status, string>,
    description: "12 personnes, 30 € maximum par personne, salle privée nécessaire.",
    queued: "En file",
    running: "En cours",
    steps: [
      "Lire la description de la tâche",
      "Passer le statut à En cours",
      "Sélectionner des restaurants adaptés au groupe et au budget",
    ],
    captions: {
      idle: "Une discussion de groupe où le bot Assistant ops est connecté.",
      ask: "Dans un groupe, le bot ne traite que les messages qui le @mentionnent : le message de Lei Li ne le déclenche pas. Cliquez sur Envoyer.",
      thinking: "Le message rejoint une discussion Multica, et Assistant ops lance une exécution.",
      replied:
        "La réponse revient dans ce groupe. Enregistrez maintenant ce travail comme tâche avec /issue : la première ligne est le titre, le reste la description. Cliquez sur Envoyer.",
      creating: "/issue est une commande : Multica crée la tâche directement à partir du message et ne l'ajoute pas à la discussion.",
      created: "La tâche est créée et assignée à l'agent lié à ce bot. Cliquez sur le lien pour l'ouvrir.",
      issue: "La tâche est assignée à Assistant ops et créée en À faire. Une exécution démarre, et l'agent passe la tâche à En cours en commençant.",
      done: "Assistant ops travaille sur la tâche, et son avancement reste consigné sur la tâche. Cliquez sur Rejouer pour recommencer.",
    },
  },
};

type Phase = "idle" | "ask" | "thinking" | "replied" | "creating" | "created" | "issue" | "done";

const ORDER: Phase[] = ["ask", "thinking", "replied", "creating", "created", "issue", "done"];

// Index of the step at which the agent moves the issue to In Progress.
const IN_PROGRESS_STEP = 2;

function ChatMessage({
  author,
  bot,
  botTag,
  own,
  children,
}: {
  author: string;
  bot?: boolean;
  botTag?: string;
  own?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex min-w-0 items-start gap-2 motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1",
        own && "flex-row-reverse",
      )}
    >
      <DemoAvatar name={author} agent={bot} />
      <div className={cn("flex min-w-0 max-w-[85%] flex-col gap-0.5", own && "items-end")}>
        <span className="flex items-center gap-1 text-micro text-muted-foreground">
          {author}
          {bot && botTag && <span className="rounded-xs bg-muted px-1 text-micro text-foreground">{botTag}</span>}
        </span>
        <div
          className={cn(
            "rounded-lg px-2.5 py-1.5 text-body break-words whitespace-pre-line",
            own ? "bg-brand/10 dark:bg-brand/20" : "border bg-background",
          )}
        >
          {children}
        </div>
      </div>
    </div>
  );
}

function TypingBubble({ author, botTag }: { author: string; botTag: string }) {
  return (
    <ChatMessage author={author} bot botTag={botTag}>
      <span aria-hidden className="inline-flex gap-1 py-1">
        {[0, 1, 2].map((i) => (
          <span
            key={i}
            className="size-1.5 rounded-full bg-muted-foreground motion-safe:animate-pulse"
            style={{ animationDelay: `${i * 200}ms` }}
          />
        ))}
      </span>
    </ChatMessage>
  );
}

// The chat tool is drawn neutral on purpose: the flow is the same on every
// supported platform, so no platform's look is implied.
export function ChannelChatDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("ask");
  const [status, setStatus] = useState<Status>("todo");
  const [queued, setQueued] = useState(true);
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";
  const reached = (p: Phase) => ORDER.indexOf(phase) >= ORDER.indexOf(p);
  const view = reached("issue") ? "multica" : "chat";
  const canSend = visible && (phase === "ask" || phase === "replied");

  const send = () => {
    if (phase === "ask") {
      setPhase("thinking");
      after(1800, () => setPhase("replied"));
    } else if (phase === "replied") {
      setPhase("creating");
      after(1200, () => setPhase("created"));
    }
  };

  const openIssue = () => {
    setPhase("issue");
    after(1000, () => setQueued(false));
    t.steps.forEach((_, i) =>
      after(1000 + i * 1000, () => {
        setStepsShown(i + 1);
        if (i + 1 === IN_PROGRESS_STEP) setStatus("in_progress");
      }),
    );
    after(1000 + t.steps.length * 1000, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("ask");
    setStatus("todo");
    setQueued(true);
    setStepsShown(0);
  };

  const mention = <span className="font-medium text-brand">{t.mention}</span>;
  const draft = phase === "ask" ? t.askText : phase === "replied" ? t.issueText : null;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div aria-hidden className="flex gap-1 border-b px-3 py-1.5 text-caption">
        {(["chat", "multica"] as const).map((v) => (
          <span
            key={v}
            className={cn(
              "rounded-md px-2 py-0.5 transition-colors",
              view === v ? "bg-muted font-medium text-foreground" : "text-muted-foreground",
            )}
          >
            {v === "chat" ? t.chatApp : t.multica}
          </span>
        ))}
      </div>

      {view === "chat" ? (
        <div className="flex min-h-96 flex-col">
          <div className="flex items-center gap-2 border-b px-4 py-2">
            <span
              aria-hidden
              className="inline-flex size-6 items-center justify-center rounded-md bg-muted text-muted-foreground"
            >
              <Users className="size-3.5" />
            </span>
            <span className="text-label font-medium">{t.chatName}</span>
          </div>

          <div className="flex flex-1 flex-col gap-3 p-4">
            <ChatMessage author={t.lei}>{t.leiMessage}</ChatMessage>
            {reached("thinking") && (
              <ChatMessage author={t.you} own>
                {mention}
                {t.askText}
              </ChatMessage>
            )}
            {phase === "thinking" && <TypingBubble author={t.bot} botTag={t.botTag} />}
            {reached("replied") && (
              <ChatMessage author={t.bot} bot botTag={t.botTag}>
                {t.askReply}
              </ChatMessage>
            )}
            {reached("creating") && (
              <ChatMessage author={t.you} own>
                {mention}
                {t.issueText}
              </ChatMessage>
            )}
            {phase === "creating" && <TypingBubble author={t.bot} botTag={t.botTag} />}
            {reached("created") && (
              <ChatMessage author={t.bot} bot botTag={t.botTag}>
                {t.created}
                {"\n"}
                <CoachMark active={phase === "created"} side="right" ringClassName="rounded-sm">
                  <button
                    type="button"
                    disabled={phase !== "created"}
                    onClick={openIssue}
                    className="text-brand underline decoration-brand/40 underline-offset-2 hover:decoration-brand"
                  >
                    {t.link}
                  </button>
                </CoachMark>
              </ChatMessage>
            )}
          </div>

          <div className="m-3 mt-0 flex items-end gap-2 rounded-lg border bg-background p-2">
            <div className="min-h-10 min-w-0 flex-1 px-1 text-body break-words whitespace-pre-line">
              {visible && draft ? (
                <span key={phase} className="motion-safe:animate-in motion-safe:fade-in">
                  {mention}
                  {draft}
                </span>
              ) : (
                <span className="text-muted-foreground">{t.placeholder}</span>
              )}
            </div>
            <CoachMark active={canSend} side="top" align="end" ringClassName="rounded-full">
              <DemoSendButton label={t.send} disabled={!canSend} onClick={send} />
            </CoachMark>
          </div>
        </div>
      ) : (
        <div className="flex min-h-96 flex-col gap-3 p-4 motion-safe:animate-in motion-safe:fade-in">
          <div className="text-body font-medium">{t.issueTitle}</div>
          <dl className="grid w-fit grid-cols-[auto_auto] items-center gap-x-6 gap-y-2 text-body">
            <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
            <dd className="flex items-center gap-1.5">
              <span
                aria-hidden
                className={cn(
                  "size-3 rounded-full border-2 transition-colors",
                  status === "todo" ? "border-muted-foreground" : "border-warning bg-warning/40",
                )}
              />
              <span key={status} className="motion-safe:animate-in motion-safe:fade-in">
                {t.status[status]}
              </span>
            </dd>
            <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
            <dd className="flex items-center gap-1.5">
              <DemoAvatar name={t.bot} agent />
              {t.bot}
            </dd>
            <dt className="text-caption text-muted-foreground">{t.creatorLabel}</dt>
            <dd className="flex items-center gap-1.5">
              <DemoAvatar name={t.you} />
              {t.you}
            </dd>
          </dl>
          <p className="rounded-lg border bg-background px-3 py-2 text-body">{t.description}</p>
          <DemoRun agent={t.bot} status={queued ? t.queued : t.running} active>
            {t.steps.slice(0, stepsShown).map((step, i) => (
              <DemoStep key={step} state={i === stepsShown - 1 ? "running" : "done"}>
                {step}
              </DemoStep>
            ))}
          </DemoRun>
        </div>
      )}
    </DemoFrame>
  );
}
