"use client";

import { useState } from "react";
import { ChevronDown, Users, X } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
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

type Status = "todo" | "in_progress" | "in_review";
type Candidate = { name: string; kind: "member" | "agent" | "squad" };

const copy = {
  en: {
    title: "OPS-21 · Prepare the end-of-month customer thank-you event",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    unassigned: "Unassigned",
    status: { todo: "Todo", in_progress: "In Progress", in_review: "In Review" } as Record<Status, string>,
    groups: { member: "Members", agent: "Agents", squad: "Squads" },
    squad: "Events squad",
    leader: "Ops helper",
    writer: "Writing helper",
    you: "You",
    leaderTag: "Leader",
    candidates: [
      { name: "You", kind: "member" },
      { name: "Lei Li", kind: "member" },
      { name: "Ops helper", kind: "agent" },
      { name: "Writing helper", kind: "agent" },
      { name: "Events squad", kind: "squad" },
    ] as Candidate[],
    confirmTitle: "Confirm assignment?",
    confirmBody: (name: string) => `Assign this issue to ${name}. They'll start right away if the run conditions are met.`,
    close: "Close",
    dontStart: "Don't start yet",
    confirm: "Confirm assignment",
    running: "Running",
    completed: "Completed",
    leaderSteps: [
      "Read the issue description and discussion",
      "Match the work against the squad roster",
      "Set the status to In Progress",
      "Post the dispatch comment",
      "Record this evaluation",
    ],
    dispatch: { writer: "draft the invitation, first version by Wednesday.", you: "please confirm the venue and the catering quote." },
    dispatchActivity: "evaluated and took action: handed off to Writing helper and You",
    writerSteps: ["Read the issue and the dispatch comment", "Draft the invitation"],
    writerReply: "The invitation draft is ready, with the time, the place and an RSVP link.",
    recheckSteps: ["Read the new comment"],
    noActionActivity: "evaluated: no action needed (waiting on You for the venue)",
    yourDraft: "Venue is the third-floor event hall; catering quote is 4,800 and confirmed.",
    send: "Send",
    finalSteps: ["Read the new comment", "Pull the invitation and venue details together", "Set the status to In Review"],
    summary: "The event is ready: the invitation draft is done, and the venue and catering are confirmed. Please review before we send it out.",
    finalActivity: "evaluated and took action: work is complete, moved to In Review",
    captions: {
      idle: "An issue that needs both writing and on-site coordination.",
      start: "Hand the whole issue to a squad: click Assignee.",
      pick: "Squads are listed with members and agents. Pick Events squad.",
      confirm: "The squad becomes the assignee. Confirm to start.",
      leader: "Only the leader starts. It reads the issue and the roster, then dispatches by @mention instead of doing the work.",
      leaderPosted: "The @mentions are the handoff: Writing helper gets a run, and you get a notification. The leader has stopped.",
      writer: "Writing helper works in its own run on the same issue.",
      writerPosted: "A squad member posted an update without @mentioning anyone, so the leader wakes up.",
      recheck: "The leader re-evaluates.",
      yourTurn: "Your part isn't in yet, so the leader records no action and posts nothing. Your reply is written: click send.",
      final: "Your reply wakes the leader again. Everything it asked for is in.",
      done: "The leader sums up and moves the issue to In Review; Done is left for a person to decide. Use Replay to watch again.",
    },
  },
  zh: {
    title: "OPS-21 · 筹备月底的客户答谢会",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    unassigned: "未分配",
    status: { todo: "待办", in_progress: "进行中", in_review: "审核中" } as Record<Status, string>,
    groups: { member: "成员", agent: "智能体", squad: "小队" },
    squad: "活动小队",
    leader: "运营助手",
    writer: "文案助手",
    you: "你",
    leaderTag: "队长",
    candidates: [
      { name: "你", kind: "member" },
      { name: "李雷", kind: "member" },
      { name: "运营助手", kind: "agent" },
      { name: "文案助手", kind: "agent" },
      { name: "活动小队", kind: "squad" },
    ] as Candidate[],
    confirmTitle: "确认指派？",
    confirmBody: (name: string) => `将这个任务指派给 ${name}；符合运行条件时会立即开始处理。`,
    close: "关闭",
    dontStart: "暂不开始",
    confirm: "确认指派",
    running: "运行中",
    completed: "已完成",
    leaderSteps: ["读取任务描述和讨论", "对照小队花名册挑选成员", "把状态改为 进行中", "发出派活评论", "记录这次评估"],
    dispatch: { writer: "写答谢会邀请函，周三前给初稿。", you: "确认一下场地和餐饮报价。" },
    dispatchActivity: "已评估并采取了操作：派给文案助手和你",
    writerSteps: ["读取任务和派活评论", "起草邀请函"],
    writerReply: "邀请函初稿写好了，时间、地点和回执链接都在里面。",
    recheckSteps: ["读取新评论"],
    noActionActivity: "已评估：无需操作（等你确认场地）",
    yourDraft: "场地定在三楼多功能厅，餐饮报价 4,800 元，已确认。",
    send: "发送",
    finalSteps: ["读取新评论", "汇总邀请函和场地信息", "把状态改为 审核中"],
    summary: "答谢会准备好了：邀请函初稿已完成，场地和餐饮已确认。请检查后再发出邀请。",
    finalActivity: "已评估并采取了操作：工作已完成，改为审核中",
    captions: {
      idle: "一条既要写文案、又要跑现场的任务。",
      start: "把整条任务交给小队：点击负责人。",
      pick: "小队和成员、智能体列在一起。选择\"活动小队\"。",
      confirm: "小队会成为负责人。确认后开始。",
      leader: "只有队长开始运行。它读任务和花名册，用 @ 派活，自己不动手。",
      leaderPosted: "@ 就是交接：文案助手入队了一次运行，你收到一条通知。队长到这里就停下了。",
      writer: "文案助手在同一条任务上开始自己的运行。",
      writerPosted: "小队成员发了进展，没有 @ 任何人，所以队长被唤醒。",
      recheck: "队长重新评估下一步。",
      yourTurn: "你那部分还没交，队长记一条\"无需操作\"，不发评论。你的回复已经写好：点击发送。",
      final: "你的回复再次唤醒队长，它要的东西都齐了。",
      done: "队长汇总结果，把任务改为审核中；改成已完成留给人来决定。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-21 · 月末の顧客感謝イベントを準備する",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    unassigned: "未割り当て",
    status: { todo: "未着手", in_progress: "進行中", in_review: "レビュー中" } as Record<Status, string>,
    groups: { member: "メンバー", agent: "エージェント", squad: "スクワッド" },
    squad: "イベントスクワッド",
    leader: "運用アシスタント",
    writer: "ライティングアシスタント",
    you: "あなた",
    leaderTag: "リーダー",
    candidates: [
      { name: "あなた", kind: "member" },
      { name: "李雷", kind: "member" },
      { name: "運用アシスタント", kind: "agent" },
      { name: "ライティングアシスタント", kind: "agent" },
      { name: "イベントスクワッド", kind: "squad" },
    ] as Candidate[],
    confirmTitle: "割り当てを確認しますか？",
    confirmBody: (name: string) => `このタスクを ${name} に割り当てます。実行条件を満たす場合はすぐに作業を開始します。`,
    close: "閉じる",
    dontStart: "まだ開始しない",
    confirm: "割り当てを確認",
    running: "実行中",
    completed: "完了",
    leaderSteps: [
      "タスクの説明と議論を読む",
      "スクワッドの名簿と照らし合わせてメンバーを選ぶ",
      "ステータスを 進行中 に変更",
      "割り振りのコメントを投稿",
      "今回の評価を記録",
    ],
    dispatch: { writer: "招待状の文面をお願いします。初稿は水曜までに。", you: "会場とケータリングの見積もりを確認してください。" },
    dispatchActivity: "評価してアクションを実行しました: ライティングアシスタントとあなたに割り振り",
    writerSteps: ["タスクと割り振りのコメントを読む", "招待状の下書きを書く"],
    writerReply: "招待状の初稿ができました。日時、場所、出欠フォームへのリンクを入れています。",
    recheckSteps: ["新しいコメントを読む"],
    noActionActivity: "評価完了: 必要なアクションはありません（会場の確認をあなたに依頼中）",
    yourDraft: "会場は 3 階のイベントホールに決まりました。ケータリングは 4,800 の見積もりで確定です。",
    send: "送信",
    finalSteps: ["新しいコメントを読む", "招待状と会場の情報をまとめる", "ステータスを レビュー中 に変更"],
    summary: "イベントの準備が整いました。招待状の初稿ができ、会場とケータリングも確定しています。送付前に確認をお願いします。",
    finalActivity: "評価してアクションを実行しました: 作業が完了したのでレビュー中に変更",
    captions: {
      idle: "文面づくりと現地の手配、両方が必要なタスクです。",
      start: "タスク全体をスクワッドに任せます。担当者をクリックします。",
      pick: "スクワッドはメンバーやエージェントと同じ一覧に並びます。イベントスクワッドを選びます。",
      confirm: "スクワッドが担当者になります。確認すると開始します。",
      leader: "動き出すのはリーダーだけです。タスクと名簿を読み、自分では作業せず @メンションで割り振ります。",
      leaderPosted: "@メンションが引き継ぎの合図です。ライティングアシスタントには実行が入り、あなたには通知が届きます。リーダーはここで止まります。",
      writer: "ライティングアシスタントが同じタスクで自分の実行を始めます。",
      writerPosted: "スクワッドのメンバーが誰も @メンションせずに進捗を投稿したので、リーダーが起こされます。",
      recheck: "リーダーが次の一手を評価し直します。",
      yourTurn: "あなたの担当分がまだなので、リーダーは「アクションなし」を記録し、コメントは投稿しません。返信は入力済みです。送信をクリックします。",
      final: "あなたの返信でリーダーがもう一度起こされます。頼んだものはすべて揃いました。",
      done: "リーダーが結果をまとめ、タスクをレビュー中にします。完了にするかどうかは人が決めます。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "OPS-21 · 월말 고객 감사 행사 준비",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    unassigned: "담당자 없음",
    status: { todo: "할 일", in_progress: "진행 중", in_review: "리뷰 중" } as Record<Status, string>,
    groups: { member: "멤버", agent: "에이전트", squad: "스쿼드" },
    squad: "행사 스쿼드",
    leader: "운영 도우미",
    writer: "글쓰기 도우미",
    you: "나",
    leaderTag: "리더",
    candidates: [
      { name: "나", kind: "member" },
      { name: "리레이", kind: "member" },
      { name: "운영 도우미", kind: "agent" },
      { name: "글쓰기 도우미", kind: "agent" },
      { name: "행사 스쿼드", kind: "squad" },
    ] as Candidate[],
    confirmTitle: "할당을 확인할까요?",
    confirmBody: (name: string) => `이 태스크를 ${name}에게 할당합니다. 실행 조건을 충족하면 바로 시작합니다.`,
    close: "닫기",
    dontStart: "아직 시작 안 함",
    confirm: "할당 확인",
    running: "실행 중",
    completed: "완료됨",
    leaderSteps: [
      "태스크 설명과 논의 읽기",
      "스쿼드 명단과 비교해 멤버 고르기",
      "상태를 진행 중으로 변경",
      "작업 배정 댓글 남기기",
      "이번 평가 기록하기",
    ],
    dispatch: { writer: "초대장 문안을 써 주세요. 초안은 수요일까지 부탁합니다.", you: "행사장과 케이터링 견적을 확인해 주세요." },
    dispatchActivity: "평가 후 작업을 수행했습니다: 글쓰기 도우미와 나에게 배정",
    writerSteps: ["태스크와 배정 댓글 읽기", "초대장 초안 작성"],
    writerReply: "초대장 초안을 다 썼습니다. 일시, 장소, 참석 회신 링크를 넣었습니다.",
    recheckSteps: ["새 댓글 읽기"],
    noActionActivity: "평가 완료: 필요한 작업 없음(행사장 확인은 나에게서 대기 중)",
    yourDraft: "행사장은 3층 다목적홀로 정했고, 케이터링 견적 4,800으로 확정했습니다.",
    send: "보내기",
    finalSteps: ["새 댓글 읽기", "초대장과 행사장 정보 정리", "상태를 리뷰 중으로 변경"],
    summary: "행사 준비가 끝났습니다. 초대장 초안이 나왔고 행사장과 케이터링도 확정됐습니다. 발송 전에 검토해 주세요.",
    finalActivity: "평가 후 작업을 수행했습니다: 작업이 끝나 리뷰 중으로 변경",
    captions: {
      idle: "문안 작성과 현장 준비가 모두 필요한 태스크입니다.",
      start: "태스크 전체를 스쿼드에 맡깁니다. 담당자를 클릭하세요.",
      pick: "스쿼드는 멤버, 에이전트와 같은 목록에 있습니다. 행사 스쿼드를 고르세요.",
      confirm: "스쿼드가 담당자가 됩니다. 확인하면 시작합니다.",
      leader: "리더만 실행을 시작합니다. 태스크와 명단을 읽고, 직접 일하지 않고 @멘션으로 배정합니다.",
      leaderPosted: "@멘션이 곧 인계입니다. 글쓰기 도우미에게는 실행이 들어가고 나에게는 알림이 옵니다. 리더는 여기서 멈춥니다.",
      writer: "글쓰기 도우미가 같은 태스크에서 자기 실행을 시작합니다.",
      writerPosted: "스쿼드 멤버가 아무도 @멘션하지 않고 진행 상황을 올렸으므로 리더가 깨어납니다.",
      recheck: "리더가 다음 단계를 다시 평가합니다.",
      yourTurn: "내 몫이 아직 들어오지 않아 리더는 '작업 없음'을 기록하고 댓글은 남기지 않습니다. 답장은 작성되어 있습니다. 보내기를 클릭하세요.",
      final: "내 답장이 리더를 다시 깨웁니다. 요청한 내용이 모두 모였습니다.",
      done: "리더가 결과를 정리하고 태스크를 리뷰 중으로 옮깁니다. 완료 처리는 사람이 결정합니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "OPS-21 · Préparer la soirée de remerciement clients de fin de mois",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    unassigned: "Non assigné",
    status: { todo: "À faire", in_progress: "En cours", in_review: "En relecture" } as Record<Status, string>,
    groups: { member: "Membres", agent: "Agents", squad: "Squads" },
    squad: "Squad événements",
    leader: "Assistant ops",
    writer: "Assistant rédaction",
    you: "Vous",
    leaderTag: "Chef",
    candidates: [
      { name: "Vous", kind: "member" },
      { name: "Lei Li", kind: "member" },
      { name: "Assistant ops", kind: "agent" },
      { name: "Assistant rédaction", kind: "agent" },
      { name: "Squad événements", kind: "squad" },
    ] as Candidate[],
    confirmTitle: "Confirmer l'assignation ?",
    confirmBody: (name: string) => `Assigner cette tâche à ${name}. Le travail démarrera immédiatement si les conditions d'exécution sont réunies.`,
    close: "Fermer",
    dontStart: "Ne pas démarrer maintenant",
    confirm: "Confirmer l'assignation",
    running: "En cours",
    completed: "Terminée",
    leaderSteps: [
      "Lire la description et la discussion de la tâche",
      "Comparer le travail au roster du squad",
      "Passer le statut à En cours",
      "Publier le commentaire de délégation",
      "Consigner cette évaluation",
    ],
    dispatch: { writer: "rédige l'invitation, première version pour mercredi.", you: "merci de confirmer la salle et le devis du traiteur." },
    dispatchActivity: "a évalué et agi : délégué à Assistant rédaction et à Vous",
    writerSteps: ["Lire la tâche et le commentaire de délégation", "Rédiger l'invitation"],
    writerReply: "Le brouillon de l'invitation est prêt, avec la date, le lieu et un lien de réponse.",
    recheckSteps: ["Lire le nouveau commentaire"],
    noActionActivity: "a évalué : aucune action nécessaire (en attente de Vous pour la salle)",
    yourDraft: "La salle sera celle du troisième étage ; le devis du traiteur est de 4 800 et il est validé.",
    send: "Envoyer",
    finalSteps: ["Lire le nouveau commentaire", "Rassembler l'invitation et les infos sur la salle", "Passer le statut à En relecture"],
    summary: "La soirée est prête : le brouillon de l'invitation est fait, la salle et le traiteur sont confirmés. Merci de relire avant l'envoi.",
    finalActivity: "a évalué et agi : travail terminé, passé En relecture",
    captions: {
      idle: "Une tâche qui demande à la fois de la rédaction et de l'organisation sur place.",
      start: "Confiez toute la tâche à un squad : cliquez sur Assigné à.",
      pick: "Les squads sont listés avec les membres et les agents. Choisissez Squad événements.",
      confirm: "Le squad devient l'assigné. Confirmez pour démarrer.",
      leader: "Seul le chef démarre. Il lit la tâche et le roster, puis délègue par @mention au lieu de faire le travail.",
      leaderPosted: "Les @mentions servent de passation : Assistant rédaction reçoit une exécution, et vous une notification. Le chef s'arrête là.",
      writer: "Assistant rédaction travaille dans sa propre exécution, sur la même tâche.",
      writerPosted: "Un membre du squad a publié un point d'avancement sans @mentionner personne : le chef se réveille.",
      recheck: "Le chef réévalue la suite.",
      yourTurn: "Votre partie n'est pas encore arrivée : le chef consigne « aucune action » et ne publie rien. Votre réponse est rédigée : cliquez sur envoyer.",
      final: "Votre réponse réveille de nouveau le chef. Tout ce qu'il avait demandé est là.",
      done: "Le chef fait la synthèse et passe la tâche En relecture ; c'est à une personne de la passer à Terminé. Cliquez sur Rejouer pour revoir la démo.",
    },
  },
};

const ORDER = [
  "start",
  "pick",
  "confirm",
  "leader",
  "leaderPosted",
  "writer",
  "writerPosted",
  "recheck",
  "yourTurn",
  "final",
  "done",
] as const;
type Phase = (typeof ORDER)[number];

const STEP_MS = 800;
const PAUSE_MS = 1200;

const statusDot: Record<Status, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
};

// The squad flow as the server runs it: assigning enqueues only the leader;
// the leader dispatches with @mentions and stops; a member's plain update wakes
// it again; it moves the parent to in_review once the goal is met.
export function SquadDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("start");
  const [status, setStatus] = useState<Status>("todo");
  const [stepsShown, setStepsShown] = useState(0);

  const at = (p: Phase) => ORDER.indexOf(phase) >= ORDER.indexOf(p);
  const assigned = at("leader");

  // Plays one run's steps, then hands over to `next` after a pause.
  const playRun = (start: number, steps: string[], next: Phase, onStep?: (i: number) => void) => {
    after(start, () => setStepsShown(0));
    steps.forEach((_, i) =>
      after(start + 300 + i * STEP_MS, () => {
        setStepsShown(i + 1);
        onStep?.(i + 1);
      }),
    );
    const end = start + 300 + steps.length * STEP_MS + 300;
    after(end, () => setPhase(next));
    return end;
  };

  const confirm = () => {
    setPhase("leader");
    let time = playRun(0, t.leaderSteps, "leaderPosted", (n) => n === 3 && setStatus("in_progress"));
    time += PAUSE_MS + 800;
    after(time, () => setPhase("writer"));
    time = playRun(time, t.writerSteps, "writerPosted");
    time += PAUSE_MS + 800;
    after(time, () => setPhase("recheck"));
    playRun(time, t.recheckSteps, "yourTurn");
  };

  const send = () => {
    setPhase("final");
    playRun(0, t.finalSteps, "done", (n) => n === 3 && setStatus("in_review"));
  };

  const replay = () => {
    clear();
    setPhase("start");
    setStatus("todo");
    setStepsShown(0);
  };

  const caption = visible ? t.captions[phase] : t.captions.idle;

  const run = (agent: string, steps: string[], active: boolean, leader = false) => (
    <DemoRun agent={leader ? `${agent} · ${t.leaderTag}` : agent} status={active ? t.running : t.completed} active={active}>
      {active &&
        steps.slice(0, stepsShown).map((step, i) => (
          <DemoStep key={step} state={i === stepsShown - 1 ? "running" : "done"}>
            {step}
          </DemoStep>
        ))}
    </DemoRun>
  );

  const mention = (name: string) => <span className="font-medium text-brand">@{name}</span>;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="relative flex min-h-96 flex-col gap-3 p-4">
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
            <CoachMark active={visible && phase === "start"} side="right" ringClassName="rounded-md">
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={phase === "pick"}
                disabled={!visible || (phase !== "start" && phase !== "pick")}
                onClick={() => setPhase(phase === "pick" ? "start" : "pick")}
                className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-muted disabled:hover:bg-transparent"
              >
                {assigned ? (
                  <>
                    <SquadAvatar />
                    {t.squad}
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
                {(["member", "agent", "squad"] as const).map((kind) => (
                  <div key={kind}>
                    <div className="px-2 pt-1 pb-0.5 text-micro text-muted-foreground">{t.groups[kind]}</div>
                    {t.candidates
                      .filter((c) => c.kind === kind)
                      .map((candidate) => {
                        const target = kind === "squad";
                        return (
                          <CoachMark
                            key={candidate.name}
                            active={target}
                            side="right"
                            ringClassName="rounded-md"
                            className="flex w-full"
                          >
                            <button
                              type="button"
                              role="option"
                              aria-selected={false}
                              disabled={!target}
                              onClick={() => setPhase("confirm")}
                              className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:hover:bg-transparent"
                            >
                              {kind === "squad" ? <SquadAvatar /> : <DemoAvatar name={candidate.name} agent={kind === "agent"} />}
                              {candidate.name}
                            </button>
                          </CoachMark>
                        );
                      })}
                  </div>
                ))}
              </div>
            )}
          </dd>
        </dl>

        {at("leader") && run(t.leader, t.leaderSteps, phase === "leader", true)}
        {at("leaderPosted") && (
          <>
            <DemoComment author={t.leader} agent>
              <span className="block">
                {mention(t.writer)} {t.dispatch.writer}
              </span>
              <span className="block">
                {mention(t.you)} {t.dispatch.you}
              </span>
            </DemoComment>
            <Activity actor={t.leader}>{t.dispatchActivity}</Activity>
          </>
        )}
        {at("writer") && run(t.writer, t.writerSteps, phase === "writer")}
        {at("writerPosted") && (
          <DemoComment author={t.writer} agent>
            {t.writerReply}
          </DemoComment>
        )}
        {at("recheck") && run(t.leader, t.recheckSteps, phase === "recheck", true)}
        {at("yourTurn") && <Activity actor={t.leader}>{t.noActionActivity}</Activity>}

        {phase === "yourTurn" && (
          <div className="rounded-lg border bg-background p-2 motion-safe:animate-in motion-safe:fade-in">
            <div className="min-h-10 px-1 text-body">{t.yourDraft}</div>
            <div className="mt-1 flex justify-end">
              <CoachMark active side="left" ringClassName="rounded-full">
                <DemoSendButton label={t.send} onClick={send} />
              </CoachMark>
            </div>
          </div>
        )}

        {at("final") && (
          <>
            <DemoComment author={t.you}>{t.yourDraft}</DemoComment>
            {run(t.leader, t.finalSteps, phase === "final", true)}
          </>
        )}
        {phase === "done" && (
          <>
            <DemoComment author={t.leader} agent>
              {t.summary}
            </DemoComment>
            <Activity actor={t.leader}>{t.finalActivity}</Activity>
          </>
        )}

        {phase === "confirm" && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-background/60 p-4 backdrop-blur-[2px] motion-safe:animate-in motion-safe:fade-in">
            <div
              role="dialog"
              aria-modal="false"
              aria-labelledby="squad-demo-title"
              className="w-full max-w-sm rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:zoom-in-95"
            >
              <div className="flex items-start gap-2 p-4 pb-3">
                <div className="min-w-0 flex-1">
                  <div id="squad-demo-title" className="text-title-sm font-medium">
                    {t.confirmTitle}
                  </div>
                  <div className="mt-1 text-body text-muted-foreground">{t.confirmBody(t.squad)}</div>
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
                <Button type="button" variant="outline" size="sm" disabled>
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

function SquadAvatar() {
  return (
    <span
      aria-hidden
      className="inline-flex size-5 shrink-0 items-center justify-center rounded-md bg-muted text-foreground"
    >
      <Users className="size-3" />
    </span>
  );
}

/** A line in the issue's activity timeline, e.g. the leader's recorded evaluation. */
function Activity({ actor, children }: { actor: string; children: string }) {
  return (
    <div className="flex min-w-0 items-start gap-2 px-1 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
      <DemoAvatar name={actor} agent />
      <span className="min-w-0 pt-0.5">
        <span className="font-medium text-foreground">{actor}</span> {children}
      </span>
    </div>
  );
}
