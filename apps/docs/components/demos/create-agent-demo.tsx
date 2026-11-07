"use client";

import { useState, type ReactNode } from "react";
import { ArrowRight, AtSign, Bot, Check, CircleCheck, ListPlus, Monitor, Sparkles, Users } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoFrame, useDemoCopy, useDemoTimers, useStartWhenVisible } from "./demo-kit";

type Harness = { id: string; label: string; version: string };
type Access = { title: string; desc: string };

const copy = {
  en: {
    frameTitle: "New agent",
    shellTitle: "Create an agent",
    steps: ["Choose runtime", "Configure", "Done"],
    runtimeTitle: "Where should this agent run?",
    machinesGroup: "Machines",
    thisMachine: "This machine",
    harnessCount: "2 harnesses",
    harnesses: [
      { id: "claude", label: "Claude", version: "v2.1" },
      { id: "codex", label: "Codex", version: "v0.46" },
    ] as Harness[],
    selected: "Selected:",
    selectedSummary: "Claude · This machine",
    cancel: "Cancel",
    next: "Next",
    configureTitle: "Configure the agent",
    askAi: "Let AI write it",
    identity: "Identity",
    nameLabel: "Name",
    name: "Ops helper",
    descriptionLabel: "Description",
    description: "Weekly reports, event prep and day-to-day ops chores",
    behavior: "Behavior & capabilities",
    instructionsLabel: "Instructions",
    instructions: [
      "You handle the team's day-to-day operations: weekly reports, event prep, customer feedback roundups.",
      "Read the description and comments first. If something is missing, ask in a comment instead of guessing.",
      "Deliver in the issue comments, in short paragraphs and lists. Check with the assignee before spending money or publishing anything.",
    ],
    skillsLabel: "Skills",
    skillsPlaceholder: "Add skills from workspace",
    execution: "Execution",
    executionHint: "Unspecified options use the runtime defaults.",
    runtimeLabel: "Runtime",
    runtime: "Claude (MacBook)",
    change: "Change",
    modelLabel: "Model",
    model: "Default (provider)",
    accessLabel: "Access",
    access: [
      { title: "Only me", desc: "Only you can run this agent." },
      { title: "Entire workspace", desc: "Every workspace member can run it." },
      { title: "Specific people", desc: "Only selected members can run it." },
    ] as Access[],
    create: "Create agent",
    creating: "Creating agent…",
    ready: (name: string) => `${name} is ready`,
    runsOn: (runtime: string) => `Runs on ${runtime}`,
    nextLabel: "What next",
    assignTitle: "Assign its first task",
    mentionTitle: "Mention it in an issue",
    squadTitle: "Add it to a squad",
    openAgent: "Open agent",
    backToList: "Back to agents",
    listTitle: "Agents",
    newAgent: "New agent",
    columns: { agent: "Agent", status: "Status", workload: "Workload", runtime: "Runtime" },
    online: "Online",
    idle: "Idle",
    working: "Working",
    otherAgent: "Writing helper",
    otherRuntime: "Codex (MacBook)",
    captions: {
      idle: "Creating an agent starts with where it runs.",
      runtime: "Step 1: pick a runtime. Only online harnesses can be chosen.",
      runtimeNext: "The footer shows your choice. Click Next.",
      configure: "Step 2: name and instructions are filled in. The model is left at the runtime default, and Access starts at Only me.",
      creating: "Creating the agent…",
      done: "Created. From here you can hand it work, or go back to the agent list.",
      list: "Ops helper is in the list: its runtime is online and nothing is running yet. Use Replay to watch again.",
    },
  },
  zh: {
    frameTitle: "新建智能体",
    shellTitle: "创建智能体",
    steps: ["选择运行时", "配置", "完成"],
    runtimeTitle: "这个智能体在哪里运行？",
    machinesGroup: "机器",
    thisMachine: "本机",
    harnessCount: "2 个 harness",
    harnesses: [
      { id: "claude", label: "Claude", version: "v2.1" },
      { id: "codex", label: "Codex", version: "v0.46" },
    ] as Harness[],
    selected: "已选：",
    selectedSummary: "Claude · 本机",
    cancel: "取消",
    next: "下一步",
    configureTitle: "配置智能体",
    askAi: "让 AI 帮我写",
    identity: "身份",
    nameLabel: "名称",
    name: "运营助手",
    descriptionLabel: "描述",
    description: "整理周报、筹备活动和日常运营杂事",
    behavior: "行为与能力",
    instructionsLabel: "指令",
    instructions: [
      "你负责团队的日常运营：整理周报、筹备活动、汇总客户反馈。",
      "收到任务先读完描述和评论；信息不够就在评论里问，不要自己猜。",
      "结果写在任务评论里，用短段落和清单。要花钱或对外发布的事，先找负责人确认。",
    ],
    skillsLabel: "Skills",
    skillsPlaceholder: "从工作区添加 skill",
    execution: "执行配置",
    executionHint: "未指定的选项沿用运行时默认值。",
    runtimeLabel: "运行时",
    runtime: "Claude (MacBook)",
    change: "更换",
    modelLabel: "模型",
    model: "默认（提供方）",
    accessLabel: "访问权限",
    access: [
      { title: "仅自己", desc: "只有你可以运行此智能体。" },
      { title: "整个工作区", desc: "工作区中的所有成员都可以运行。" },
      { title: "指定成员", desc: "只有选中的成员可以运行。" },
    ] as Access[],
    create: "创建智能体",
    creating: "正在创建…",
    ready: (name: string) => `${name} 已就绪`,
    runsOn: (runtime: string) => `运行在 ${runtime}`,
    nextLabel: "接下来",
    assignTitle: "指派第一个任务",
    mentionTitle: "在任务里 @ 它",
    squadTitle: "加入小队",
    openAgent: "打开智能体",
    backToList: "返回智能体列表",
    listTitle: "智能体",
    newAgent: "新建智能体",
    columns: { agent: "智能体", status: "状态", workload: "工作负载", runtime: "运行时" },
    online: "在线",
    idle: "空闲",
    working: "处理中",
    otherAgent: "文案助手",
    otherRuntime: "Codex (MacBook)",
    captions: {
      idle: "创建智能体的第一步是决定它在哪里运行。",
      runtime: "第 1 步：选择运行时。只有在线的 harness 可以选。",
      runtimeNext: "底部显示了你的选择。点击下一步。",
      configure: "第 2 步：名称和指令已经填好。模型留空，沿用运行时默认值；访问权限默认是仅自己。",
      creating: "正在创建智能体…",
      done: "创建完成。可以直接给它派活，也可以先回到智能体列表。",
      list: "运营助手 出现在列表里：运行时在线，还没有运行，所以是空闲。点右上角可以重新演示。",
    },
  },
  ja: {
    frameTitle: "新規エージェント",
    shellTitle: "エージェントを作成",
    steps: ["ランタイムを選択", "設定", "完了"],
    runtimeTitle: "このエージェントをどこで実行しますか？",
    machinesGroup: "マシン",
    thisMachine: "このマシン",
    harnessCount: "2 個のハーネス",
    harnesses: [
      { id: "claude", label: "Claude", version: "v2.1" },
      { id: "codex", label: "Codex", version: "v0.46" },
    ] as Harness[],
    selected: "選択中：",
    selectedSummary: "Claude · このマシン",
    cancel: "キャンセル",
    next: "次へ",
    configureTitle: "エージェントを設定",
    askAi: "AI に書いてもらう",
    identity: "基本情報",
    nameLabel: "名前",
    name: "運用アシスタント",
    descriptionLabel: "説明",
    description: "週報、イベント準備、日々の運用まわりの雑務",
    behavior: "動作と機能",
    instructionsLabel: "指示",
    instructions: [
      "チームの日常運用を担当します：週報の作成、イベントの準備、顧客フィードバックの取りまとめ。",
      "タスクを受けたら、まず説明とコメントを読みます。情報が足りなければ推測せず、コメントで質問します。",
      "成果はタスクのコメントに、短い段落と箇条書きで書きます。費用が発生する作業や社外への公開は、先に担当者に確認します。",
    ],
    skillsLabel: "スキル",
    skillsPlaceholder: "ワークスペースからスキルを追加",
    execution: "実行設定",
    executionHint: "未指定の項目はランタイムの既定値を使います。",
    runtimeLabel: "ランタイム",
    runtime: "Claude (MacBook)",
    change: "変更",
    modelLabel: "モデル",
    model: "デフォルト（プロバイダー）",
    accessLabel: "アクセス",
    access: [
      { title: "自分のみ", desc: "このエージェントを実行できるのは自分だけです。" },
      { title: "ワークスペース全体", desc: "すべてのワークスペースメンバーが実行できます。" },
      { title: "特定のメンバー", desc: "選択したメンバーだけが実行できます。" },
    ] as Access[],
    create: "エージェントを作成",
    creating: "エージェントを作成中…",
    ready: (name: string) => `${name} の準備ができました`,
    runsOn: (runtime: string) => `${runtime} で実行`,
    nextLabel: "次にすること",
    assignTitle: "最初のタスクを割り当てる",
    mentionTitle: "イシューでメンションする",
    squadTitle: "スクワッドに追加",
    openAgent: "エージェントを開く",
    backToList: "エージェント一覧に戻る",
    listTitle: "エージェント",
    newAgent: "新規エージェント",
    columns: { agent: "エージェント", status: "ステータス", workload: "作業量", runtime: "ランタイム" },
    online: "オンライン",
    idle: "アイドル",
    working: "作業中",
    otherAgent: "ライティングアシスタント",
    otherRuntime: "Codex (MacBook)",
    captions: {
      idle: "エージェントの作成は、どこで実行するかを決めるところから始まります。",
      runtime: "ステップ 1：ランタイムを選びます。選べるのはオンラインのハーネスだけです。",
      runtimeNext: "下部に選んだ内容が表示されます。「次へ」をクリックします。",
      configure: "ステップ 2：名前と指示は入力済みです。モデルは空欄のままなのでランタイムの既定値を使い、アクセスは最初「自分のみ」です。",
      creating: "エージェントを作成しています…",
      done: "作成できました。ここからすぐ仕事を任せることも、エージェント一覧に戻ることもできます。",
      list: "運用アシスタントが一覧に加わりました。ランタイムはオンラインで、まだ実行がないのでアイドルです。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    frameTitle: "새 에이전트",
    shellTitle: "에이전트 만들기",
    steps: ["런타임 선택", "구성", "완료"],
    runtimeTitle: "이 에이전트를 어디서 실행할까요?",
    machinesGroup: "머신",
    thisMachine: "이 머신",
    harnessCount: "하네스 2개",
    harnesses: [
      { id: "claude", label: "Claude", version: "v2.1" },
      { id: "codex", label: "Codex", version: "v0.46" },
    ] as Harness[],
    selected: "선택됨:",
    selectedSummary: "Claude · 이 머신",
    cancel: "취소",
    next: "다음",
    configureTitle: "에이전트 구성",
    askAi: "AI에게 작성 맡기기",
    identity: "기본 정보",
    nameLabel: "이름",
    name: "운영 도우미",
    descriptionLabel: "설명",
    description: "주간 보고, 행사 준비, 일상적인 운영 업무",
    behavior: "동작 및 기능",
    instructionsLabel: "지침",
    instructions: [
      "팀의 일상 운영을 맡습니다: 주간 보고 작성, 행사 준비, 고객 피드백 정리.",
      "태스크를 받으면 설명과 댓글부터 읽습니다. 정보가 부족하면 추측하지 말고 댓글로 물어봅니다.",
      "결과는 태스크 댓글에 짧은 문단과 목록으로 남깁니다. 비용이 들거나 외부에 공개하는 일은 먼저 담당자에게 확인합니다.",
    ],
    skillsLabel: "스킬",
    skillsPlaceholder: "워크스페이스에서 스킬 추가",
    execution: "실행 설정",
    executionHint: "지정하지 않은 옵션은 런타임 기본값을 사용합니다.",
    runtimeLabel: "런타임",
    runtime: "Claude (MacBook)",
    change: "변경",
    modelLabel: "모델",
    model: "기본값(제공자)",
    accessLabel: "접근 권한",
    access: [
      { title: "나만", desc: "나만 이 에이전트를 실행할 수 있습니다." },
      { title: "전체 워크스페이스", desc: "모든 워크스페이스 멤버가 실행할 수 있습니다." },
      { title: "특정 멤버", desc: "선택한 멤버만 실행할 수 있습니다." },
    ] as Access[],
    create: "에이전트 만들기",
    creating: "에이전트 만드는 중…",
    ready: (name: string) => `${name} 준비 완료`,
    runsOn: (runtime: string) => `${runtime}에서 실행`,
    nextLabel: "다음 단계",
    assignTitle: "첫 작업 할당하기",
    mentionTitle: "이슈에서 멘션하기",
    squadTitle: "스쿼드에 추가",
    openAgent: "에이전트 열기",
    backToList: "에이전트 목록으로",
    listTitle: "에이전트",
    newAgent: "새 에이전트",
    columns: { agent: "에이전트", status: "상태", workload: "작업량", runtime: "런타임" },
    online: "온라인",
    idle: "유휴",
    working: "작업 중",
    otherAgent: "글쓰기 도우미",
    otherRuntime: "Codex (MacBook)",
    captions: {
      idle: "에이전트를 만들 때는 먼저 어디서 실행할지 정합니다.",
      runtime: "1단계: 런타임을 고릅니다. 온라인인 하네스만 선택할 수 있습니다.",
      runtimeNext: "아래쪽에 선택한 항목이 표시됩니다. 다음을 클릭하세요.",
      configure: "2단계: 이름과 지침은 채워져 있습니다. 모델은 비워 두어 런타임 기본값을 쓰고, 접근 권한은 처음에 나만입니다.",
      creating: "에이전트를 만드는 중…",
      done: "만들었습니다. 여기서 바로 일을 맡기거나 에이전트 목록으로 돌아갈 수 있습니다.",
      list: "운영 도우미가 목록에 추가되었습니다. 런타임은 온라인이고 아직 실행이 없어 유휴 상태입니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    frameTitle: "Nouvel agent",
    shellTitle: "Créer un agent",
    steps: ["Choisir le runtime", "Configurer", "Terminé"],
    runtimeTitle: "Où cet agent doit-il s'exécuter ?",
    machinesGroup: "Machines",
    thisMachine: "Cette machine",
    harnessCount: "2 harness",
    harnesses: [
      { id: "claude", label: "Claude", version: "v2.1" },
      { id: "codex", label: "Codex", version: "v0.46" },
    ] as Harness[],
    selected: "Sélection :",
    selectedSummary: "Claude · Cette machine",
    cancel: "Annuler",
    next: "Suivant",
    configureTitle: "Configurer l'agent",
    askAi: "Laisser l'IA rédiger",
    identity: "Identité",
    nameLabel: "Nom",
    name: "Assistant ops",
    descriptionLabel: "Description",
    description: "Rapports hebdomadaires, préparation d'événements et tâches ops du quotidien",
    behavior: "Comportement et capacités",
    instructionsLabel: "Instructions",
    instructions: [
      "Tu gères les opérations courantes de l'équipe : rapports hebdomadaires, préparation d'événements, synthèse des retours clients.",
      "Lis d'abord la description et les commentaires. S'il manque une information, pose la question en commentaire au lieu de deviner.",
      "Livre le résultat dans les commentaires de la tâche, en paragraphes courts et en listes. Avant toute dépense ou publication externe, demande confirmation à la personne assignée.",
    ],
    skillsLabel: "Skills",
    skillsPlaceholder: "Ajouter des skills de l'espace de travail",
    execution: "Exécution",
    executionHint: "Les options non spécifiées utilisent les valeurs par défaut du runtime.",
    runtimeLabel: "Runtime",
    runtime: "Claude (MacBook)",
    change: "Changer",
    modelLabel: "Modèle",
    model: "Par défaut (fournisseur)",
    accessLabel: "Accès",
    access: [
      { title: "Moi uniquement", desc: "Vous seul pouvez exécuter cet agent." },
      { title: "Tout l'espace de travail", desc: "Tous les membres de l'espace de travail peuvent l'exécuter." },
      { title: "Personnes précises", desc: "Seuls les membres sélectionnés peuvent l'exécuter." },
    ] as Access[],
    create: "Créer l'agent",
    creating: "Création de l'agent…",
    ready: (name: string) => `${name} est prêt`,
    runsOn: (runtime: string) => `S'exécute sur ${runtime}`,
    nextLabel: "Et ensuite",
    assignTitle: "Lui confier une première tâche",
    mentionTitle: "Le mentionner dans un ticket",
    squadTitle: "L'ajouter à une escouade",
    openAgent: "Ouvrir l'agent",
    backToList: "Retour aux agents",
    listTitle: "Agents",
    newAgent: "Nouvel agent",
    columns: { agent: "Agent", status: "Statut", workload: "Charge", runtime: "Runtime" },
    online: "En ligne",
    idle: "Au repos",
    working: "Au travail",
    otherAgent: "Assistant rédaction",
    otherRuntime: "Codex (MacBook)",
    captions: {
      idle: "Créer un agent commence par choisir où il s'exécute.",
      runtime: "Étape 1 : choisissez un runtime. Seuls les harness en ligne sont sélectionnables.",
      runtimeNext: "Le bas de l'écran affiche votre choix. Cliquez sur Suivant.",
      configure: "Étape 2 : le nom et les instructions sont remplis. Le modèle reste vide, donc le runtime applique sa valeur par défaut, et l'accès part de Moi uniquement.",
      creating: "Création de l'agent…",
      done: "C'est créé. D'ici, vous pouvez lui confier du travail ou revenir à la liste des agents.",
      list: "Assistant ops apparaît dans la liste : son runtime est en ligne et rien ne tourne encore, il est donc au repos. Cliquez sur Rejouer pour revoir la démo.",
    },
  },
};

type Phase = "runtime" | "runtimeNext" | "configure" | "creating" | "done" | "list";

const STEP_INDEX: Record<Phase, number> = {
  runtime: 0,
  runtimeNext: 0,
  configure: 1,
  creating: 1,
  done: 2,
  list: 2,
};

// Follows the product's three-step route: choose a runtime, configure the
// draft, land on the done page. Form fields are fixed text, so the only
// enabled control on each screen is the one that moves the flow forward.
export function CreateAgentDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("runtime");

  const create = () => {
    setPhase("creating");
    after(900, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("runtime");
  };

  const caption = visible ? t.captions[phase] : t.captions.idle;
  const step = STEP_INDEX[phase];

  return (
    <DemoFrame frameRef={frameRef} title={t.frameTitle} caption={caption} onReplay={replay}>
      {phase === "list" ? (
        <AgentList t={t} />
      ) : (
        <div className="flex min-h-80 flex-col">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b px-4 py-2">
            <div className="min-w-0 flex-1">
              <div className="truncate text-label font-semibold">{t.shellTitle}</div>
              <div className="truncate text-caption text-muted-foreground">{t.steps[step]}</div>
            </div>
            <ol className="flex items-center gap-1.5">
              {t.steps.map((label, i) => (
                <li key={label} className="flex items-center gap-1.5 text-caption">
                  <span
                    className={cn(
                      "flex size-4 items-center justify-center rounded-full text-micro font-medium",
                      i < step && "bg-success/15 text-success",
                      i === step && "bg-foreground text-background",
                      i > step && "bg-muted text-muted-foreground",
                    )}
                  >
                    {i < step ? <Check aria-hidden className="size-2.5" /> : i + 1}
                  </span>
                  <span className={cn("max-sm:hidden", i === step ? "text-foreground" : "text-muted-foreground")}>
                    {label}
                  </span>
                </li>
              ))}
            </ol>
          </div>

          {(phase === "runtime" || phase === "runtimeNext") && (
            <RuntimeStep t={t} visible={visible} phase={phase} onPick={() => setPhase("runtimeNext")} onNext={() => setPhase("configure")} />
          )}
          {(phase === "configure" || phase === "creating") && (
            <ConfigureStep t={t} creating={phase === "creating"} onCreate={create} />
          )}
          {phase === "done" && <DoneStep t={t} onBack={() => setPhase("list")} />}
        </div>
      )}
    </DemoFrame>
  );
}

type Copy = (typeof copy)["en"];

function RuntimeStep({
  t,
  visible,
  phase,
  onPick,
  onNext,
}: {
  t: Copy;
  visible: boolean;
  phase: Phase;
  onPick: () => void;
  onNext: () => void;
}) {
  const picked = phase === "runtimeNext";
  return (
    <>
      <div className="flex-1 px-4 py-4">
        <div className="text-title-sm font-semibold">{t.runtimeTitle}</div>
        <div className="mt-3 text-micro text-muted-foreground">{t.machinesGroup}</div>
        <div className="mt-1 rounded-lg border bg-background">
          <div className="flex items-center gap-2 border-b px-3 py-2">
            <Monitor aria-hidden className="size-4 text-muted-foreground" />
            <span className="text-label font-medium">{t.thisMachine}</span>
            <span className="text-caption text-muted-foreground">· {t.harnessCount}</span>
          </div>
          <div className="grid gap-1.5 p-2 sm:grid-cols-2">
            {t.harnesses.map((harness, i) => {
              const selected = picked && i === 0;
              const enabled = visible && phase === "runtime" && i === 0;
              return (
                <CoachMark
                  key={harness.id}
                  active={enabled}
                  side="bottom"
                  align="start"
                  ringClassName="rounded-md"
                  className="flex w-full"
                >
                  <button
                    type="button"
                    aria-pressed={selected}
                    disabled={!enabled}
                    onClick={onPick}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left transition-colors",
                      selected ? "border-brand bg-brand/7 dark:bg-brand/12" : "hover:bg-muted/60 disabled:hover:bg-transparent",
                    )}
                  >
                    <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-success" />
                    <span className="min-w-0 flex-1 truncate text-body font-medium">{harness.label}</span>
                    <span className="text-caption text-muted-foreground">{harness.version}</span>
                    {selected && <Check aria-hidden className="size-3.5 text-brand" />}
                  </button>
                </CoachMark>
              );
            })}
          </div>
        </div>
      </div>
      <div className="mt-4 flex items-center gap-2 border-t px-4 py-2.5">
        <p className="min-w-0 flex-1 truncate text-label text-muted-foreground">
          {picked && (
            <>
              {t.selected} <span className="font-medium text-foreground">{t.selectedSummary}</span>
            </>
          )}
        </p>
        <Button type="button" variant="ghost" size="sm" disabled>
          {t.cancel}
        </Button>
        <CoachMark active={picked} side="top" align="end" ringClassName="rounded-lg">
          <Button type="button" size="sm" disabled={!picked} onClick={onNext}>
            {t.next}
            <ArrowRight aria-hidden className="size-3.5" />
          </Button>
        </CoachMark>
      </div>
    </>
  );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 px-3 py-2 sm:grid-cols-[6rem_minmax(0,1fr)] sm:gap-3">
      <div className="pt-1 text-label font-medium">{label}</div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function Section({ title, hint, children }: { title: string; hint?: string; children: ReactNode }) {
  return (
    <section>
      <div className="mb-1 flex flex-wrap items-baseline gap-x-2">
        <span className="text-label font-semibold">{title}</span>
        {hint && <span className="text-caption text-muted-foreground">{hint}</span>}
      </div>
      <div className="divide-y rounded-lg border bg-background">{children}</div>
    </section>
  );
}

const valueBox = "rounded-md border bg-card px-2 py-1 text-body";

function ConfigureStep({ t, creating, onCreate }: { t: Copy; creating: boolean; onCreate: () => void }) {
  return (
    <>
      <div className="flex flex-1 flex-col gap-4 px-4 py-4 motion-safe:animate-in motion-safe:fade-in">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="text-title-sm font-semibold">{t.configureTitle}</div>
          <Button type="button" variant="outline" size="sm" disabled>
            <Sparkles aria-hidden className="size-3.5" />
            {t.askAi}
          </Button>
        </div>

        <Section title={t.identity}>
          <Field label={t.nameLabel}>
            <div className={valueBox}>{t.name}</div>
          </Field>
          <Field label={t.descriptionLabel}>
            <div className={valueBox}>{t.description}</div>
          </Field>
        </Section>

        <Section title={t.behavior}>
          <Field label={t.instructionsLabel}>
            <div className={cn(valueBox, "flex flex-col gap-1 font-mono text-caption leading-5")}>
              {t.instructions.map((line) => (
                <span key={line}>{line}</span>
              ))}
            </div>
          </Field>
          <Field label={t.skillsLabel}>
            <div className={cn(valueBox, "border-dashed text-muted-foreground")}>{t.skillsPlaceholder}</div>
          </Field>
        </Section>

        <Section title={t.execution} hint={t.executionHint}>
          <Field label={t.runtimeLabel}>
            <div className={cn(valueBox, "flex items-center gap-2")}>
              <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-success" />
              <span className="min-w-0 flex-1 truncate">{t.runtime}</span>
              <span className="text-caption text-muted-foreground">{t.change}</span>
            </div>
          </Field>
          <Field label={t.modelLabel}>
            <div className={cn(valueBox, "text-muted-foreground")}>{t.model}</div>
          </Field>
        </Section>

        <Section title={t.accessLabel}>
          <div className="flex flex-col gap-0.5 p-1.5">
            {t.access.map((option, i) => (
              <div key={option.title} className={cn("flex items-start gap-2 rounded-md px-2 py-1.5", i === 0 && "bg-muted")}>
                <span
                  aria-hidden
                  className={cn(
                    "mt-1 flex size-3.5 shrink-0 items-center justify-center rounded-full border",
                    i === 0 ? "border-foreground" : "border-muted-foreground/50",
                  )}
                >
                  {i === 0 && <span className="size-1.5 rounded-full bg-foreground" />}
                </span>
                <span className="min-w-0 text-body">
                  <span className="font-medium">{option.title}</span>
                  <span className="text-caption text-muted-foreground"> · {option.desc}</span>
                </span>
              </div>
            ))}
          </div>
        </Section>
      </div>
      <div className="flex justify-end border-t px-4 py-2.5">
        <CoachMark active={!creating} side="left" ringClassName="rounded-lg">
          <Button type="button" size="sm" disabled={creating} onClick={onCreate}>
            {creating ? t.creating : t.create}
          </Button>
        </CoachMark>
      </div>
    </>
  );
}

function DoneStep({ t, onBack }: { t: Copy; onBack: () => void }) {
  const actions = [
    { icon: ListPlus, title: t.assignTitle, primary: true },
    { icon: AtSign, title: t.mentionTitle, primary: false },
    { icon: Users, title: t.squadTitle, primary: false },
  ];
  return (
    <div className="flex flex-1 flex-col items-center px-4 py-6 motion-safe:animate-in motion-safe:fade-in">
      <span className="relative inline-flex">
        <span className="flex size-12 items-center justify-center rounded-full bg-brand/12 text-brand">
          <Bot aria-hidden className="size-6" />
        </span>
        <CircleCheck aria-hidden className="absolute -right-1 -bottom-1 size-5 rounded-full bg-background text-success" />
      </span>
      <div className="mt-3 text-title-sm font-semibold">{t.ready(t.name)}</div>
      <div className="mt-1 text-body text-muted-foreground">{t.runsOn(t.runtime)}</div>

      <div className="mt-5 w-full max-w-sm">
        <div className="text-label font-medium text-muted-foreground">{t.nextLabel}</div>
        <div className="mt-1.5 flex flex-col gap-1.5">
          {actions.map(({ icon: Icon, title, primary }) => (
            <div
              key={title}
              className={cn(
                "flex items-center gap-2.5 rounded-lg border bg-card px-3 py-2",
                primary && "border-primary/30 bg-primary/[0.03]",
              )}
            >
              <span
                className={cn(
                  "flex size-7 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground",
                  primary && "bg-primary/10 text-primary",
                )}
              >
                <Icon aria-hidden className="size-3.5" />
              </span>
              <span className="min-w-0 flex-1 text-body font-medium">{title}</span>
              <ArrowRight aria-hidden className="size-3.5 text-muted-foreground" />
            </div>
          ))}
        </div>
      </div>

      <div className="mt-5 flex items-center gap-3 text-label">
        <span className="font-medium">{t.openAgent}</span>
        <span aria-hidden className="text-muted-foreground">·</span>
        <CoachMark active side="bottom" ringClassName="rounded-md">
          <button type="button" onClick={onBack} className="rounded-md px-1 text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">
            {t.backToList}
          </button>
        </CoachMark>
      </div>
      <div className="h-6" />
    </div>
  );
}

function AgentList({ t }: { t: Copy }) {
  const rows = [
    { name: t.name, workload: t.idle, runtime: t.runtime, fresh: true },
    { name: t.otherAgent, workload: t.working, runtime: t.otherRuntime, fresh: false },
  ];
  return (
    <div className="flex flex-col gap-3 p-4 motion-safe:animate-in motion-safe:fade-in">
      <div className="flex items-center justify-between gap-2">
        <div className="text-title-sm font-semibold">{t.listTitle}</div>
        <Button type="button" size="sm" disabled>
          {t.newAgent}
        </Button>
      </div>
      <div className="overflow-hidden rounded-lg border bg-background">
        <div className="grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)] gap-2 border-b px-3 py-1.5 text-caption text-muted-foreground sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.4fr)]">
          <span>{t.columns.agent}</span>
          <span>{t.columns.status}</span>
          <span>{t.columns.workload}</span>
          <span className="max-sm:hidden">{t.columns.runtime}</span>
        </div>
        {rows.map((row) => (
          <div
            key={row.name}
            className={cn(
              "grid grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)] items-center gap-2 px-3 py-2 text-body sm:grid-cols-[minmax(0,1.4fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1.4fr)]",
              row.fresh && "bg-brand/5 motion-safe:animate-in motion-safe:slide-in-from-top-1",
            )}
          >
            <span className="flex min-w-0 items-center gap-2">
              <span aria-hidden className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-brand/12 text-brand">
                <Bot className="size-3" />
              </span>
              <span className="truncate font-medium">{row.name}</span>
            </span>
            <span className="flex items-center gap-1.5">
              <span aria-hidden className="size-1.5 shrink-0 rounded-full bg-success" />
              {t.online}
            </span>
            <span className="text-muted-foreground">{row.workload}</span>
            <span className="truncate text-caption text-muted-foreground max-sm:hidden">{row.runtime}</span>
          </div>
        ))}
      </div>
    </div>
  );
}
