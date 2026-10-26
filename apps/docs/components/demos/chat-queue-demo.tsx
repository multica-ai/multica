"use client";

import { type ReactNode, useEffect, useState } from "react";
import { CornerDownRight, Ellipsis, ListEnd, Loader2, Pencil, Square, Trash2 } from "lucide-react";
import {
  CoachMark,
  DemoAvatar,
  DemoFrame,
  DemoSendButton,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Captions = {
  idle: string;
  opening: string;
  ready: string;
  queued: string;
  menu: string;
  steering: string;
  steered: string;
  clearing: string;
  cleared: string;
};

const copy = {
  en: {
    title: "Chat · Ops helper",
    agent: "Ops helper",
    placeholder: "Message Ops helper…",
    queueSend: "Queue message",
    stop: "Stop",
    typing: "Typing",
    thinking: "Thinking",
    repliedIn: (elapsed: string) => `Replied in ${elapsed}`,
    replyTimes: ["14s", "11s"],
    cancelled: "This reply was cancelled.",
    queueTitle: "1 queued message",
    steer: "Steer",
    remove: "Remove queued message",
    more: "More queue actions",
    edit: "Edit queued message",
    clear: "Clear all",
    firstPrompt: "Draft an all-staff notice for next Wednesday's fire drill. Assembly point: the 1st-floor lobby.",
    partial: [
      "Hi all, we're holding a fire drill next Wednesday at 3 pm. ",
      "When the alarm sounds, take the nearest emergency exit ",
    ],
    tail: "and gather in the 1st-floor lobby. Team leads, please count heads and report to the admin office.",
    draft: "Change the assembly point to the B1 parking garage. The lobby is being renovated that day.",
    steeredReply:
      "Hi all, we're holding a fire drill next Wednesday at 3 pm. When the alarm sounds, take the nearest emergency exit and gather in the B1 parking garage (the lobby is closed for renovation that day). Team leads, please count heads and report to the admin office.",
    captions: {
      idle: "You've asked Ops helper to draft a notice.",
      opening: "Ops helper is writing its reply. Then you remember the assembly point has to change.",
      ready: "The agent is still replying, so the send button now queues the message. Click send.",
      queued:
        "The message waits in the queue until the current reply ends. Click Steer to have it handled now, or open ⋯ to clear the queue.",
      menu: "Clear all withdraws every queued message; the current reply keeps going. Click Clear all.",
      steering:
        "Steer stopped the current reply and kept what it had written. The agent started on your new message right away.",
      steered: "The notice is rewritten with the new location. Use Replay to watch again.",
      clearing: "The queue is empty and that message won't reach the agent. The current reply continues.",
      cleared: "The reply finished as first asked, still at the 1st-floor lobby. Use Replay and try Steer.",
    } satisfies Captions,
  },
  zh: {
    title: "聊天 · 运营助手",
    agent: "运营助手",
    placeholder: "给 运营助手 发消息...",
    queueSend: "排队发送",
    stop: "停止",
    typing: "输入中",
    thinking: "思考中",
    repliedIn: (elapsed: string) => `${elapsed} 内回复`,
    replyTimes: ["14s", "11s"],
    cancelled: "这次回复已取消。",
    queueTitle: "1 条排队消息",
    steer: "引导",
    remove: "移除排队消息",
    more: "更多队列操作",
    edit: "编辑排队消息",
    clear: "全部清空",
    firstPrompt: "起草一份下周三消防演练的全员通知，集合地点写 1 楼大厅。",
    partial: ["各位同事：下周三下午 3 点进行消防演练，", "警报响起后请走最近的安全通道，"],
    tail: "到 1 楼大厅集合，各部门负责人清点人数后报给行政部。",
    draft: "集合地点改成 B1 停车场，1 楼大厅那天在装修。",
    steeredReply:
      "各位同事：下周三下午 3 点进行消防演练，警报响起后请走最近的安全通道，到 B1 停车场集合（1 楼大厅当天装修），各部门负责人清点人数后报给行政部。",
    captions: {
      idle: "你请 运营助手 起草一份通知。",
      opening: "运营助手 正在写回复。这时你想起集合地点得改。",
      ready: "智能体还在回复，发送按钮变成了\"排队发送\"。点击发送。",
      queued: "消息进了队列，要等当前回复写完才处理。点击\"引导\"让它马上处理，或者点 ⋯ 清空队列。",
      menu: "全部清空会撤回队列里的消息，当前回复不受影响。点击\"全部清空\"。",
      steering: "引导停掉了当前回复，已写出的部分留在对话里。智能体马上开始处理你的新消息。",
      steered: "通知按新地点重写好了。点右上角可以重新演示。",
      clearing: "队列已清空，那条消息不会发给智能体。当前回复继续写。",
      cleared: "回复按原来的要求写完，地点仍是 1 楼大厅。点右上角重新演示，可以试试\"引导\"。",
    } satisfies Captions,
  },
  ja: {
    title: "チャット · 運用アシスタント",
    agent: "運用アシスタント",
    placeholder: "運用アシスタント にメッセージ…",
    queueSend: "キューに追加",
    stop: "停止",
    typing: "入力中",
    thinking: "考え中",
    repliedIn: (elapsed: string) => `${elapsed}で返信`,
    replyTimes: ["14s", "11s"],
    cancelled: "この返信はキャンセルされました。",
    queueTitle: "キュー内のメッセージ 1 件",
    steer: "指示",
    remove: "キューから削除",
    more: "その他のキュー操作",
    edit: "キュー内のメッセージを編集",
    clear: "すべて削除",
    firstPrompt: "来週水曜の避難訓練について、全社員向けのお知らせを書いてください。集合場所は 1 階ロビーです。",
    partial: ["皆さま、来週水曜の午後 3 時に避難訓練を行います。", "警報が鳴ったら最寄りの非常口から避難し、"],
    tail: "1 階ロビーに集合してください。各部署の責任者は人数を確認し、総務部に報告してください。",
    draft: "集合場所を地下 1 階の駐車場に変更してください。当日は 1 階ロビーが改装中です。",
    steeredReply:
      "皆さま、来週水曜の午後 3 時に避難訓練を行います。警報が鳴ったら最寄りの非常口から避難し、地下 1 階の駐車場に集合してください（当日は 1 階ロビーが改装中です）。各部署の責任者は人数を確認し、総務部に報告してください。",
    captions: {
      idle: "運用アシスタント にお知らせの下書きを頼みました。",
      opening: "運用アシスタント が返信を書いています。そこで集合場所を変える必要を思い出しました。",
      ready: "エージェントはまだ返信中なので、送信ボタンは「キューに追加」になっています。送信をクリックします。",
      queued:
        "メッセージはキューに入り、今の返信が終わるまで処理されません。「指示」をクリックするとすぐ処理されます。⋯ からキューを空にすることもできます。",
      menu: "「すべて削除」はキュー内のメッセージを取り消します。今の返信はそのまま続きます。「すべて削除」をクリックします。",
      steering: "「指示」で今の返信が止まり、書きかけの部分はチャットに残りました。エージェントはすぐ新しいメッセージに取りかかります。",
      steered: "新しい集合場所でお知らせが書き直されました。「もう一度」で最初から見られます。",
      clearing: "キューが空になり、あのメッセージはエージェントに届きません。今の返信は続きます。",
      cleared: "返信は最初の依頼どおり 1 階ロビーのまま完成しました。「もう一度」で「指示」も試せます。",
    } satisfies Captions,
  },
  ko: {
    title: "채팅 · 운영 도우미",
    agent: "운영 도우미",
    placeholder: "운영 도우미에게 메시지 보내기…",
    queueSend: "대기열에 추가",
    stop: "중지",
    typing: "입력 중",
    thinking: "생각 중",
    repliedIn: (elapsed: string) => `${elapsed} 만에 답변`,
    replyTimes: ["14s", "11s"],
    cancelled: "이 답변이 취소되었습니다.",
    queueTitle: "대기 중인 메시지 1개",
    steer: "지시",
    remove: "대기열에서 삭제",
    more: "추가 대기열 작업",
    edit: "대기 중인 메시지 편집",
    clear: "모두 지우기",
    firstPrompt: "다음 주 수요일 소방 훈련 전 직원 공지를 작성해 주세요. 집결 장소는 1층 로비입니다.",
    partial: ["여러분, 다음 주 수요일 오후 3시에 소방 훈련이 있습니다. ", "경보가 울리면 가장 가까운 비상구로 나와 "],
    tail: "1층 로비에 모여 주세요. 각 팀장은 인원을 확인해 총무팀에 알려 주세요.",
    draft: "집결 장소를 지하 1층 주차장으로 바꿔 주세요. 그날 1층 로비는 공사 중이에요.",
    steeredReply:
      "여러분, 다음 주 수요일 오후 3시에 소방 훈련이 있습니다. 경보가 울리면 가장 가까운 비상구로 나와 지하 1층 주차장에 모여 주세요(그날 1층 로비는 공사 중입니다). 각 팀장은 인원을 확인해 총무팀에 알려 주세요.",
    captions: {
      idle: "운영 도우미에게 공지 초안을 부탁했습니다.",
      opening: "운영 도우미가 답변을 쓰는 중입니다. 그때 집결 장소를 바꿔야 한다는 게 떠오릅니다.",
      ready: "에이전트가 아직 답변 중이라 보내기 버튼이 '대기열에 추가'로 바뀌었습니다. 보내기를 클릭하세요.",
      queued:
        "메시지는 대기열에 들어가 현재 답변이 끝난 뒤에 처리됩니다. '지시'를 클릭하면 바로 처리되고, ⋯에서 대기열을 비울 수도 있습니다.",
      menu: "'모두 지우기'는 대기 중인 메시지를 취소하고, 현재 답변은 그대로 이어집니다. '모두 지우기'를 클릭하세요.",
      steering: "'지시'로 현재 답변이 멈췄고, 이미 쓴 부분은 채팅에 남았습니다. 에이전트가 바로 새 메시지를 처리합니다.",
      steered: "새 집결 장소로 공지가 다시 작성되었습니다. '다시 보기'로 다시 볼 수 있습니다.",
      clearing: "대기열이 비었고, 그 메시지는 에이전트에게 전달되지 않습니다. 현재 답변은 계속됩니다.",
      cleared: "답변은 처음 요청대로 1층 로비로 완성되었습니다. '다시 보기'로 '지시'도 해 보세요.",
    } satisfies Captions,
  },
  fr: {
    title: "Discussion · Assistant ops",
    agent: "Assistant ops",
    placeholder: "Écrire à Assistant ops…",
    queueSend: "Mettre le message en file d'attente",
    stop: "Arrêter",
    typing: "Rédaction",
    thinking: "Réflexion",
    repliedIn: (elapsed: string) => `Réponse en ${elapsed}`,
    replyTimes: ["14s", "11s"],
    cancelled: "Cette réponse a été annulée.",
    queueTitle: "1 message en file d'attente",
    steer: "Réorienter",
    remove: "Retirer le message de la file d'attente",
    more: "Plus d'actions sur la file d'attente",
    edit: "Modifier le message en file d'attente",
    clear: "Tout effacer",
    firstPrompt:
      "Rédigez une note à tout le personnel pour l'exercice d'évacuation de mercredi prochain. Point de rassemblement : le hall du rez-de-chaussée.",
    partial: [
      "Bonjour à tous, un exercice d'évacuation aura lieu mercredi prochain à 15 h. ",
      "Quand l'alarme retentit, sortez par l'issue de secours la plus proche ",
    ],
    tail: "et rejoignez le hall du rez-de-chaussée. Les responsables d'équipe comptent leurs effectifs et préviennent les services généraux.",
    draft: "Changez le point de rassemblement pour le parking du sous-sol : le hall est en travaux ce jour-là.",
    steeredReply:
      "Bonjour à tous, un exercice d'évacuation aura lieu mercredi prochain à 15 h. Quand l'alarme retentit, sortez par l'issue de secours la plus proche et rejoignez le parking du sous-sol (le hall est en travaux ce jour-là). Les responsables d'équipe comptent leurs effectifs et préviennent les services généraux.",
    captions: {
      idle: "Vous avez demandé à Assistant ops de rédiger une note.",
      opening: "Assistant ops rédige sa réponse. Vous vous rappelez alors que le point de rassemblement doit changer.",
      ready:
        "L'agent répond encore : le bouton d'envoi met donc le message en file d'attente. Cliquez sur envoyer.",
      queued:
        "Le message attend dans la file jusqu'à la fin de la réponse en cours. Cliquez sur Réorienter pour qu'il soit traité tout de suite, ou ouvrez ⋯ pour vider la file.",
      menu: "Tout effacer retire tous les messages en attente ; la réponse en cours continue. Cliquez sur Tout effacer.",
      steering:
        "Réorienter a arrêté la réponse en cours, en gardant ce qui était déjà écrit. L'agent traite aussitôt votre nouveau message.",
      steered: "La note est réécrite avec le nouveau lieu. Utilisez Rejouer pour revoir la démo.",
      clearing: "La file est vide et ce message n'arrivera pas à l'agent. La réponse en cours continue.",
      cleared:
        "La réponse s'est terminée comme demandé au départ, toujours dans le hall. Utilisez Rejouer pour essayer Réorienter.",
    } satisfies Captions,
  },
};

type Phase = keyof Captions;

export function ChatQueueDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [run, setRun] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [chunks, setChunks] = useState(0);
  const [secondPill, setSecondPill] = useState<"thinking" | "typing">("thinking");

  // Opening: the first reply streams in, then the composer offers the follow-up.
  useEffect(() => {
    if (!visible) return;
    clear();
    setPhase("opening");
    setChunks(1);
    after(1000, () => setChunks(2));
    after(1800, () => setPhase("ready"));
  }, [visible, run, after, clear]);

  const queue = () => setPhase("queued");

  const steer = () => {
    setPhase("steering");
    setSecondPill("thinking");
    after(1100, () => setSecondPill("typing"));
    after(2600, () => setPhase("steered"));
  };

  const clearQueue = () => {
    setPhase("clearing");
    after(1600, () => setPhase("cleared"));
  };

  const replay = () => {
    clear();
    setChunks(0);
    setSecondPill("thinking");
    setPhase("idle");
    setRun((r) => r + 1);
  };

  const steered = phase === "steering" || phase === "steered";
  const firstDone = phase === "cleared";
  const firstRunning = !steered && !firstDone;
  const queueShown = phase === "queued" || phase === "menu";
  // While the first reply is still being written, an empty composer offers Stop.
  const showStop = phase === "queued" || phase === "menu" || phase === "clearing" || phase === "steering";
  const partial = t.partial.slice(0, chunks).join("");

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[phase]} onReplay={replay}>
      <div className="flex min-h-96 flex-col gap-3 p-4">
        <div className="flex flex-col gap-3">
          <UserMessage>{t.firstPrompt}</UserMessage>
          {phase !== "idle" && (
            <AgentMessage name={t.agent} meta={firstDone ? t.repliedIn(t.replyTimes[0]!) : undefined}>
              <p className={steered ? "text-muted-foreground" : undefined}>
                {firstDone ? `${t.partial.join("")}${t.tail}` : partial}
              </p>
              {steered && <p className="mt-1 text-caption text-muted-foreground">{t.cancelled}</p>}
            </AgentMessage>
          )}
          {phase !== "idle" && firstRunning && <StatusPill label={t.typing} />}

          {steered && <UserMessage>{t.draft}</UserMessage>}
          {phase === "steering" && <StatusPill label={secondPill === "thinking" ? t.thinking : t.typing} />}
          {phase === "steered" && (
            <AgentMessage name={t.agent} meta={t.repliedIn(t.replyTimes[1]!)}>
              <p>{t.steeredReply}</p>
            </AgentMessage>
          )}
        </div>

        <div className="mt-auto">
          {queueShown && (
            <section
              aria-label={t.queueTitle}
              className="relative z-0 mx-3 -mb-3 rounded-lg border bg-muted/40 px-2 pt-1 pb-4 motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-2"
            >
              <div className="flex min-h-7 min-w-0 items-center gap-2 px-1 py-0.5 text-caption">
                <ListEnd aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                <span className="min-w-0 flex-1 truncate text-muted-foreground">{t.draft}</span>
                <div className="flex shrink-0 items-center gap-0.5">
                  <CoachMark quiet active={phase === "queued"} ringClassName="rounded-md">
                    <button
                      type="button"
                      disabled={phase !== "queued"}
                      onClick={steer}
                      className="inline-flex h-6 items-center gap-1 rounded-md px-1.5 text-caption text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-60 disabled:hover:bg-transparent"
                    >
                      <CornerDownRight aria-hidden className="size-3.5" />
                      {t.steer}
                    </button>
                  </CoachMark>
                  <IconButton label={t.remove} disabled>
                    <Trash2 aria-hidden className="size-3.5" />
                  </IconButton>
                  <span className="relative inline-flex">
                    <IconButton
                      label={t.more}
                      disabled={phase !== "queued"}
                      pressed={phase === "menu"}
                      onClick={() => setPhase("menu")}
                    >
                      <Ellipsis aria-hidden className="size-3.5" />
                    </IconButton>
                    {phase === "menu" && (
                      <div
                        role="menu"
                        aria-label={t.more}
                        className="absolute right-0 bottom-full z-20 mb-1.5 flex w-max flex-col rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
                      >
                        <button
                          type="button"
                          role="menuitem"
                          disabled
                          className="flex items-center gap-2 rounded-md px-2 py-1.5 text-caption opacity-50"
                        >
                          <Pencil aria-hidden className="size-3.5" />
                          {t.edit}
                        </button>
                        <div aria-hidden className="-mx-1 my-1 h-px bg-border" />
                        <CoachMark quiet active ringClassName="rounded-md" className="flex">
                          <button
                            type="button"
                            role="menuitem"
                            onClick={clearQueue}
                            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-caption text-destructive hover:bg-destructive/10"
                          >
                            <Trash2 aria-hidden className="size-3.5" />
                            {t.clear}
                          </button>
                        </CoachMark>
                      </div>
                    )}
                  </span>
                </div>
              </div>
            </section>
          )}

          <div className="relative z-10 rounded-lg border bg-background p-2">
            <div className="min-h-10 px-1 text-body">
              {phase === "ready" || phase === "opening" ? (
                <span className="motion-safe:animate-in motion-safe:fade-in">{t.draft}</span>
              ) : (
                <span className="text-muted-foreground">{t.placeholder}</span>
              )}
            </div>
            <div className="mt-1.5 flex items-center">
              <span className="ml-auto" />
              {showStop ? (
                <button
                  type="button"
                  aria-label={t.stop}
                  disabled
                  className="inline-flex size-7 items-center justify-center rounded-full bg-foreground text-background opacity-40"
                >
                  <Square aria-hidden className="size-3 fill-current" />
                </button>
              ) : (
                <CoachMark active={phase === "ready"} side="left" ringClassName="rounded-full">
                  <DemoSendButton label={t.queueSend} disabled={phase !== "ready"} onClick={queue} />
                </CoachMark>
              )}
            </div>
          </div>
        </div>
      </div>
    </DemoFrame>
  );
}

function IconButton({
  label,
  disabled,
  pressed,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  pressed?: boolean;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      aria-expanded={pressed}
      disabled={disabled}
      onClick={onClick}
      className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-accent hover:text-foreground disabled:opacity-60 disabled:hover:bg-transparent aria-expanded:bg-accent"
    >
      {children}
    </button>
  );
}

function UserMessage({ children }: { children: ReactNode }) {
  return (
    <div className="flex justify-end motion-safe:animate-in motion-safe:fade-in motion-safe:slide-in-from-bottom-1">
      <div className="max-w-[85%] rounded-2xl bg-muted px-3 py-2 text-body break-words">{children}</div>
    </div>
  );
}

function AgentMessage({ name, meta, children }: { name: string; meta?: string; children: ReactNode }) {
  return (
    <div className="min-w-0 motion-safe:animate-in motion-safe:fade-in">
      <div className="flex items-center gap-2">
        <DemoAvatar name={name} agent />
        <span className="text-label font-medium">{name}</span>
        {meta && <span className="text-caption text-muted-foreground">{meta}</span>}
      </div>
      <div className="mt-1 pl-7 text-body break-words">{children}</div>
    </div>
  );
}

function StatusPill({ label }: { label: string }) {
  return (
    <div className="pl-7">
      <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
        <Loader2 aria-hidden className="size-3 text-brand motion-safe:animate-spin" />
        {label}
      </span>
    </div>
  );
}
