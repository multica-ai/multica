"use client";

import { useState } from "react";
import { ArrowRight, Check } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { LocaleLink } from "@/components/locale-link";
import { CoachMark, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type Choice = {
  /** The situation, worded as in the page's table. */
  need: string;
  method: string;
  how: string;
  why: string;
  page: string;
  href: string;
};

const copy = {
  en: {
    title: "Which way should I start the work?",
    needsLabel: "What you need",
    useLabel: "Use",
    howLabel: "How",
    whyLabel: "Why",
    choices: [
      {
        need: "Have an agent own an issue",
        method: "Assign",
        how: "Open the issue, click Assignee, and choose an agent or squad.",
        why: "The agent reads the issue's description and discussion and writes progress and results back to the same issue.",
        page: "Assign issues to agents",
        href: "/assigning-issues",
      },
      {
        need: "Ask for help without changing the assignee",
        method: "@mention",
        how: "@mention the agent in a comment, or reply to its comment to continue.",
        why: "The comment becomes the starting point for the run; the issue's assignee and status stay unchanged.",
        page: "@-mention agents in comments",
        href: "/mentioning-agents",
      },
      {
        need: "Send the same kind of request often",
        method: "Quick action",
        how: "Save the request as a quick action and use it from the issue.",
        why: "Save who to ask and what to say once, then use the same request on other issues.",
        page: "Quick Actions",
        href: "/quick-actions",
      },
      {
        need: "Pass work through a repeatable set of steps",
        method: "Workflow",
        how: "Choose a project workflow and who takes over at each step.",
        why: "The description, discussion, and earlier results stay on the same issue, so whoever handles the next step picks up where the last one left off.",
        page: "Workflows",
        href: "/workflows",
      },
      {
        need: "Ask a question without creating an issue",
        method: "Chat",
        how: "Start a chat with the agent.",
        why: "A chat is not bound to any issue, and the entire conversation is private to you.",
        page: "Chat with agents",
        href: "/chat",
      },
      {
        need: "Start recurring work by time or an outside event",
        method: "Autopilot",
        how: "Create an autopilot with an assignee and one or more triggers.",
        why: "When triggered, Multica creates an issue or runs the agent directly, and keeps a record of every run.",
        page: "Autopilots",
        href: "/autopilots",
      },
      {
        need: "Have an agent come back to an issue later",
        method: "Wakeup",
        how: "In the issue sidebar, add a wakeup with a time or an event to wait for.",
        why: "When the time comes or the event happens, the agent starts a new run on the same issue and continues from where things stand.",
        page: "Issue wakeups",
        href: "/issue-wakeups",
      },
    ] as Choice[],
    readMore: (page: string) => `Read: ${page}`,
    captions: {
      idle: "Seven things you might want an agent to do.",
      start: "Pick what you need to see which way fits.",
      picked: (method: string) => `Use ${method}. Pick another need to compare.`,
    },
  },
  zh: {
    title: "该用哪种方式让智能体开始工作？",
    needsLabel: "你想做什么",
    useLabel: "使用",
    howLabel: "怎么做",
    whyLabel: "为什么",
    choices: [
      {
        need: "让智能体负责一条任务",
        method: "分配任务",
        how: "打开任务，点击负责人，选择智能体或小队。",
        why: "智能体会读取任务的说明和讨论，并把进展和结果写回原来的任务。",
        page: "分配任务给智能体",
        href: "/assigning-issues",
      },
      {
        need: "请求帮助，保留原来的负责人",
        method: "@提及",
        how: "在评论中 @智能体，或回复它的评论继续讨论。",
        why: "评论会成为这次执行的起点，任务的负责人和状态都不会改变。",
        page: "在评论中 @智能体",
        href: "/mentioning-agents",
      },
      {
        need: "经常发送同一类请求",
        method: "快捷操作",
        how: "把请求保存成快捷操作，在任务中直接使用。",
        why: "保存一次\"请谁来做、做什么\"，之后就能在其他任务上重复使用。",
        page: "快捷操作",
        href: "/quick-actions",
      },
      {
        need: "让任务按固定步骤交给不同处理人",
        method: "工作流",
        how: "为项目选择工作流，安排每一步由谁接手。",
        why: "任务的描述、讨论和之前的结果都留在原处，下一位处理人可以接着做。",
        page: "工作流",
        href: "/workflows",
      },
      {
        need: "问一个问题，不需要新建任务",
        method: "对话",
        how: "与智能体开始对话。",
        why: "对话不绑定某一条任务，整段对话完全私人。",
        page: "与智能体对话",
        href: "/chat",
      },
      {
        need: "按时间或外部事件开展重复性工作",
        method: "自动化",
        how: "创建自动化，选好执行方和触发器。",
        why: "触发后，Multica 会创建任务或直接运行智能体，并保留每一次运行记录。",
        page: "自动化",
        href: "/autopilots",
      },
      {
        need: "让智能体之后再回到某条任务",
        method: "任务唤醒",
        how: "在任务侧栏添加唤醒，设好要等的时间或事件。",
        why: "时间到了或事件发生后，智能体在同一条任务上开始新的运行，接着现在的进度做。",
        page: "任务唤醒",
        href: "/issue-wakeups",
      },
    ] as Choice[],
    readMore: (page: string) => `详见${page}`,
    captions: {
      idle: "你可能想让智能体做的七类事。",
      start: "选一件你想做的事，看看该用哪种方式。",
      // Space before a method that starts with "@" or a Latin letter, e.g. "用 @提及".
      picked: (method: string) => `用${/^[@A-Za-z]/.test(method) ? " " : ""}${method}。换一件事可以对比。`,
    },
  },
  ja: {
    title: "どの方法でエージェントに作業を始めてもらう？",
    needsLabel: "やりたいこと",
    useLabel: "使うもの",
    howLabel: "やり方",
    whyLabel: "理由",
    choices: [
      {
        need: "エージェントにタスクを任せる",
        method: "割り当て",
        how: "タスクを開き、担当者をクリックして、エージェントかスクワッドを選びます。",
        why: "エージェントはタスクの説明と議論を読み、進捗と結果を同じタスクに書き戻します。",
        page: "タスクをエージェントに割り当てる",
        href: "/assigning-issues",
      },
      {
        need: "担当者を変えずに相談する",
        method: "@メンション",
        how: "コメントでエージェントを @メンションするか、そのエージェントのコメントに返信して続けます。",
        why: "コメントがこの実行の起点になります。タスクの担当者とステータスは変わりません。",
        page: "コメントでエージェントを @メンション",
        href: "/mentioning-agents",
      },
      {
        need: "同じ種類の依頼を繰り返す",
        method: "クイックアクション",
        how: "依頼をクイックアクションとして保存し、タスクから使います。",
        why: "誰に何を頼むかを一度保存すれば、ほかのタスクでも同じ依頼を使えます。",
        page: "クイックアクション",
        href: "/quick-actions",
      },
      {
        need: "決まったステップで担当者を引き継ぐ",
        method: "ワークフロー",
        how: "プロジェクトのワークフローを選び、各ステップを誰が引き継ぐか決めます。",
        why: "説明、議論、それまでの結果が同じタスクに残るので、次のステップの担当者は前の続きから進められます。",
        page: "ワークフロー",
        href: "/workflows",
      },
      {
        need: "タスクを作らず質問する",
        method: "チャット",
        how: "エージェントとチャットを始めます。",
        why: "チャットはどのタスクにも紐づかず、会話全体があなただけのものです。",
        page: "エージェントとのチャット",
        href: "/chat",
      },
      {
        need: "時刻や外部イベントから繰り返し作業を始める",
        method: "オートパイロット",
        how: "担当者と 1 つ以上のトリガーを設定して、オートパイロットを作成します。",
        why: "トリガーされると、Multica はタスクを作成するか、エージェントを直接実行し、毎回の実行を記録に残します。",
        page: "オートパイロット",
        href: "/autopilots",
      },
      {
        need: "あとでエージェントにタスクへ戻ってもらう",
        method: "ウェイクアップ",
        how: "タスクのサイドバーで、待つ時刻やイベントを決めてウェイクアップを追加します。",
        why: "時刻になるかイベントが起きると、エージェントは同じタスクで新しい実行を始め、今の状況から続けます。",
        page: "タスクのウェイクアップ",
        href: "/issue-wakeups",
      },
    ] as Choice[],
    readMore: (page: string) => `詳しくは${page}`,
    captions: {
      idle: "エージェントに頼みたいことは、主に 7 種類あります。",
      start: "やりたいことを選ぶと、合う方法が表示されます。",
      picked: (method: string) => `${method}を使います。別の項目を選ぶと比べられます。`,
    },
  },
  ko: {
    title: "어떤 방식으로 작업을 시작할까요?",
    needsLabel: "하고 싶은 일",
    useLabel: "사용할 기능",
    howLabel: "방법",
    whyLabel: "이유",
    choices: [
      {
        need: "에이전트에게 태스크 맡기기",
        method: "할당",
        how: "태스크를 열고 담당자를 클릭한 뒤 에이전트나 스쿼드를 선택합니다.",
        why: "에이전트가 태스크의 설명과 논의를 읽고, 진행 상황과 결과를 같은 태스크에 기록합니다.",
        page: "태스크를 에이전트에게 할당하기",
        href: "/assigning-issues",
      },
      {
        need: "담당자를 바꾸지 않고 도움 요청하기",
        method: "@멘션",
        how: "댓글에서 에이전트를 @멘션하거나, 에이전트 댓글에 답글을 달아 이어 갑니다.",
        why: "댓글이 이번 실행의 출발점이 되며, 태스크의 담당자와 상태는 바뀌지 않습니다.",
        page: "댓글에서 에이전트 @멘션하기",
        href: "/mentioning-agents",
      },
      {
        need: "같은 종류의 요청 반복하기",
        method: "퀵 액션",
        how: "요청을 퀵 액션으로 저장해 태스크에서 사용합니다.",
        why: "누구에게 무엇을 요청할지 한 번 저장해 두면 다른 태스크에서도 같은 요청을 쓸 수 있습니다.",
        page: "퀵 액션",
        href: "/quick-actions",
      },
      {
        need: "정해진 단계마다 다른 담당자에게 넘기기",
        method: "워크플로",
        how: "프로젝트 워크플로를 선택하고 단계마다 누가 이어받을지 정합니다.",
        why: "설명, 논의, 이전 결과가 같은 태스크에 남아 있어 다음 단계 담당자가 이어서 작업할 수 있습니다.",
        page: "워크플로",
        href: "/workflows",
      },
      {
        need: "태스크를 만들지 않고 질문하기",
        method: "채팅",
        how: "에이전트와 채팅을 시작합니다.",
        why: "채팅은 특정 태스크에 묶이지 않으며, 대화 전체를 나만 볼 수 있습니다.",
        page: "에이전트와 대화",
        href: "/chat",
      },
      {
        need: "일정이나 외부 이벤트에 따라 반복 작업 시작하기",
        method: "오토파일럿",
        how: "담당자와 트리거를 하나 이상 정해 오토파일럿을 만듭니다.",
        why: "트리거되면 Multica가 태스크를 만들거나 에이전트를 바로 실행하고, 모든 실행 기록을 남깁니다.",
        page: "오토파일럿",
        href: "/autopilots",
      },
      {
        need: "나중에 에이전트가 태스크로 돌아오게 하기",
        method: "깨우기",
        how: "태스크 사이드바에서 기다릴 시각이나 이벤트를 정해 깨우기를 추가합니다.",
        why: "시각이 되거나 이벤트가 생기면 에이전트가 같은 태스크에서 새 실행을 시작해 지금 상황부터 이어 갑니다.",
        page: "태스크 깨우기",
        href: "/issue-wakeups",
      },
    ] as Choice[],
    readMore: (page: string) => `자세히: ${page}`,
    captions: {
      idle: "에이전트에게 맡길 만한 일 일곱 가지입니다.",
      start: "하고 싶은 일을 고르면 맞는 방법을 보여 줍니다.",
      // Phrased so no particle has to agree with the method's final consonant.
      picked: (method: string) => `${method} 기능을 사용하세요. 다른 일을 골라 비교해 보세요.`,
    },
  },
  fr: {
    title: "Comment lancer le travail ?",
    needsLabel: "Votre besoin",
    useLabel: "À utiliser",
    howLabel: "Comment",
    whyLabel: "Pourquoi",
    choices: [
      {
        need: "Confier une tâche à un agent",
        method: "Assignation",
        how: "Ouvrez la tâche, cliquez sur Assigné à et choisissez un agent ou un squad.",
        why: "L'agent lit la description et la discussion de la tâche, puis réécrit l'avancement et les résultats dans la même tâche.",
        page: "Assigner des tâches aux agents",
        href: "/assigning-issues",
      },
      {
        need: "Demander de l'aide sans changer le responsable",
        method: "@mention",
        how: "@mentionnez l'agent dans un commentaire, ou répondez à son commentaire pour continuer.",
        why: "Le commentaire devient le point de départ de l'exécution ; l'assigné et le statut de la tâche ne changent pas.",
        page: "@mentionner des agents dans les commentaires",
        href: "/mentioning-agents",
      },
      {
        need: "Envoyer souvent le même type de demande",
        method: "Action rapide",
        how: "Enregistrez la demande comme action rapide et utilisez-la depuis la tâche.",
        why: "Enregistrez une fois à qui demander et quoi dire, puis réutilisez la même demande sur d'autres tâches.",
        page: "Actions rapides",
        href: "/quick-actions",
      },
      {
        need: "Transmettre le travail selon des étapes répétées",
        method: "Workflow",
        how: "Choisissez un workflow de projet et qui prend le relais à chaque étape.",
        why: "La description, la discussion et les résultats précédents restent sur la même tâche : la personne suivante reprend là où la précédente s'est arrêtée.",
        page: "Workflows",
        href: "/workflows",
      },
      {
        need: "Poser une question sans créer de tâche",
        method: "Discussion",
        how: "Démarrez une discussion avec l'agent.",
        why: "Une discussion n'est liée à aucune tâche, et toute la conversation reste privée.",
        page: "Discuter avec des agents",
        href: "/chat",
      },
      {
        need: "Commencer un travail récurrent selon une date ou un événement extérieur",
        method: "Automatisation",
        how: "Créez une automatisation avec un assigné et un ou plusieurs déclencheurs.",
        why: "Une fois déclenchée, Multica crée une tâche ou exécute directement l'agent, et garde une trace de chaque exécution.",
        page: "Automatisations",
        href: "/autopilots",
      },
      {
        need: "Faire revenir un agent sur une tâche plus tard",
        method: "Réveil",
        how: "Dans la barre latérale de la tâche, ajoutez un réveil avec l'heure ou l'événement à attendre.",
        why: "À l'heure dite ou quand l'événement se produit, l'agent démarre une nouvelle exécution sur la même tâche et reprend là où en sont les choses.",
        page: "Réveils de tâches",
        href: "/issue-wakeups",
      },
    ] as Choice[],
    readMore: (page: string) => `Voir : ${page}`,
    captions: {
      idle: "Sept choses que vous pourriez demander à un agent.",
      start: "Choisissez votre besoin pour voir la méthode adaptée.",
      picked: (method: string) => `À utiliser : ${method}. Choisissez un autre besoin pour comparer.`,
    },
  },
};

export function TriggerChooserDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [picked, setPicked] = useState<number | null>(null);

  const choice = picked === null ? null : t.choices[picked];
  const caption = !visible ? t.captions.idle : choice ? t.captions.picked(choice.method) : t.captions.start;

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={() => setPicked(null)}>
      <div className="grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1.1fr)]">
        <div className="min-w-0">
          <div id="trigger-chooser-needs" className="mb-1.5 text-micro text-muted-foreground">
            {t.needsLabel}
          </div>
          <ul aria-labelledby="trigger-chooser-needs" className="flex flex-col gap-1">
            {t.choices.map((c, i) => (
              <li key={c.need} className="flex">
                <CoachMark
                  active={visible && picked === null && i === 0}
                  side="right"
                  ringClassName="rounded-md"
                  className="flex w-full"
                >
                  <button
                    type="button"
                    aria-pressed={picked === i}
                    disabled={!visible}
                    onClick={() => setPicked(i)}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-md border px-2.5 py-1.5 text-left text-body transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
                      picked === i ? "border-brand bg-brand/10 font-medium" : "bg-background hover:bg-muted",
                    )}
                  >
                    <span className="min-w-0 flex-1">{c.need}</span>
                    {picked === i && <Check aria-hidden className="size-3.5 shrink-0 text-brand" />}
                  </button>
                </CoachMark>
              </li>
            ))}
          </ul>
        </div>

        <div className={cn("min-w-0", !choice && "max-sm:hidden")}>
          {choice ? (
            <div
              key={picked}
              className="flex h-full flex-col gap-2.5 rounded-lg border bg-background p-3 motion-safe:animate-in motion-safe:fade-in"
            >
              <div>
                <div className="text-micro text-muted-foreground">{t.useLabel}</div>
                <div className="text-title-sm font-medium text-brand">{choice.method}</div>
              </div>
              <div>
                <div className="text-micro text-muted-foreground">{t.howLabel}</div>
                <p className="text-body">{choice.how}</p>
              </div>
              <div>
                <div className="text-micro text-muted-foreground">{t.whyLabel}</div>
                <p className="text-body text-muted-foreground">{choice.why}</p>
              </div>
              <LocaleLink
                href={choice.href}
                className="mt-auto inline-flex w-fit items-center gap-1 text-label font-medium text-brand hover:underline"
              >
                {t.readMore(choice.page)}
                <ArrowRight aria-hidden className="size-3.5" />
              </LocaleLink>
            </div>
          ) : (
            <div aria-hidden className="h-full rounded-lg border border-dashed" />
          )}
        </div>
      </div>
    </DemoFrame>
  );
}
