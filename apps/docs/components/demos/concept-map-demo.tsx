"use client";

import { useState } from "react";
import { CornerDownRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { LocaleLink } from "@/components/locale-link";
import { CoachMark, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type NodeId =
  | "workspace"
  | "issue"
  | "project"
  | "agent"
  | "skill"
  | "runtime"
  | "run"
  | "squad"
  | "chat"
  | "inbox"
  | "autopilot";

type MapNode = Exclude<NodeId, "workspace">;

type EdgeId =
  | "project_issue"
  | "issue_agent"
  | "chat_agent"
  | "autopilot_agent"
  | "agent_run"
  | "run_runtime"
  | "run_issue"
  | "run_chat"
  | "skill_agent"
  | "squad_agent"
  | "squad_issue"
  | "issue_inbox";

type NodeCopy = { name: string; desc: string; page: string };

// Positions on the wide map, in percent of its width and height.
const POSITION: Record<MapNode, [number, number]> = {
  project: [12, 24],
  issue: [12, 48],
  chat: [12, 71],
  autopilot: [12, 90],
  inbox: [31, 24],
  squad: [50, 24],
  skill: [70, 24],
  agent: [48, 52],
  run: [72, 52],
  runtime: [90, 52],
};

// Write-back edges curve below the agent so they do not run through it.
const EDGES: { id: EdgeId; from: MapNode; to: MapNode; curve?: [number, number]; dashed?: boolean }[] = [
  { id: "project_issue", from: "project", to: "issue" },
  { id: "issue_agent", from: "issue", to: "agent" },
  { id: "chat_agent", from: "chat", to: "agent" },
  { id: "autopilot_agent", from: "autopilot", to: "agent" },
  { id: "agent_run", from: "agent", to: "run" },
  { id: "run_runtime", from: "run", to: "runtime" },
  { id: "run_issue", from: "run", to: "issue", curve: [44, 92], dashed: true },
  { id: "run_chat", from: "run", to: "chat", curve: [46, 102], dashed: true },
  { id: "skill_agent", from: "skill", to: "agent" },
  { id: "squad_agent", from: "squad", to: "agent" },
  { id: "squad_issue", from: "squad", to: "issue" },
  { id: "issue_inbox", from: "issue", to: "inbox" },
];

const MAP_NODES = Object.keys(POSITION) as MapNode[];

// The main line the coach mark walks the reader along, as on "How Multica works".
const GUIDE: MapNode[] = ["issue", "agent", "run", "runtime"];

const GROUPS: { key: "basic" | "execution" | "collaboration"; nodes: NodeId[] }[] = [
  { key: "basic", nodes: ["workspace", "issue", "project"] },
  { key: "execution", nodes: ["agent", "skill", "runtime", "run"] },
  { key: "collaboration", nodes: ["squad", "chat", "inbox", "autopilot"] },
];

const copy = {
  en: {
    title: "Core objects and how they relate",
    groups: { basic: "Basic objects", execution: "Agents and execution", collaboration: "Collaboration and automation" },
    nodes: {
      workspace: {
        name: "Workspace",
        desc: "The self-contained scope where a team works together; all work and configuration happen inside it. Humans and agents collaborate in the same workspace.",
        page: "Workspaces",
      },
      issue: {
        name: "Issue",
        desc: "A piece of work to be done, plus the description, discussion, status, and history that accumulate around it. The assignee can be a member, an agent, or a squad.",
        page: "Issues",
      },
      project: {
        name: "Project",
        desc: "Organizes related issues under one goal, tracks overall progress, and can bind resources such as repositories and directories for use during execution.",
        page: "Projects",
      },
      agent: {
        name: "Agent",
        desc: "An AI collaborator in the workspace — a reusable configuration: name, instructions, model, skills, Access, and runtime. It executes only when triggered.",
        page: "Agents",
      },
      skill: {
        name: "Skill",
        desc: "A reusable capability package. It describes how a kind of work is done, and can be attached to multiple agents.",
        page: "Skills",
      },
      runtime: {
        name: "Runtime",
        desc: "Where execution actually happens: a computer connected to Multica and the AI coding tools on it.",
        page: "Daemon and runtimes",
      },
      run: {
        name: "Run",
        desc: "One concrete execution record for an agent. One issue can produce multiple runs over time; a finished run does not mean the issue is done.",
        page: "Runs",
      },
      squad: {
        name: "Squad",
        desc: "A group of agents and members led by an agent leader. Assign an issue to a squad and the leader coordinates the work.",
        page: "Squads",
      },
      chat: {
        name: "Chat",
        desc: "A way to talk that is not attached to an issue — good for questions and quick experiments. Each message triggers one run.",
        page: "Chat with agents",
      },
      inbox: {
        name: "Inbox",
        desc: "A member's notification center: activity on subscribed issues, mentions, and assignments all land here. Agents do not use the inbox.",
        page: "Inbox and subscriptions",
      },
      autopilot: {
        name: "Autopilot",
        desc: "Triggers agent runs automatically on a schedule or from external events; you can also run one manually.",
        page: "Autopilots",
      },
    } as Record<NodeId, NodeCopy>,
    edges: {
      project_issue: "Related issues are organized with projects.",
      issue_agent: "Assigning or @-mentioning on an issue triggers the agent.",
      chat_agent: "A chat message triggers the agent.",
      autopilot_agent: "An autopilot triggers the agent on a schedule or event.",
      agent_run: "Every trigger produces a run.",
      run_runtime: "The run completes on a runtime.",
      run_issue: "Results are written back to the issue that triggered it.",
      run_chat: "Results are written back to the chat that triggered it.",
      skill_agent: "Skills attach to agents, so what works is reused across them.",
      squad_agent: "A squad lets multiple agents work together.",
      squad_issue: "An issue can be assigned to a squad; its leader coordinates.",
      issue_inbox: "Issue activity, mentions, and assignments reach members' inboxes.",
    } as Record<EdgeId, string>,
    workspaceRelation: "The workspace is the container for everything; humans and agents collaborate inside it.",
    learnMore: "See ",
    list: (names: string[]) => names.join(", "),
    captions: {
      idle: "The core objects in a workspace and the lines between them.",
      start: "Click Issue to see what it connects to.",
      connected: (name: string, related: string) => `${name} connects directly to ${related}.`,
      contains: "The workspace contains every object.",
      // Leading spaces: these follow another sentence in the caption.
      next: (name: string) => ` Next, click ${name}.`,
      done: "That is the main line: an issue triggers an agent, which produces a run; the run completes on a runtime and the results go back to the issue. Click any other object to explore.",
      explore: " Click any other object to explore.",
    },
  },
  zh: {
    title: "核心对象和它们的关系",
    groups: { basic: "基础对象", execution: "智能体与执行", collaboration: "协作与自动化" },
    nodes: {
      workspace: {
        name: "工作区",
        desc: "团队共同工作的独立范围，所有工作和配置都发生在其中；人和智能体在同一个工作区里协作。",
        page: "工作区",
      },
      issue: {
        name: "任务",
        desc: "一项要完成的工作，以及围绕它积累的描述、讨论、状态和历史。负责人可以是成员、智能体或小队。",
        page: "任务",
      },
      project: {
        name: "项目",
        desc: "把相关的任务组织在一个目标下，跟踪整体进度，并可以绑定仓库、目录等资源供执行时使用。",
        page: "项目",
      },
      agent: {
        name: "智能体",
        desc: "加入工作区的 AI 协作者，是一份可复用的配置：名称、指令、模型、skill、Access 和运行时。只在被触发时执行。",
        page: "智能体",
      },
      skill: {
        name: "Skill",
        desc: "可复用的能力包，说明\"这类工作怎么做\"，可以挂载给多个智能体共用。",
        page: "Skills",
      },
      runtime: {
        name: "运行时",
        desc: "执行实际发生的地方：连接到 Multica 的电脑和其中的 AI 编程工具。",
        page: "守护进程与运行时",
      },
      run: {
        name: "运行",
        desc: "智能体的一次具体执行记录。一个任务可以先后产生多次运行；运行结束不等于任务完成。",
        page: "运行",
      },
      squad: {
        name: "小队",
        desc: "一组智能体和成员，由一个智能体 leader 带领。把任务分配给小队，由 leader 协调完成。",
        page: "小队",
      },
      chat: {
        name: "对话",
        desc: "不依附于任务的交流方式，适合提问和快速尝试。每条消息触发一轮执行。",
        page: "与智能体对话",
      },
      inbox: {
        name: "收件箱",
        desc: "成员的通知中心：订阅的任务动态、提及和分配都汇聚在这里。智能体不使用收件箱。",
        page: "收件箱与订阅",
      },
      autopilot: {
        name: "自动化",
        desc: "按时间表或外部事件自动触发智能体执行，也可以手动运行一次。",
        page: "自动化",
      },
    } as Record<NodeId, NodeCopy>,
    edges: {
      project_issue: "相关的任务用项目组织。",
      issue_agent: "在任务上分配或 @提及，触发智能体。",
      chat_agent: "对话里的每条消息触发智能体。",
      autopilot_agent: "自动化按时间表或外部事件触发智能体。",
      agent_run: "每次触发产生一条运行。",
      run_runtime: "运行在某个运行时上完成。",
      run_issue: "结果写回触发它的任务。",
      run_chat: "结果写回触发它的对话。",
      skill_agent: "Skill 挂载给智能体，让成功经验跨智能体复用。",
      squad_agent: "小队让多个智能体协同。",
      squad_issue: "任务可以分配给小队，由 leader 协调。",
      issue_inbox: "任务动态、提及和分配通知进成员的收件箱。",
    } as Record<EdgeId, string>,
    workspaceRelation: "工作区是一切的容器，人和智能体都在其中协作。",
    learnMore: "详见",
    list: (names: string[]) => names.join("、"),
    captions: {
      idle: "工作区里的核心对象，以及它们之间的连线。",
      start: "点击任务，看它和哪些对象相连。",
      // Space only where an English name meets Chinese, e.g. "Skill 和智能体".
      connected: (name: string, related: string) =>
        `${name}${/[A-Za-z]$/.test(name) ? " " : ""}和${/^[A-Za-z]/.test(related) ? " " : ""}${related}直接相连。`,
      contains: "工作区包含所有对象。",
      next: (name: string) => `接着点${name}。`,
      done: "主线走完了：任务触发智能体，产生一条运行；运行在运行时上完成，结果写回任务。其他对象也可以点开看。",
      explore: "其他对象也可以点开看。",
    },
  },
  ja: {
    title: "コアオブジェクトとその関係",
    groups: { basic: "基本オブジェクト", execution: "エージェントと実行", collaboration: "協働と自動化" },
    nodes: {
      workspace: {
        name: "ワークスペース",
        desc: "チームが一緒に働く独立した範囲で、すべての仕事と設定はその中で行われます。人間とエージェントは同じワークスペースで協働します。",
        page: "ワークスペース",
      },
      issue: {
        name: "タスク",
        desc: "完了すべき 1 件の仕事と、その周りに蓄積される説明、議論、ステータス、履歴。担当者はメンバー、エージェント、スクワッドのいずれかです。",
        page: "タスク",
      },
      project: {
        name: "プロジェクト",
        desc: "関連するタスクを 1 つの目標の下に整理し、全体の進捗を追跡します。リポジトリやディレクトリなどのリソースを紐づけて、実行時に使わせることもできます。",
        page: "プロジェクト",
      },
      agent: {
        name: "エージェント",
        desc: "ワークスペースに参加する AI の協働者で、名前、指示、モデル、スキル、アクセス、ランタイムからなる再利用可能な設定です。トリガーされたときだけ実行します。",
        page: "エージェント",
      },
      skill: {
        name: "スキル",
        desc: "再利用可能な能力パッケージ。ある種類の仕事をどう進めるかを記述し、複数のエージェントに取り付けて共用できます。",
        page: "スキル",
      },
      runtime: {
        name: "ランタイム",
        desc: "実行が実際に行われる場所。Multica に接続されたコンピュータと、その上の AI コーディングツールです。",
        page: "デーモンとランタイム",
      },
      run: {
        name: "実行",
        desc: "エージェントの 1 回の具体的な実行記録。1 つのタスクから複数の実行が順に生まれることがあり、実行の終了はタスクの完了を意味しません。",
        page: "実行",
      },
      squad: {
        name: "スクワッド",
        desc: "1 体のリーダーエージェントが率いる、エージェントとメンバーのグループ。タスクをスクワッドに割り当てると、リーダーが調整して進めます。",
        page: "スクワッド",
      },
      chat: {
        name: "チャット",
        desc: "タスクに紐づかないやり取りの方法で、質問や素早い試行に向いています。メッセージ 1 件ごとに 1 回の実行がトリガーされます。",
        page: "エージェントとのチャット",
      },
      inbox: {
        name: "インボックス",
        desc: "メンバーの通知センター。購読しているタスクの動き、メンション、割り当てがここに集まります。エージェントはインボックスを使いません。",
        page: "インボックスと購読",
      },
      autopilot: {
        name: "オートパイロット",
        desc: "スケジュールまたは外部イベントに応じて、エージェントの実行を自動的にトリガーします。手動で 1 回だけ実行することもできます。",
        page: "オートパイロット",
      },
    } as Record<NodeId, NodeCopy>,
    edges: {
      project_issue: "関連するタスクはプロジェクトで整理します。",
      issue_agent: "タスクでの割り当てや @メンションがエージェントをトリガーします。",
      chat_agent: "チャットのメッセージがエージェントをトリガーします。",
      autopilot_agent: "オートパイロットがスケジュールや外部イベントでエージェントをトリガーします。",
      agent_run: "トリガーのたびに実行が 1 件作られます。",
      run_runtime: "実行はいずれかのランタイム上で完了します。",
      run_issue: "結果はトリガー元のタスクに書き戻されます。",
      run_chat: "結果はトリガー元のチャットに書き戻されます。",
      skill_agent: "スキルはエージェントに取り付けて、成功したやり方をエージェント間で再利用させます。",
      squad_agent: "スクワッドは複数のエージェントを協働させます。",
      squad_issue: "タスクはスクワッドにも割り当てられ、リーダーが調整します。",
      issue_inbox: "タスクの動き、メンション、割り当てはメンバーのインボックスに届きます。",
    } as Record<EdgeId, string>,
    workspaceRelation: "ワークスペースはすべての入れ物で、人間とエージェントがその中で協働します。",
    learnMore: "詳しくは",
    list: (names: string[]) => names.join("、"),
    captions: {
      idle: "ワークスペースのコアオブジェクトと、それらをつなぐ線です。",
      start: "タスクをクリックすると、つながっているオブジェクトが分かります。",
      connected: (name: string, related: string) => `${name}は${related}と直接つながっています。`,
      contains: "ワークスペースはすべてのオブジェクトを含みます。",
      next: (name: string) => `次に${name}をクリックしてください。`,
      done: "これがメインの流れです。タスクがエージェントをトリガーして実行が作られ、実行はランタイム上で完了し、結果がタスクに書き戻されます。ほかのオブジェクトもクリックして確認できます。",
      explore: "ほかのオブジェクトもクリックして確認できます。",
    },
  },
  ko: {
    title: "핵심 객체와 서로의 관계",
    groups: { basic: "기본 객체", execution: "에이전트와 실행", collaboration: "협업과 자동화" },
    nodes: {
      workspace: {
        name: "워크스페이스",
        desc: "팀이 함께 일하는 독립된 공간입니다. 모든 작업과 설정이 워크스페이스 안에서 이루어지며, 사람과 에이전트가 같은 워크스페이스에서 협업합니다.",
        page: "워크스페이스",
      },
      issue: {
        name: "태스크",
        desc: "해야 할 일과 그에 관한 설명, 논의, 상태, 이력을 한곳에 모은 객체입니다. 멤버나 에이전트, 스쿼드를 담당자로 지정할 수 있습니다.",
        page: "태스크",
      },
      project: {
        name: "프로젝트",
        desc: "관련 태스크를 하나의 목표 아래 정리하고 전체 진행 상황을 추적합니다. 실행할 때 사용할 저장소나 디렉터리 같은 리소스도 연결할 수 있습니다.",
        page: "프로젝트",
      },
      agent: {
        name: "에이전트",
        desc: "워크스페이스에서 함께 일하는 AI 협업자입니다. 이름, 지침, 모델, 스킬, 접근 권한, 런타임으로 이루어진 재사용 가능한 설정이며, 트리거될 때만 동작합니다.",
        page: "에이전트",
      },
      skill: {
        name: "스킬",
        desc: "재사용할 수 있는 기능 패키지입니다. 특정 종류의 작업을 어떻게 수행하는지 설명하며, 여러 에이전트에 연결해 함께 사용할 수 있습니다.",
        page: "스킬",
      },
      runtime: {
        name: "런타임",
        desc: "실제 실행이 일어나는 곳으로, Multica에 연결된 컴퓨터와 그 컴퓨터의 AI 코딩 도구를 뜻합니다.",
        page: "데몬과 런타임",
      },
      run: {
        name: "실행",
        desc: "에이전트가 한 번 실행된 기록입니다. 하나의 태스크에서 여러 실행이 만들어질 수 있으며, 실행이 끝났다고 해서 태스크까지 완료된 것은 아닙니다.",
        page: "실행",
      },
      squad: {
        name: "스쿼드",
        desc: "리더 에이전트 한 명이 이끄는 에이전트와 멤버의 그룹입니다. 태스크를 스쿼드에 할당하면 리더가 작업을 조율합니다.",
        page: "스쿼드",
      },
      chat: {
        name: "채팅",
        desc: "태스크에 연결하지 않고 대화하는 방식으로, 질문이나 간단한 시도에 적합합니다. 메시지를 보낼 때마다 한 번의 실행이 트리거됩니다.",
        page: "에이전트와 대화",
      },
      inbox: {
        name: "인박스",
        desc: "멤버를 위한 알림 센터입니다. 구독한 태스크의 활동, 멘션, 할당 알림이 이곳에 모입니다. 에이전트는 인박스를 사용하지 않습니다.",
        page: "인박스와 구독",
      },
      autopilot: {
        name: "오토파일럿",
        desc: "일정이나 외부 이벤트에 따라 에이전트 실행을 자동으로 트리거하며, 필요하면 한 번만 수동으로 실행할 수도 있습니다.",
        page: "오토파일럿",
      },
    } as Record<NodeId, NodeCopy>,
    edges: {
      project_issue: "관련 태스크는 프로젝트로 정리합니다.",
      issue_agent: "태스크에서 할당하거나 @멘션하면 에이전트가 트리거됩니다.",
      chat_agent: "채팅 메시지가 에이전트를 트리거합니다.",
      autopilot_agent: "오토파일럿이 일정이나 외부 이벤트에 따라 에이전트를 트리거합니다.",
      agent_run: "트리거할 때마다 실행이 하나 만들어집니다.",
      run_runtime: "실행은 런타임에서 완료됩니다.",
      run_issue: "결과는 실행을 트리거한 태스크에 다시 기록됩니다.",
      run_chat: "결과는 실행을 트리거한 채팅에 다시 기록됩니다.",
      skill_agent: "스킬은 에이전트에 연결되어 검증된 작업 방식을 여러 에이전트에서 재사용하게 합니다.",
      squad_agent: "스쿼드는 여러 에이전트가 함께 일하게 합니다.",
      squad_issue: "태스크를 스쿼드에 할당하면 리더가 조율합니다.",
      issue_inbox: "태스크 활동, 멘션, 할당 알림은 멤버의 인박스로 갑니다.",
    } as Record<EdgeId, string>,
    workspaceRelation: "워크스페이스는 모든 객체를 담는 공간이며, 사람과 에이전트가 그 안에서 협업합니다.",
    learnMore: "자세히: ",
    list: (names: string[]) => names.join(", "),
    captions: {
      idle: "워크스페이스의 핵심 객체와 그 사이를 잇는 선입니다.",
      start: "태스크를 클릭해 어떤 객체와 연결되는지 확인하세요.",
      // Phrased so no particle has to agree with the name's final consonant.
      connected: (name: string, related: string) => `${name}에 직접 연결된 객체: ${related}.`,
      contains: "워크스페이스가 모든 객체를 담고 있습니다.",
      next: (name: string) => ` 이어서 ${name} 항목을 클릭하세요.`,
      done: "이것이 주요 흐름입니다. 태스크가 에이전트를 트리거해 실행이 만들어지고, 실행은 런타임에서 완료되어 결과가 태스크에 다시 기록됩니다. 다른 객체도 클릭해 살펴보세요.",
      explore: " 다른 객체도 클릭해 살펴보세요.",
    },
  },
  fr: {
    title: "Les objets clés et leurs relations",
    groups: { basic: "Objets de base", execution: "Agents et exécution", collaboration: "Collaboration et automatisation" },
    nodes: {
      workspace: {
        name: "Espace de travail",
        desc: "Le périmètre autonome dans lequel une équipe travaille ensemble ; tout le travail et toute la configuration s'y trouvent. Humains et agents collaborent dans le même espace de travail.",
        page: "Espaces de travail",
      },
      issue: {
        name: "Tâche",
        desc: "Un travail à réaliser, accompagné de la description, de la discussion, du statut et de l'historique qui s'accumulent autour de lui. L'assigné peut être un membre, un agent ou un squad.",
        page: "Tâches",
      },
      project: {
        name: "Projet",
        desc: "Regroupe des tâches liées autour d'un même objectif, suit l'avancement global et peut rattacher des ressources, comme des dépôts et des répertoires, utilisées pendant l'exécution.",
        page: "Projets",
      },
      agent: {
        name: "Agent",
        desc: "Un collaborateur IA de l'espace de travail — une configuration réutilisable : nom, instructions, modèle, skills, Accès et runtime. Il ne s'exécute que lorsqu'il est déclenché.",
        page: "Agents",
      },
      skill: {
        name: "Skill",
        desc: "Un paquet de capacités réutilisable. Il décrit comment réaliser un type de travail et peut être rattaché à plusieurs agents.",
        page: "Skills",
      },
      runtime: {
        name: "Runtime",
        desc: "L'endroit où l'exécution a réellement lieu : un ordinateur connecté à Multica et les outils de codage IA qui y sont installés.",
        page: "Daemon et runtimes",
      },
      run: {
        name: "Exécution",
        desc: "L'enregistrement concret d'une exécution d'un agent. Une tâche peut produire plusieurs exécutions au fil du temps ; une exécution terminée ne signifie pas que la tâche est terminée.",
        page: "Exécutions",
      },
      squad: {
        name: "Squad",
        desc: "Un groupe d'agents et de membres dirigé par un agent chef. Assignez une tâche à un squad et le chef coordonne le travail.",
        page: "Squads",
      },
      chat: {
        name: "Discussion",
        desc: "Un moyen d'échanger qui n'est rattaché à aucune tâche, idéal pour les questions et les essais rapides. Chaque message déclenche une exécution.",
        page: "Discuter avec des agents",
      },
      inbox: {
        name: "Boîte de réception",
        desc: "Le centre de notifications d'un membre : l'activité sur les tâches suivies, les mentions et les assignations y arrivent. Les agents n'utilisent pas la Boîte de réception.",
        page: "Boîte de réception et abonnements",
      },
      autopilot: {
        name: "Automatisation",
        desc: "Déclenche automatiquement des exécutions d'agents selon une planification ou à partir d'événements externes ; vous pouvez aussi en lancer une manuellement.",
        page: "Automatisations",
      },
    } as Record<NodeId, NodeCopy>,
    edges: {
      project_issue: "Les tâches liées sont organisées en projets.",
      issue_agent: "Assigner une tâche ou @mentionner l'agent dans une tâche le déclenche.",
      chat_agent: "Un message de discussion déclenche l'agent.",
      autopilot_agent: "Une automatisation déclenche l'agent selon une planification ou un événement.",
      agent_run: "Chaque déclenchement produit une exécution.",
      run_runtime: "L'exécution s'effectue sur un runtime.",
      run_issue: "Les résultats sont réécrits dans la tâche qui l'a déclenchée.",
      run_chat: "Les résultats sont réécrits dans la discussion qui l'a déclenchée.",
      skill_agent: "Les skills se rattachent aux agents pour réutiliser ce qui fonctionne de l'un à l'autre.",
      squad_agent: "Un squad fait travailler plusieurs agents ensemble.",
      squad_issue: "Une tâche peut être assignée à un squad ; son chef coordonne.",
      issue_inbox: "L'activité des tâches, les mentions et les assignations arrivent dans la Boîte de réception des membres.",
    } as Record<EdgeId, string>,
    workspaceRelation: "L'espace de travail contient tout ; humains et agents y collaborent.",
    learnMore: "Voir ",
    list: (names: string[]) =>
      names.length < 2 ? names.join("") : `${names.slice(0, -1).join(", ")} et ${names[names.length - 1]}`,
    captions: {
      idle: "Les objets clés d'un espace de travail et les liens entre eux.",
      start: "Cliquez sur Tâche pour voir à quoi elle est reliée.",
      connected: (name: string, related: string) => `${name} se connecte directement à : ${related}.`,
      contains: "L'espace de travail contient tous les objets.",
      next: (name: string) => ` Cliquez ensuite sur ${name}.`,
      done: "Voilà le fil principal : une tâche déclenche un agent, qui produit une exécution ; l'exécution se termine sur un runtime et les résultats reviennent dans la tâche. Cliquez sur n'importe quel autre objet pour explorer.",
      explore: " Cliquez sur n'importe quel autre objet pour explorer.",
    },
  },
};

const PAGE_HREF: Record<NodeId, string> = {
  workspace: "/workspaces",
  issue: "/issues",
  project: "/projects",
  agent: "/agents",
  skill: "/skills",
  runtime: "/daemon-runtimes",
  run: "/tasks",
  squad: "/squads",
  chat: "/chat",
  inbox: "/inbox",
  autopilot: "/autopilots",
};

function edgesOf(id: NodeId) {
  return EDGES.filter((e) => e.from === id || e.to === id);
}

function neighborsOf(id: NodeId): Set<NodeId> {
  if (id === "workspace") return new Set(MAP_NODES);
  return new Set(edgesOf(id).map((e) => (e.from === id ? e.to : e.from)));
}

function edgePath({ from, to, curve }: (typeof EDGES)[number]) {
  const [x1, y1] = POSITION[from];
  const [x2, y2] = POSITION[to];
  return curve ? `M ${x1} ${y1} Q ${curve[0]} ${curve[1]} ${x2} ${y2}` : `M ${x1} ${y1} L ${x2} ${y2}`;
}

export function ConceptMapDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [selected, setSelected] = useState<NodeId | null>(null);
  const [guide, setGuide] = useState(0);

  const guideTarget = visible && guide < GUIDE.length ? GUIDE[guide] : null;
  const neighbors = selected ? neighborsOf(selected) : null;

  const select = (id: NodeId) => {
    setSelected(id);
    if (id === guideTarget) setGuide(guide + 1);
  };

  const replay = () => {
    setSelected(null);
    setGuide(0);
  };

  const caption = (() => {
    if (!visible) return t.captions.idle;
    if (!selected) return t.captions.start;
    const name = t.nodes[selected].name;
    const head =
      selected === "workspace"
        ? t.captions.contains
        : t.captions.connected(name, t.list([...neighborsOf(selected)].map((n) => t.nodes[n].name)));
    if (guideTarget) return `${head}${t.captions.next(t.nodes[guideTarget].name)}`;
    if (selected === GUIDE[GUIDE.length - 1] && guide === GUIDE.length) return t.captions.done;
    return `${head}${t.captions.explore}`;
  })();

  const nodeState = (id: NodeId) => {
    if (!selected) return "idle" as const;
    if (id === selected) return "selected" as const;
    return neighbors?.has(id) ? ("related" as const) : ("dimmed" as const);
  };

  const nodeButton = (id: NodeId, className?: string) => {
    const state = nodeState(id);
    return (
      <button
        type="button"
        aria-pressed={id === selected}
        disabled={!visible}
        onClick={() => select(id)}
        className={cn(
          "rounded-md border px-2.5 py-1 text-label font-medium whitespace-nowrap shadow-xs transition-[color,background-color,border-color,opacity] outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
          // Every state keeps an opaque fill: edges run to node centers and
          // must stay hidden behind the label.
          state === "selected" && "border-brand bg-brand text-brand-foreground",
          state === "related" && "border-brand/60 bg-background bg-linear-to-b from-brand/10 to-brand/10 text-foreground",
          state === "idle" && "bg-background hover:bg-muted",
          state === "dimmed" && "border-border/60 bg-background text-muted-foreground/60 shadow-none hover:bg-muted",
          className,
        )}
      >
        {t.nodes[id].name}
      </button>
    );
  };

  const workspaceState = nodeState("workspace");

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="flex flex-col gap-3 p-4">
        {/* Wide screens: the map, with the workspace as the frame around every object. */}
        <div
          className={cn(
            "relative hidden aspect-[16/9] rounded-lg border border-dashed transition-colors sm:block",
            workspaceState === "selected" && "border-brand bg-brand/5",
          )}
        >
          <div className="absolute top-2 left-2 z-10">{nodeButton("workspace", "shadow-none")}</div>
          <svg
            aria-hidden
            className="absolute inset-0 size-full"
            viewBox="0 0 100 100"
            preserveAspectRatio="none"
          >
            {EDGES.map((edge) => {
              const lit = selected === "workspace" || edge.from === selected || edge.to === selected;
              return (
                <path
                  key={edge.id}
                  d={edgePath(edge)}
                  vectorEffect="non-scaling-stroke"
                  strokeDasharray={edge.dashed ? "4 3" : undefined}
                  strokeWidth={lit ? 2 : 1.25}
                  className={cn(
                    "fill-none transition-[stroke,opacity]",
                    lit ? "stroke-brand" : "stroke-border",
                    selected && !lit && "opacity-50",
                  )}
                />
              );
            })}
          </svg>
          {MAP_NODES.map((id) => {
            const [x, y] = POSITION[id];
            return (
              <div
                key={id}
                className="absolute z-10 -translate-x-1/2 -translate-y-1/2"
                style={{ left: `${x}%`, top: `${y}%` }}
              >
                <CoachMark
                  active={guideTarget === id}
                  side="top"
                  align={x > 85 ? "end" : x < 15 ? "start" : "center"}
                  ringClassName="rounded-lg"
                >
                  {nodeButton(id)}
                </CoachMark>
              </div>
            );
          })}
        </div>

        {/* Phones: the same objects grouped as on the page; related ones light up. */}
        <div className="flex flex-col gap-3 sm:hidden">
          {GROUPS.map((group) => (
            <div key={group.key}>
              <div className="mb-1.5 text-micro text-muted-foreground">{t.groups[group.key]}</div>
              <div className="flex flex-wrap gap-2">
                {group.nodes.map((id) => (
                  <CoachMark quiet key={id} active={guideTarget === id} ringClassName="rounded-lg">
                    {nodeButton(id)}
                  </CoachMark>
                ))}
              </div>
            </div>
          ))}
        </div>

        {selected && (
          <div
            key={selected}
            className="rounded-lg border bg-background px-3 py-2.5 motion-safe:animate-in motion-safe:fade-in"
          >
            <div className="text-body font-medium">{t.nodes[selected].name}</div>
            <p className="mt-0.5 text-body text-muted-foreground">{t.nodes[selected].desc}</p>
            <ul className="mt-2 flex flex-col gap-1">
              {selected === "workspace" ? (
                <li className="flex items-start gap-1.5 text-caption">
                  <CornerDownRight aria-hidden className="mt-px size-3.5 shrink-0 text-brand" />
                  {t.workspaceRelation}
                </li>
              ) : (
                edgesOf(selected).map((edge) => (
                  <li key={edge.id} className="flex items-start gap-1.5 text-caption">
                    <CornerDownRight aria-hidden className="mt-px size-3.5 shrink-0 text-brand" />
                    {t.edges[edge.id]}
                  </li>
                ))
              )}
            </ul>
            <div className="mt-2 text-caption text-muted-foreground">
              {t.learnMore}
              <LocaleLink href={PAGE_HREF[selected]} className="font-medium text-brand hover:underline">
                {t.nodes[selected].page}
              </LocaleLink>
            </div>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
