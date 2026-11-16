"use client";

import { useRef, useState } from "react";
import { ArrowRight, ChevronDown, ChevronRight, CornerDownRight, X } from "@multica/ui/icons";
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

// The Content template's middle columns (packages/views/workflows/templates.ts)
// between the fixed todo / done / cancelled columns every workflow lists.
type Status = "todo" | "drafting" | "editing" | "scheduled" | "done" | "cancelled";
type Handler = "you" | "agent" | "member";

const PICKER: Status[] = ["todo", "drafting", "editing", "scheduled", "done", "cancelled"];
const STEPS: Status[] = ["drafting", "editing", "scheduled"];
const HANDLERS: Partial<Record<Status, Handler>> = { drafting: "you", editing: "agent", scheduled: "member" };

const statusDot: Record<Status, string> = {
  todo: "border-muted-foreground",
  drafting: "border-warning bg-warning/40",
  editing: "border-brand bg-brand/40",
  scheduled: "border-success bg-success/40",
  done: "border-success bg-success",
  cancelled: "border-muted-foreground bg-muted-foreground/50",
};

const copy = {
  en: {
    title: "OPS-31 · Write the anniversary event announcement",
    issue: "OPS-31",
    workflow: "Content",
    project: "Anniversary online event",
    statusLabel: "Status",
    assigneeLabel: "Assignee",
    names: { you: "You", agent: "Writing helper", member: "Lei Li" } as Record<Handler, string>,
    status: {
      todo: "Todo",
      drafting: "Drafting",
      editing: "Editing",
      scheduled: "Scheduled",
      done: "Done",
      cancelled: "Cancelled",
    } as Record<Status, string>,
    handsOffTo: (name: string) => `Hands off to ${name}`,
    then: (status: string) => `Then → ${status}`,
    pickerHeader: (workflow: string, project: string) => `${workflow} · from project "${project}"`,
    pickerFooter: "Only this issue's workflow statuses are shown. The right side is who entering the status hands it to.",
    confirmTitle: (issue: string, status: string) => `Move ${issue} to "${status}"?`,
    reassignRun: (from: string, to: string) => [`The assignee changes from `, from, ` to `, to, `, who starts working right away.`],
    briefToggle: (name: string) => `What ${name} receives`,
    brief: "Polish the title and body in the brand voice, fix typos, check the event dates and prize rules, then move the issue to Scheduled.",
    cancel: "Cancel",
    confirm: (status: string) => `Move to ${status}`,
    close: "Close",
    changed: (from: string, to: string) => `changed status from ${from} to ${to}`,
    handedTo: "Handed to",
    runStarted: "and started a run",
    memberNoRun: "(member, no run)",
    queued: "Queued",
    running: "Running",
    completed: "Completed",
    steps: [
      "Read the issue description, comments and handoff brief",
      "Tighten the title and fix two typos",
      "Check the event dates, prize rules and sign-up link",
      "Set the status to Scheduled",
    ],
    reply: "The post is edited: a shorter title and two date fixes. Lei Li, it is ready to schedule.",
    captions: {
      idle: "An issue in a project that uses the Content workflow.",
      start: "Your draft is written. Click Status to pass the issue to the next step.",
      pick: "The list only has this workflow's steps, and shows who each one hands to. Pick Editing.",
      confirm: "The confirmation names the new assignee, and you can read the brief it will receive. Confirm, or cancel to keep the issue where it is.",
      kept: "Nothing moved: the issue stays in Drafting and you are still the assignee. Click Status to try again.",
      running: "Writing helper took over and started a run. The description, comments and earlier work stay on the issue.",
      done: "Writing helper followed \"Then\" and moved the issue to Scheduled, which hands it to Lei Li. A member handoff only changes the assignee and starts no run.",
    },
  },
  zh: {
    title: "OPS-31 · 写周年庆活动推文",
    issue: "OPS-31",
    workflow: "内容生产",
    project: "周年庆线上活动",
    statusLabel: "状态",
    assigneeLabel: "负责人",
    names: { you: "你", agent: "文案助手", member: "李雷" } as Record<Handler, string>,
    status: {
      todo: "待办",
      drafting: "撰写中",
      editing: "编辑中",
      scheduled: "已排期",
      done: "已完成",
      cancelled: "已取消",
    } as Record<Status, string>,
    handsOffTo: (name: string) => `交给 ${name}`,
    then: (status: string) => `完成后 → ${status}`,
    pickerHeader: (workflow: string, project: string) => `${workflow} · 跟随项目"${project}"`,
    pickerFooter: "只显示这个任务所在工作流里的状态。右侧是进入该状态时的处理人。",
    confirmTitle: (issue: string, status: string) => `将 ${issue} 移到"${status}"？`,
    reassignRun: (from: string, to: string) => [`负责人将从 `, from, ` 改为 `, to, `，它会立即开始运行。`],
    briefToggle: (name: string) => `${name} 会收到的说明`,
    brief: "按品牌语气润色标题和正文，改掉错别字，核对活动时间和抽奖规则，改好后把任务移到已排期。",
    cancel: "取消",
    confirm: (status: string) => `移到${status}`,
    close: "关闭",
    changed: (from: string, to: string) => `状态从 ${from} 改为 ${to}`,
    handedTo: "已交给",
    runStarted: "并开始运行",
    memberNoRun: "（成员，不启动运行）",
    queued: "排队中",
    running: "运行中",
    completed: "已完成",
    steps: [
      "读取任务描述、评论和交接说明",
      "缩短标题，改掉两处错别字",
      "核对活动时间、抽奖规则和报名链接",
      "把状态改为 已排期",
    ],
    reply: "推文已改好：标题换成了更短的版本，修正了两处日期。李雷，可以安排发布了。",
    captions: {
      idle: "一条任务，所在项目使用内容生产工作流。",
      start: "你的初稿写完了。点击状态，把任务交给下一步。",
      pick: "列表里只有这个工作流的步骤，右侧标出进入后交给谁。选择编辑中。",
      confirm: "确认窗口写明了新的负责人，还能查看它会收到的说明。确认交接，或取消留在原处。",
      kept: "没有交接：任务留在撰写中，负责人还是你。点击状态可以再试一次。",
      running: "文案助手接手并开始运行。任务的描述、评论和之前的成果都留在原处。",
      done: "文案助手按\"完成后进入\"把任务移到已排期，负责人随之换成李雷。交给成员只改负责人，不启动运行。",
    },
  },
  ja: {
    title: "OPS-31 · 周年記念イベントの告知投稿を書く",
    issue: "OPS-31",
    workflow: "コンテンツ制作",
    project: "周年記念オンラインイベント",
    statusLabel: "ステータス",
    assigneeLabel: "担当者",
    names: { you: "あなた", agent: "ライティングアシスタント", member: "李雷" } as Record<Handler, string>,
    status: {
      todo: "未着手",
      drafting: "執筆中",
      editing: "編集中",
      scheduled: "公開予約済み",
      done: "完了",
      cancelled: "キャンセル済み",
    } as Record<Status, string>,
    handsOffTo: (name: string) => `${name} に引き継ぐ`,
    then: (status: string) => `完了後 → ${status}`,
    pickerHeader: (workflow: string, project: string) => `${workflow} · プロジェクト「${project}」に従う`,
    pickerFooter: "このタスクのワークフローのステータスだけを表示しています。右側はそのステータスに入ったときの引き継ぎ先です。",
    confirmTitle: (issue: string, status: string) => `${issue} を「${status}」に移動しますか？`,
    reassignRun: (from: string, to: string) => [`担当者が `, from, ` から `, to, ` に変わり、すぐに実行を始めます。`],
    briefToggle: (name: string) => `${name} が受け取る説明`,
    brief: "ブランドのトーンに合わせてタイトルと本文を整え、誤字を直し、イベントの日程と抽選ルールを確認してから、タスクを公開予約済みに移動する。",
    cancel: "キャンセル",
    confirm: (status: string) => `${status}に移動`,
    close: "閉じる",
    changed: (from: string, to: string) => `ステータスを ${from} から ${to} に変更しました`,
    handedTo: "引き継ぎ先",
    runStarted: "実行を開始",
    memberNoRun: "（メンバー、実行なし）",
    queued: "待機中",
    running: "実行中",
    completed: "完了",
    steps: [
      "タスクの説明、コメント、引き継ぎの説明を読む",
      "タイトルを短くし、誤字を 2 か所直す",
      "イベントの日程、抽選ルール、申し込みリンクを確認する",
      "ステータスを 公開予約済み に変更",
    ],
    reply: "投稿を直しました。タイトルを短くし、日付を 2 か所修正しています。李雷さん、公開の予約をお願いします。",
    captions: {
      idle: "コンテンツ制作ワークフローを使うプロジェクトのタスク。",
      start: "下書きが書き終わりました。ステータスをクリックして、タスクを次のステップに渡します。",
      pick: "一覧にはこのワークフローのステップだけが並び、右側にそれぞれの引き継ぎ先が出ます。編集中を選んでください。",
      confirm: "確認ダイアログに新しい担当者が表示され、その担当者が受け取る説明も読めます。確定するか、キャンセルしてそのままにします。",
      kept: "何も動いていません。タスクは執筆中のままで、担当者もあなたのままです。ステータスをクリックしてもう一度試せます。",
      running: "ライティングアシスタントが引き継いで実行を始めました。説明、コメント、それまでの成果はタスクに残っています。",
      done: "ライティングアシスタントは「完了したら移動」に従ってタスクを公開予約済みに移し、担当者は李雷に替わりました。メンバーへの引き継ぎは担当者を変えるだけで、実行は始まりません。",
    },
  },
  ko: {
    title: "OPS-31 · 창립 기념 이벤트 공지 게시물 작성",
    issue: "OPS-31",
    workflow: "콘텐츠 제작",
    project: "창립 기념 온라인 이벤트",
    statusLabel: "상태",
    assigneeLabel: "담당자",
    names: { you: "나", agent: "글쓰기 도우미", member: "리레이" } as Record<Handler, string>,
    status: {
      todo: "할 일",
      drafting: "작성 중",
      editing: "편집 중",
      scheduled: "예약됨",
      done: "완료",
      cancelled: "취소됨",
    } as Record<Status, string>,
    handsOffTo: (name: string) => `${name}에게 인계`,
    then: (status: string) => `완료 후 → ${status}`,
    pickerHeader: (workflow: string, project: string) => `${workflow} · 프로젝트 “${project}” 기준`,
    pickerFooter: "이 태스크 워크플로의 상태만 표시합니다. 오른쪽은 해당 상태에 들어갈 때의 처리자입니다.",
    confirmTitle: (issue: string, status: string) => `${issue}을(를) “${status}”(으)로 옮길까요?`,
    reassignRun: (from: string, to: string) => [`담당자가 `, from, `에서 `, to, `(으)로 바뀌고 바로 실행을 시작합니다.`],
    briefToggle: (name: string) => `${name}이(가) 받는 설명`,
    brief: "브랜드 말투에 맞춰 제목과 본문을 다듬고, 오탈자를 고치고, 이벤트 일정과 추첨 규칙을 확인한 뒤 태스크를 예약됨으로 옮긴다.",
    cancel: "취소",
    confirm: (status: string) => `${status}(으)로 이동`,
    close: "닫기",
    changed: (from: string, to: string) => `상태를 ${from}에서 ${to}로 변경했습니다`,
    handedTo: "인계 대상",
    runStarted: "실행 시작",
    memberNoRun: "(멤버, 실행 없음)",
    queued: "대기 중",
    running: "실행 중",
    completed: "완료됨",
    steps: [
      "태스크 설명, 댓글, 인계 설명 읽기",
      "제목을 줄이고 오탈자 두 곳 수정",
      "이벤트 일정, 추첨 규칙, 신청 링크 확인",
      "상태를 예약됨으로 변경",
    ],
    reply: "게시물을 다듬었습니다. 제목을 더 짧게 바꾸고 날짜 두 곳을 고쳤습니다. 리레이 님, 게시 예약을 부탁드립니다.",
    captions: {
      idle: "콘텐츠 제작 워크플로를 쓰는 프로젝트의 태스크입니다.",
      start: "초안을 다 썼습니다. 상태를 클릭해 태스크를 다음 단계로 넘기세요.",
      pick: "목록에는 이 워크플로의 단계만 있고, 오른쪽에 각 단계의 인계 대상이 표시됩니다. 편집 중을 선택하세요.",
      confirm: "확인 창에 새 담당자가 표시되고, 그 담당자가 받을 설명도 볼 수 있습니다. 확인하거나, 취소해서 그대로 두세요.",
      kept: "아무것도 바뀌지 않았습니다. 태스크는 작성 중에 그대로 있고 담당자도 여전히 나입니다. 상태를 클릭해 다시 해 보세요.",
      running: "글쓰기 도우미가 넘겨받아 실행을 시작했습니다. 설명, 댓글, 이전 결과는 태스크에 그대로 남아 있습니다.",
      done: "글쓰기 도우미가 “완료 후 이동”에 따라 태스크를 예약됨으로 옮겼고, 담당자는 리레이로 바뀌었습니다. 멤버에게 넘기면 담당자만 바뀌고 실행은 시작되지 않습니다.",
    },
  },
  fr: {
    title: "OPS-31 · Rédiger l'annonce de l'événement anniversaire",
    issue: "OPS-31",
    workflow: "Contenu",
    project: "Événement en ligne de l'anniversaire",
    statusLabel: "Statut",
    assigneeLabel: "Assigné à",
    names: { you: "Vous", agent: "Assistant rédaction", member: "Lei Li" } as Record<Handler, string>,
    status: {
      todo: "À faire",
      drafting: "Rédaction",
      editing: "Relecture",
      scheduled: "Planifié",
      done: "Terminé",
      cancelled: "Annulé",
    } as Record<Status, string>,
    handsOffTo: (name: string) => `Confiée à ${name}`,
    then: (status: string) => `puis → ${status}`,
    pickerHeader: (workflow: string, project: string) => `${workflow} · selon le projet « ${project} »`,
    pickerFooter: "Seuls les états du workflow de cette tâche sont affichés. À droite : à qui elle est confiée en entrant dans l’état.",
    confirmTitle: (issue: string, status: string) => `Déplacer ${issue} vers « ${status} » ?`,
    reassignRun: (from: string, to: string) => [`L’assigné passe de `, from, ` à `, to, `, qui commence aussitôt.`],
    briefToggle: (name: string) => `Ce que reçoit ${name}`,
    brief: "Retravailler le titre et le texte dans le ton de la marque, corriger les coquilles, vérifier les dates de l'événement et le règlement du tirage, puis passer la tâche à Planifié.",
    cancel: "Annuler",
    confirm: (status: string) => `Déplacer vers ${status}`,
    close: "Fermer",
    changed: (from: string, to: string) => `a changé le statut de ${from} à ${to}`,
    handedTo: "Confiée à",
    runStarted: "exécution lancée",
    memberNoRun: "(membre, sans exécution)",
    queued: "En file d'attente",
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire la description de la tâche, les commentaires et les consignes de relais",
      "Raccourcir le titre et corriger deux coquilles",
      "Vérifier les dates, le règlement du tirage et le lien d'inscription",
      "Passer le statut à Planifié",
    ],
    reply: "L'annonce est relue : un titre plus court et deux dates corrigées. Lei Li, elle est prête à être planifiée.",
    captions: {
      idle: "Une tâche dans un projet qui utilise le workflow Contenu.",
      start: "Votre brouillon est écrit. Cliquez sur Statut pour passer la tâche à l'étape suivante.",
      pick: "La liste ne contient que les étapes de ce workflow et indique à qui chacune confie la tâche. Choisissez Relecture.",
      confirm: "La confirmation indique le nouvel assigné, et vous pouvez lire les consignes qu'il recevra. Confirmez, ou annulez pour laisser la tâche où elle est.",
      kept: "Rien n'a bougé : la tâche reste en Rédaction et vous en êtes toujours l'assigné. Cliquez sur Statut pour réessayer.",
      running: "Assistant rédaction a pris le relais et lancé une exécution. La description, les commentaires et le travail déjà fait restent sur la tâche.",
      done: "Assistant rédaction a suivi « Une fois terminé, passer à » et déplacé la tâche vers Planifié, qui la confie à Lei Li. Un relais vers un membre ne change que l'assigné et ne lance aucune exécution.",
    },
  },
};

type Phase = "idle" | "start" | "pick" | "confirm" | "kept" | "running" | "done";

type Entry = { actor: Handler; from: Status; to: Status; handedTo: Handler };

export function WorkflowDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");
  const [status, setStatus] = useState<Status>("drafting");
  const [assignee, setAssignee] = useState<Handler>("you");
  const [entries, setEntries] = useState<Entry[]>([]);
  const [briefOpen, setBriefOpen] = useState(false);
  const [queued, setQueued] = useState(true);
  const [stepsShown, setStepsShown] = useState(0);
  const statusRef = useRef<Status>("drafting");

  const shownPhase: Phase = visible ? phase : "idle";

  // A status change that enters a handoff step reassigns the issue in the same write.
  const enter = (to: Status, actor: Handler) => {
    const from = statusRef.current;
    const handedTo = HANDLERS[to]!;
    statusRef.current = to;
    setStatus(to);
    setAssignee(handedTo);
    setEntries((items) => [...items, { actor, from, to, handedTo }]);
  };

  const confirm = () => {
    enter("editing", "you");
    setPhase("running");
    after(900, () => setQueued(false));
    t.steps.forEach((_, i) =>
      after(900 + i * 1000, () => {
        setStepsShown(i + 1);
        if (i === t.steps.length - 1) enter("scheduled", "agent");
      }),
    );
    after(900 + t.steps.length * 1000 + 500, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("start");
    statusRef.current = "drafting";
    setStatus("drafting");
    setAssignee("you");
    setEntries([]);
    setBriefOpen(false);
    setQueued(true);
    setStepsShown(0);
  };

  const canOpenPicker = visible && (phase === "start" || phase === "kept" || phase === "pick");
  const runActive = phase === "running";
  const showRun = phase === "running" || phase === "done";
  const name = (h: Handler) => t.names[h];

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-[28rem] flex-col gap-3 p-4">
        <ol className="flex flex-wrap items-stretch gap-1.5" aria-label={t.workflow}>
          {STEPS.map((step, i) => {
            const handler = HANDLERS[step]!;
            const next = STEPS[i + 1];
            return (
              <li key={step} className="m-0 flex min-w-0 items-center gap-1.5">
                <div
                  className={cn(
                    "min-w-0 rounded-lg border px-2 py-1 transition-colors",
                    status === step ? "border-brand/40 bg-brand/5" : "bg-background",
                  )}
                >
                  <div className="flex items-center gap-1.5 text-label font-medium">
                    <span aria-hidden className={cn("size-2.5 rounded-full border-2", statusDot[step])} />
                    {t.status[step]}
                  </div>
                  <div className="flex items-center gap-1 text-micro text-muted-foreground">
                    <DemoAvatar name={name(handler)} agent={handler === "agent"} />
                    <span className="truncate">{t.handsOffTo(name(handler))}</span>
                  </div>
                  {handler === "agent" && next && (
                    <div className="text-micro text-muted-foreground">{t.then(t.status[next])}</div>
                  )}
                </div>
                {next && <ArrowRight aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />}
              </li>
            );
          })}
        </ol>

        <dl className="grid w-fit grid-cols-[auto_auto] items-center gap-x-6 gap-y-2 text-body">
          <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
          <dd className="relative">
            <CoachMark
              active={visible && (phase === "start" || phase === "kept")}
              side="right"
              ringClassName="rounded-md"
            >
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={phase === "pick"}
                disabled={!canOpenPicker}
                onClick={() => setPhase(phase === "pick" ? "start" : "pick")}
                className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-muted disabled:hover:bg-transparent"
              >
                <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", statusDot[status])} />
                <span key={status} className="motion-safe:animate-in motion-safe:fade-in">
                  {t.status[status]}
                </span>
                {canOpenPicker && <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />}
              </button>
            </CoachMark>
            {phase === "pick" && (
              <div
                role="listbox"
                aria-label={t.statusLabel}
                className="absolute top-full left-0 z-10 mt-1 w-72 max-w-[calc(100vw-7.5rem)] rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                <div className="truncate px-2 pt-1 pb-1 text-micro text-muted-foreground">
                  {t.pickerHeader(t.workflow, t.project)}
                </div>
                {PICKER.map((s) => {
                  const handler = HANDLERS[s];
                  return (
                    <CoachMark quiet key={s} active={s === "editing"} ringClassName="rounded-md" className="flex w-full">
                      <button
                        type="button"
                        role="option"
                        aria-selected={s === status}
                        disabled={s !== "editing"}
                        onClick={() => setPhase("confirm")}
                        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-body hover:bg-accent disabled:opacity-45 disabled:hover:bg-transparent"
                      >
                        <span aria-hidden className={cn("size-3 shrink-0 rounded-full border-2", statusDot[s])} />
                        <span className="min-w-0 flex-1 truncate">{t.status[s]}</span>
                        {s === status ? (
                          <span className="text-caption text-muted-foreground">✓</span>
                        ) : (
                          handler && (
                            <span className="flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
                              <DemoAvatar name={name(handler)} agent={handler === "agent"} />
                              {name(handler)}
                            </span>
                          )
                        )}
                      </button>
                    </CoachMark>
                  );
                })}
                <div className="mt-1 border-t px-2 pt-1.5 pb-1 text-micro text-muted-foreground">{t.pickerFooter}</div>
              </div>
            )}
          </dd>
          <dt className="text-caption text-muted-foreground">{t.assigneeLabel}</dt>
          <dd key={assignee} className="flex items-center gap-1.5 motion-safe:animate-in motion-safe:fade-in">
            <DemoAvatar name={name(assignee)} agent={assignee === "agent"} />
            {name(assignee)}
          </dd>
        </dl>

        {entries.length > 0 && (
          <ul className="flex list-none flex-col gap-1.5 p-0">
            {entries.map((entry, i) => (
              <li key={i} className="m-0 min-w-0 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
                <div className="flex min-w-0 items-center gap-1.5">
                  <DemoAvatar name={name(entry.actor)} agent={entry.actor === "agent"} />
                  <span className="font-medium text-foreground">{name(entry.actor)}</span>
                  <span className="min-w-0 truncate">{t.changed(t.status[entry.from], t.status[entry.to])}</span>
                </div>
                <div className="mt-0.5 flex flex-wrap items-center gap-1.5 pl-6">
                  <CornerDownRight aria-hidden className="size-3 shrink-0" />
                  <span>{t.handedTo}</span>
                  <DemoAvatar name={name(entry.handedTo)} agent={entry.handedTo === "agent"} />
                  <span className="font-medium text-foreground">{name(entry.handedTo)}</span>
                  <span>{entry.handedTo === "member" ? t.memberNoRun : t.runStarted}</span>
                </div>
              </li>
            ))}
          </ul>
        )}

        {showRun && (
          <DemoRun
            agent={name("agent")}
            status={runActive ? (queued ? t.queued : t.running) : t.completed}
            active={runActive}
          >
            {t.steps.slice(0, stepsShown).map((step, i) => (
              <DemoStep key={step} state={i === stepsShown - 1 && runActive ? "running" : "done"}>
                {step}
              </DemoStep>
            ))}
          </DemoRun>
        )}

        {phase === "done" && (
          <DemoComment author={name("agent")} agent>
            {t.reply}
          </DemoComment>
        )}

        {phase === "confirm" && (
          <div className="absolute inset-0 z-20 flex items-center justify-center bg-background/60 p-4 backdrop-blur-[2px] motion-safe:animate-in motion-safe:fade-in">
            <div
              role="dialog"
              aria-modal="false"
              aria-labelledby="workflow-demo-title"
              className="w-full max-w-md rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:zoom-in-95"
            >
              <div className="flex items-start gap-2 p-4 pb-3">
                <div className="min-w-0 flex-1">
                  <div id="workflow-demo-title" className="text-title-sm font-medium">
                    {t.confirmTitle(t.issue, t.status.editing)}
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-1.5 text-caption text-muted-foreground">
                    <span className="inline-flex items-center gap-1">
                      <span aria-hidden className={cn("size-2.5 rounded-full border-2", statusDot.drafting)} />
                      {t.status.drafting}
                    </span>
                    <ArrowRight aria-hidden className="size-3" />
                    <span className="inline-flex items-center gap-1">
                      <span aria-hidden className={cn("size-2.5 rounded-full border-2", statusDot.editing)} />
                      {t.status.editing}
                    </span>
                    <span aria-hidden>·</span>
                    <span>{t.workflow}</span>
                  </div>
                </div>
                <button
                  type="button"
                  aria-label={t.close}
                  onClick={() => setPhase("kept")}
                  className="rounded-md p-1 text-muted-foreground hover:bg-muted"
                >
                  <X className="size-4" />
                </button>
              </div>
              <div className="space-y-3 px-4 pb-4">
                <p className="text-body leading-7">
                  {t.reassignRun(name("you"), name("agent")).map((part, i) =>
                    i % 2 === 1 ? (
                      <span key={i} className="mx-0.5 inline-flex items-center gap-1 align-middle font-medium">
                        <DemoAvatar name={part} agent={i === 3} />
                        {part}
                      </span>
                    ) : (
                      <span key={i}>{part}</span>
                    ),
                  )}
                </p>
                <div>
                  <button
                    type="button"
                    aria-expanded={briefOpen}
                    onClick={() => setBriefOpen((v) => !v)}
                    className="flex items-center gap-1 text-caption font-medium text-brand hover:underline"
                  >
                    {briefOpen ? (
                      <ChevronDown aria-hidden className="size-3.5" />
                    ) : (
                      <ChevronRight aria-hidden className="size-3.5" />
                    )}
                    {t.briefToggle(name("agent"))}
                  </button>
                  {briefOpen && (
                    <div className="mt-2 rounded-lg border bg-muted/40 px-3 py-2 text-caption motion-safe:animate-in motion-safe:fade-in">
                      {t.brief}
                    </div>
                  )}
                </div>
              </div>
              <div className="flex justify-end gap-2 rounded-b-xl border-t bg-muted/40 px-4 py-3">
                <Button type="button" variant="outline" size="sm" onClick={() => setPhase("kept")}>
                  {t.cancel}
                </Button>
                <CoachMark active side="bottom" align="end" ringClassName="rounded-lg">
                  <Button type="button" size="sm" onClick={confirm}>
                    {t.confirm(t.status.editing)}
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
