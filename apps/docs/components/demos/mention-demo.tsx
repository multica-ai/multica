"use client";

import { useState } from "react";
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

type Candidate = { name: string; agent: boolean };

const copy = {
  en: {
    title: "OPS-15 · Prepare next week's customer training",
    assigneeLabel: "Assignee",
    assignee: "Lei Li",
    you: "You",
    draft: "The invitation email for the training isn't written yet. ",
    agentsGroup: "Agents",
    membersGroup: "Members",
    candidates: [
      { name: "Writing helper", agent: true },
      { name: "Ops helper", agent: true },
      { name: "Lei Li", agent: false },
    ] as Candidate[],
    memberNote: "Notify only",
    mention: "@ Mention",
    willStart: "Starts when sent",
    send: "Send",
    running: "Running",
    completed: "Completed",
    steps: [
      "Read this comment and the issue description",
      "Check the training date and agenda",
      "Draft the invitation email",
    ],
    reply: "The invitation draft is ready, with the date, the agenda and the sign-up link. Edit it as you like.",
    captions: {
      idle: "A member is the assignee. Ask an agent for help without changing that.",
      type: "The comment is written. Click @ Mention to bring in an agent.",
      pick: "Pick an agent. Mentioning a member only notifies them, so members can't be picked here.",
      preview: "The preview under the composer shows who starts when you send. Click send.",
      running: "The agent works on this comment. The assignee is still Lei Li.",
      done: "Done. The assignee never changed. Use Replay to try again.",
    },
  },
  zh: {
    title: "OPS-15 · 准备下周的客户培训",
    assigneeLabel: "负责人",
    assignee: "李雷",
    you: "你",
    draft: "培训的邀请邮件还没写，",
    agentsGroup: "智能体",
    membersGroup: "成员",
    candidates: [
      { name: "文案助手", agent: true },
      { name: "运营助手", agent: true },
      { name: "李雷", agent: false },
    ] as Candidate[],
    memberNote: "仅通知",
    mention: "@ 提及",
    willStart: "发送后开始",
    send: "发送",
    running: "运行中",
    completed: "已完成",
    steps: ["读取这条评论和任务描述", "查看培训时间和议程", "起草邀请邮件"],
    reply: "邀请邮件草稿已写好，包含时间、议程和报名链接，你可以直接修改。",
    captions: {
      idle: "这条任务的负责人是成员。不改变负责人，也能请智能体帮忙。",
      type: "评论已经写好。点击\"@ 提及\"，请一个智能体来处理。",
      pick: "选择一个智能体。提及成员只会通知对方，所以这里不能选成员。",
      preview: "评论框下方的触发预览列出发送后会开始工作的智能体。点击发送。",
      running: "智能体处理这条评论，负责人仍然是 李雷。",
      done: "处理完成，负责人没有变化。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-15 · 来週の顧客トレーニングを準備する",
    assigneeLabel: "担当者",
    assignee: "李雷",
    you: "あなた",
    draft: "トレーニングの案内メールがまだ書けていません。",
    agentsGroup: "エージェント",
    membersGroup: "メンバー",
    candidates: [
      { name: "ライティングアシスタント", agent: true },
      { name: "運用アシスタント", agent: true },
      { name: "李雷", agent: false },
    ] as Candidate[],
    memberNote: "通知のみ",
    mention: "@ メンション",
    willStart: "送信後に作業を開始します",
    send: "送信",
    running: "実行中",
    completed: "完了",
    steps: ["このコメントとタスクの説明を読む", "トレーニングの日程とアジェンダを確認する", "案内メールを下書きする"],
    reply: "案内メールの下書きができました。日程、アジェンダ、申し込みリンクを入れてあります。自由に手を入れてください。",
    captions: {
      idle: "このタスクの担当者はメンバーです。担当者を変えずに、エージェントに手伝ってもらえます。",
      type: "コメントは書けています。「@ メンション」をクリックしてエージェントを呼びます。",
      pick: "エージェントを選びます。メンバーをメンションしても通知が届くだけなので、ここでは選べません。",
      preview: "入力欄の下のトリガープレビューに、送信すると作業を始めるエージェントが表示されます。送信をクリックします。",
      running: "エージェントがこのコメントに取り組みます。担当者は 李雷 のままです。",
      done: "完了しました。担当者は一度も変わっていません。「もう一度」でもう一度試せます。",
    },
  },
  ko: {
    title: "OPS-15 · 다음 주 고객 교육 준비",
    assigneeLabel: "담당자",
    assignee: "리레이",
    you: "나",
    draft: "교육 초대 메일을 아직 못 썼어요. ",
    agentsGroup: "에이전트",
    membersGroup: "멤버",
    candidates: [
      { name: "글쓰기 도우미", agent: true },
      { name: "운영 도우미", agent: true },
      { name: "리레이", agent: false },
    ] as Candidate[],
    memberNote: "알림만",
    mention: "@ 멘션",
    willStart: "전송 후 작업을 시작합니다",
    send: "보내기",
    running: "실행 중",
    completed: "완료됨",
    steps: ["이 댓글과 태스크 설명 읽기", "교육 일정과 안건 확인", "초대 메일 초안 작성"],
    reply: "초대 메일 초안을 작성했습니다. 일정, 안건, 신청 링크가 들어 있으니 자유롭게 고쳐 주세요.",
    captions: {
      idle: "이 태스크의 담당자는 멤버입니다. 담당자를 바꾸지 않고도 에이전트에게 도움을 요청할 수 있습니다.",
      type: "댓글은 다 썼습니다. '@ 멘션'을 클릭해 에이전트를 불러오세요.",
      pick: "에이전트를 선택하세요. 멤버를 멘션하면 알림만 가므로 여기서는 멤버를 고를 수 없습니다.",
      preview: "입력란 아래의 트리거 미리보기에 보내면 작업을 시작할 에이전트가 표시됩니다. 보내기를 클릭하세요.",
      running: "에이전트가 이 댓글을 처리합니다. 담당자는 여전히 리레이입니다.",
      done: "완료되었습니다. 담당자는 한 번도 바뀌지 않았습니다. '다시 보기'로 다시 해 보세요.",
    },
  },
  fr: {
    title: "OPS-15 · Préparer la formation clients de la semaine prochaine",
    assigneeLabel: "Assigné à",
    assignee: "Lei Li",
    you: "Vous",
    draft: "L'e-mail d'invitation à la formation n'est pas encore rédigé. ",
    agentsGroup: "Agents",
    membersGroup: "Membres",
    candidates: [
      { name: "Assistant rédaction", agent: true },
      { name: "Assistant ops", agent: true },
      { name: "Lei Li", agent: false },
    ] as Candidate[],
    memberNote: "Notification seule",
    mention: "@ Mentionner",
    willStart: "Démarrera à l'envoi",
    send: "Envoyer",
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire ce commentaire et la description de la tâche",
      "Vérifier la date et le programme de la formation",
      "Rédiger l'e-mail d'invitation",
    ],
    reply:
      "Le brouillon de l'invitation est prêt, avec la date, le programme et le lien d'inscription. Modifiez-le comme vous voulez.",
    captions: {
      idle: "Un membre est assigné à cette tâche. Vous pouvez demander l'aide d'un agent sans changer l'assigné.",
      type: "Le commentaire est rédigé. Cliquez sur « @ Mentionner » pour faire intervenir un agent.",
      pick: "Choisissez un agent. Mentionner un membre ne fait que le notifier, donc les membres ne sont pas proposés ici.",
      preview: "L'aperçu sous la zone de commentaire indique qui démarrera à l'envoi. Cliquez sur envoyer.",
      running: "L'agent traite ce commentaire. L'assigné reste Lei Li.",
      done: "Terminé. L'assigné n'a jamais changé. Utilisez Rejouer pour recommencer.",
    },
  },
};

type Phase = "idle" | "type" | "pick" | "preview" | "running" | "done";

// Every step advances by a click on the one control the caption names: the
// comment itself is fixed, so the demo can't reach a state it doesn't script.
export function MentionDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("type");
  const [agent, setAgent] = useState<string | null>(null);
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";
  const sent = phase === "running" || phase === "done";

  const pick = (name: string) => {
    setAgent(name);
    setPhase("preview");
  };

  const send = () => {
    setPhase("running");
    t.steps.forEach((_, i) => after(500 + i * 1000, () => setStepsShown(i + 1)));
    after(500 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("type");
    setAgent(null);
    setStepsShown(0);
  };

  const mentionToken = (name: string | null) => (
    <span className="font-medium text-brand">@{name ?? ""}</span>
  );

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-80 flex-col gap-3 p-4">
        <div className="flex items-center gap-2 text-caption text-muted-foreground">
          <span>{t.assigneeLabel}</span>
          <DemoAvatar name={t.assignee} />
          <span className="text-label text-foreground">{t.assignee}</span>
        </div>

        {sent && agent && (
          <>
            <DemoComment author={t.you}>
              {t.draft}
              {mentionToken(agent)}
            </DemoComment>
            <DemoRun agent={agent} status={phase === "done" ? t.completed : t.running} active={phase !== "done"}>
              {t.steps.slice(0, stepsShown).map((step, i) => (
                <DemoStep key={step} state={i === stepsShown - 1 && phase !== "done" ? "running" : "done"}>
                  {step}
                </DemoStep>
              ))}
            </DemoRun>
          </>
        )}

        {phase === "done" && agent && (
          <DemoComment author={agent} agent>
            {t.reply}
          </DemoComment>
        )}

        <div className="relative mt-auto rounded-lg border bg-background p-2">
          {phase === "pick" && (
            <div
              role="listbox"
              aria-label={t.agentsGroup}
              className="absolute bottom-full left-2 z-10 mb-1 w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
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
                          disabled={!candidate.agent}
                          onClick={() => pick(candidate.name)}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent disabled:opacity-50 disabled:hover:bg-transparent"
                        >
                          <DemoAvatar name={candidate.name} agent={candidate.agent} />
                          <span className="min-w-0 flex-1 truncate">{candidate.name}</span>
                          {!candidate.agent && <span className="text-caption text-muted-foreground">{t.memberNote}</span>}
                        </button>
                      </CoachMark>
                    ))}
                </div>
              ))}
            </div>
          )}

          <div className="min-h-12 px-1 text-body">
            {!sent && visible && (
              <>
                {t.draft}
                {phase === "type" ? (
                  <CoachMark active side="right" ringClassName="rounded-md">
                    <button
                      type="button"
                      onClick={() => setPhase("pick")}
                      className="rounded-md border border-dashed border-brand/50 px-1.5 text-label font-medium text-brand hover:bg-brand/7"
                    >
                      {t.mention}
                    </button>
                  </CoachMark>
                ) : (
                  mentionToken(agent)
                )}
              </>
            )}
          </div>

          <div className="mt-1.5 flex items-center gap-2">
            {phase === "preview" && agent && (
              <span className="inline-flex h-6 min-w-0 items-center gap-1.5 rounded-md bg-muted px-1.5 text-caption motion-safe:animate-in motion-safe:fade-in">
                <DemoAvatar name={agent} agent />
                <span className="truncate">{agent}</span>
                <span className="text-muted-foreground">· {t.willStart}</span>
              </span>
            )}
            <span className="ml-auto" />
            <CoachMark active={phase === "preview"} side="left" ringClassName="rounded-full">
              <DemoSendButton label={t.send} disabled={phase !== "preview"} onClick={send} />
            </CoachMark>
          </div>
        </div>
      </div>
    </DemoFrame>
  );
}
