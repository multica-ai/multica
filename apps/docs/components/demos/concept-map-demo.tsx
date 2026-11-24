"use client";

import { useState } from "react";
import { ArrowRight } from "@multica/ui/icons";
import { LocaleLink } from "@/components/locale-link";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type NodeId =
  | "project"
  | "issue"
  | "triggers"
  | "skills"
  | "squad"
  | "agent"
  | "run"
  | "runtime"
  | "inbox";

type NodeCopy = {
  code: string;
  name: string;
  desc: string;
  page: string;
  href: string;
};

const EDGES: { from: NodeId; to: NodeId; main?: boolean }[] = [
  { from: "project", to: "issue" },
  { from: "triggers", to: "agent" },
  { from: "skills", to: "agent" },
  { from: "squad", to: "agent" },
  { from: "issue", to: "agent", main: true },
  { from: "agent", to: "run", main: true },
  { from: "run", to: "runtime", main: true },
  { from: "runtime", to: "issue", main: true },
  { from: "issue", to: "inbox" },
];

const MAIN_PATH = new Set<NodeId>(["issue", "agent", "run", "runtime"]);

const copy = {
  en: {
    kicker: "ARCHITECTURE",
    title: "Core objects and execution flow",
    cols: ["01 · WORK & TRIGGERS", "02 · ORCHESTRATION", "03 · LOCAL EXECUTION"],
    edgeLabels: {
      trigger: "assign / @",
      create: "creates",
      claim: "claims",
      writeback: "logs, comments & PRs written back",
      notify: "notifies",
    },
    hint: "Click any object to inspect its role and connected paths.",
    seeDoc: "Read",
    nodes: {
      project: { code: "PROJECT", name: "Project", desc: "Groups related issues under one goal and binds shared repos, directories, and workflows.", page: "Projects", href: "/projects" },
      issue: { code: "ISSUE", name: "Issue", desc: "A unit of work holding description, status, comments, PRs, and execution history.", page: "Issues", href: "/issues" },
      triggers: { code: "TRIGGER", name: "Chat / Autopilot", desc: "Alternative triggers outside an issue: 1:1 chats, schedules, and external webhooks.", page: "Triggering agents", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "Reusable capability packages synced to runtimes so proven methods scale across agents.", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "Squad", desc: "A leader-coordinated group of agents and humans assignable as a single owner.", page: "Squads", href: "/squads" },
      agent: { code: "AGENT", name: "Agent", desc: "Reusable AI collaborator configuration: instructions, model, skills, Access, and runtime.", page: "Agents", href: "/agents" },
      run: { code: "RUN", name: "Run", desc: "One concrete execution attempt for an agent. One issue can produce many runs over time.", page: "Runs", href: "/tasks" },
      runtime: { code: "DAEMON", name: "Runtime", desc: "A connected computer and its local AI coding tools where execution actually runs.", page: "Daemon & runtimes", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "Inbox", desc: "Member notification center for mentions, assignments, and subscribed issue activity.", page: "Inbox", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
  zh: {
    kicker: "架构总览",
    title: "核心对象与执行链路",
    cols: ["01 · 工作与触发", "02 · 智能体编排", "03 · 运行与本机执行"],
    edgeLabels: {
      trigger: "指派 / @",
      create: "创建",
      claim: "领取",
      writeback: "执行日志、评论与 PR 回写",
      notify: "通知",
    },
    hint: "点击任一对象查看职责与关联路径。",
    seeDoc: "详见",
    nodes: {
      project: { code: "PROJECT", name: "项目", desc: "把相关任务组织在一个目标下，绑定共享代码仓库、本地目录与工作流。", page: "项目", href: "/projects" },
      issue: { code: "ISSUE", name: "任务", desc: "日常工作的基本单位，沉淀需求描述、状态、讨论、关联 PR 与全部执行历史。", page: "任务", href: "/issues" },
      triggers: { code: "TRIGGER", name: "对话 / 自动化", desc: "任务之外的触发入口：一对一对话、定时计划或外部 Webhook 事件。", page: "让智能体开始工作", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "可复用的能力包，执行时自动同步到运行时，让成熟做法跨智能体复用。", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "小队", desc: "由队长智能体统筹的协作组，可直接作为任务负责人自动拆解与协调。", page: "小队", href: "/squads" },
      agent: { code: "AGENT", name: "智能体", desc: "工作区内的可复用 AI 协作者配置：指令、模型、Skills、Access 与运行时。", page: "智能体", href: "/agents" },
      run: { code: "RUN", name: "运行", desc: "智能体的一次具体执行记录；同一条任务可先后产生多次运行。", page: "运行", href: "/tasks" },
      runtime: { code: "DAEMON", name: "运行时", desc: "连接到 Multica 的电脑及其本地 AI 编程工具，实际执行发生在这里。", page: "守护进程与运行时", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "收件箱", desc: "成员的通知中心，汇聚被分配、@提及与订阅任务的最新动态。", page: "收件箱与订阅", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
  "zh-Hant": {
    kicker: "架構總覽",
    title: "核心物件與執行鏈路",
    cols: ["01 · 工作與觸發", "02 · 智能體編排", "03 · 執行與本機環境"],
    edgeLabels: {
      trigger: "指派 / @",
      create: "建立",
      claim: "領取",
      writeback: "執行日誌、評論與 PR 回寫",
      notify: "通知",
    },
    hint: "點擊任一物件檢視職責與關聯路徑。",
    seeDoc: "詳見",
    nodes: {
      project: { code: "PROJECT", name: "專案", desc: "將相關任務組織在同一個目標下，綁定共享倉庫、目錄與工作流。", page: "專案", href: "/projects" },
      issue: { code: "ISSUE", name: "任務", desc: "日常工作的基本單位，沉澱描述、狀態、討論、PR 與執行歷史。", page: "任務", href: "/issues" },
      triggers: { code: "TRIGGER", name: "對話 / 自動化", desc: "任務之外的觸發入口：一對一對話、排程或外部 Webhook。", page: "讓智能體開始工作", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "可複用的能力包，執行時自動同步至執行環境。", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "小隊", desc: "由隊長智能體統籌的協作群組，可直接指派任務。", page: "小隊", href: "/squads" },
      agent: { code: "AGENT", name: "智能體", desc: "可複用的 AI 協作者設定：指令、模型、Skills、Access 與執行環境。", page: "智能體", href: "/agents" },
      run: { code: "RUN", name: "執行", desc: "智能體的一次具體執行記錄；同一條任務可產生多次執行。", page: "執行", href: "/tasks" },
      runtime: { code: "DAEMON", name: "執行環境", desc: "連接至 Multica 的電腦與本機 AI 程式設計工具。", page: "守護行程與執行環境", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "收件箱", desc: "成員的通知中心，彙整指派、@提及與訂閱任務動態。", page: "收件箱與訂閱", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
  ja: {
    kicker: "ARCHITECTURE",
    title: "コアオブジェクトと実行フロー",
    cols: ["01 · 作業とトリガー", "02 · 編成", "03 · ローカル実行"],
    edgeLabels: {
      trigger: "割当 / @",
      create: "作成",
      claim: "取得",
      writeback: "ログ・コメント・PR を書き戻す",
      notify: "通知",
    },
    hint: "任意のオブジェクトをクリックして役割と接続を確認できます。",
    seeDoc: "詳細:",
    nodes: {
      project: { code: "PROJECT", name: "プロジェクト", desc: "関連タスクを束ね、共有リポジトリ・ディレクトリ・ワークフローを紐づけます。", page: "プロジェクト", href: "/projects" },
      issue: { code: "ISSUE", name: "タスク", desc: "説明・ステータス・議論・PR・実行履歴が集まる作業の基本単位です。", page: "タスク", href: "/issues" },
      triggers: { code: "TRIGGER", name: "チャット / 自動化", desc: "タスク外のトリガー：1対1チャット、スケジュール、Webhook。", page: "エージェントを動かす", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "再利用可能な手順パッケージ。実行時にランタイムへ同期されます。", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "スクワッド", desc: "リーダーエージェントが統括するチーム単位の担当者です。", page: "スクワッド", href: "/squads" },
      agent: { code: "AGENT", name: "エージェント", desc: "指示・モデル・Skills・アクセス・ランタイムをまとめた再利用可能な設定です。", page: "エージェント", href: "/agents" },
      run: { code: "RUN", name: "実行", desc: "エージェントの 1 回の具体的な実行記録です。", page: "実行", href: "/tasks" },
      runtime: { code: "DAEMON", name: "ランタイム", desc: "Multica に接続されたコンピュータとローカル AI コーディングツールです。", page: "デーモンとランタイム", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "インボックス", desc: "メンション・割り当て・購読タスクの通知が集まる場所です。", page: "インボックス", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
  ko: {
    kicker: "ARCHITECTURE",
    title: "핵심 객체와 실행 흐름",
    cols: ["01 · 작업 및 트리거", "02 · 오케스트레이션", "03 · 로컬 실행"],
    edgeLabels: {
      trigger: "할당 / @",
      create: "생성",
      claim: "수신",
      writeback: "로그·댓글·PR 기록 반환",
      notify: "알림",
    },
    hint: "객체를 클릭해 역할과 연결 경로를 확인하세요.",
    seeDoc: "자세히:",
    nodes: {
      project: { code: "PROJECT", name: "프로젝트", desc: "관련 태스크를 묶고 공유 저장소·디렉터리·워크플로를 연결합니다.", page: "프로젝트", href: "/projects" },
      issue: { code: "ISSUE", name: "태스크", desc: "설명, 상태, 댓글, PR, 실행 기록이 쌓이는 작업의 기본 단위입니다.", page: "태스크", href: "/issues" },
      triggers: { code: "TRIGGER", name: "채팅 / 오토파일럿", desc: "태스크 외 트리거: 1:1 채팅, 예약 실행, 외부 웹훅.", page: "에이전트 시작하기", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "여러 에이전트에 재사용할 수 있는 역량 패키지입니다.", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "스쿼드", desc: "리더 에이전트가 조율하는 에이전트·멤버 그룹입니다.", page: "스쿼드", href: "/squads" },
      agent: { code: "AGENT", name: "에이전트", desc: "지침, 모델, Skills, 접근 권한, 런타임을 담은 재사용 구성입니다.", page: "에이전트", href: "/agents" },
      run: { code: "RUN", name: "실행", desc: "에이전트의 개별 실행 기록입니다. 한 태스크에 여러 실행이 쌓일 수 있습니다.", page: "실행", href: "/tasks" },
      runtime: { code: "DAEMON", name: "런타임", desc: "실제 실행이 이루어지는 연결된 컴퓨터와 AI 코딩 도구입니다.", page: "데몬과 런타임", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "인박스", desc: "멘션, 할당, 구독한 태스크 활동이 모이는 멤버 알림 센터입니다.", page: "인박스", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
  fr: {
    kicker: "ARCHITECTURE",
    title: "Objets clés et flux d'exécution",
    cols: ["01 · TRAVAIL & DÉCLENCHEURS", "02 · ORCHESTRATION", "03 · EXÉCUTION LOCALE"],
    edgeLabels: {
      trigger: "assign / @",
      create: "crée",
      claim: "prend",
      writeback: "journaux, commentaires et PR réécrits",
      notify: "notifie",
    },
    hint: "Cliquez sur un objet pour inspecter son rôle et ses liens.",
    seeDoc: "Voir",
    nodes: {
      project: { code: "PROJECT", name: "Projet", desc: "Regroupe les tâches liées avec leurs dépôts, dossiers et workflows partagés.", page: "Projets", href: "/projects" },
      issue: { code: "ISSUE", name: "Tâche", desc: "Unité de travail rassemblant description, statut, discussions, PR et historique.", page: "Tâches", href: "/issues" },
      triggers: { code: "TRIGGER", name: "Chat / Auto", desc: "Déclencheurs hors tâche : discussions privées, horaires et webhooks.", page: "Déclencher un agent", href: "/triggering-agents" },
      skills: { code: "SKILL", name: "Skills", desc: "Paquets de compétences réutilisables synchronisés vers les runtimes.", page: "Skills", href: "/skills" },
      squad: { code: "SQUAD", name: "Squad", desc: "Groupe coordonné par un agent leader, assignable comme un responsable unique.", page: "Squads", href: "/squads" },
      agent: { code: "AGENT", name: "Agent", desc: "Collaborateur IA réutilisable : instructions, modèle, skills, accès et runtime.", page: "Agents", href: "/agents" },
      run: { code: "RUN", name: "Exécution", desc: "Une tentative concrète d'exécution. Une tâche peut produire plusieurs exécutions.", page: "Exécutions", href: "/tasks" },
      runtime: { code: "DAEMON", name: "Runtime", desc: "Ordinateur connecté et outils de codage IA locaux où le travail s'exécute.", page: "Daemon et runtimes", href: "/daemon-runtimes" },
      inbox: { code: "INBOX", name: "Boîte", desc: "Centre de notifications des membres pour mentions, assignations et abonnements.", page: "Boîte de réception", href: "/inbox" },
    } as Record<NodeId, NodeCopy>,
  },
};

export function ConceptMapDemo() {
  const t = useDemoCopy(copy);
  const [active, setActive] = useState<NodeId | null>(null);

  const isEdgeLit = (from: NodeId, to: NodeId, main?: boolean) => {
    if (!active) return Boolean(main);
    return from === active || to === active;
  };

  const isNodeLit = (id: NodeId) => {
    if (!active) return MAIN_PATH.has(id);
    if (id === active) return true;
    return EDGES.some((e) => (e.from === active && e.to === id) || (e.to === active && e.from === id));
  };

  const nodeFill = (id: NodeId) => {
    if (active === id) return "var(--accent)";
    if (!active && MAIN_PATH.has(id)) return "var(--accent)";
    return "var(--card)";
  };

  const nodeStroke = (id: NodeId) => {
    if (active === id || (!active && MAIN_PATH.has(id))) return "var(--primary)";
    if (active && isNodeLit(id)) return "var(--primary)";
    return "var(--border)";
  };

  const nodeOpacity = (id: NodeId) => (active && !isNodeLit(id) ? 0.42 : 1);

  const selectedNode = active ? t.nodes[active] : null;

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      caption={
        selectedNode ? (
          <span className="flex flex-wrap items-baseline justify-between gap-2">
            <span>
              <strong className="font-semibold text-foreground">{selectedNode.name}</strong>
              {" — "}
              {selectedNode.desc}
            </span>
            <LocaleLink
              href={selectedNode.href}
              className="inline-flex shrink-0 items-center gap-1 font-medium text-[var(--primary)] hover:underline"
            >
              {t.seeDoc} {selectedNode.page}
              <ArrowRight aria-hidden className="size-3" />
            </LocaleLink>
          </span>
        ) : (
          t.hint
        )
      }
    >
      <svg
        viewBox="0 0 640 276"
        className="block h-auto w-full select-none"
        role="img"
        aria-label={t.title}
      >
        <defs>
          <marker id="cm-arr-hi" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
          <marker id="cm-arr-lo" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5.5" markerHeight="5.5" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--docs-faint)" strokeWidth="1.3" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Column headers */}
        <text x="16" y="18" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="1">
          {t.cols[0]}
        </text>
        <text x="220" y="18" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="1">
          {t.cols[1]}
        </text>
        <text x="434" y="18" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9.5" letterSpacing="1">
          {t.cols[2]}
        </text>
        <line x1="16" y1="26" x2="624" y2="26" stroke="var(--border)" strokeWidth="1" />

        {/* Top supporting connectors */}
        <path
          d="M72 76 V112"
          fill="none"
          stroke={isEdgeLit("project", "issue") ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("project", "issue") ? 1.5 : 1.15}
          markerEnd={isEdgeLit("project", "issue") ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <path
          d="M178 76 V96 H262 V112"
          fill="none"
          stroke={isEdgeLit("triggers", "agent") ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("triggers", "agent") ? 1.5 : 1.15}
          markerEnd={isEdgeLit("triggers", "agent") ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <path
          d="M276 76 V112"
          fill="none"
          stroke={isEdgeLit("skills", "agent") ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("skills", "agent") ? 1.5 : 1.15}
          strokeDasharray="3 3"
          markerEnd={isEdgeLit("skills", "agent") ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <path
          d="M358 76 V96 H312 V112"
          fill="none"
          stroke={isEdgeLit("squad", "agent") ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("squad", "agent") ? 1.5 : 1.15}
          strokeDasharray="3 3"
          markerEnd={isEdgeLit("squad", "agent") ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />

        {/* Inbox connector (bottom-left) */}
        <path
          d="M46 164 V216"
          fill="none"
          stroke={isEdgeLit("issue", "inbox") ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("issue", "inbox") ? 1.5 : 1.15}
          markerEnd={isEdgeLit("issue", "inbox") ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <text x="53" y="194" fill="var(--muted-foreground)" fontSize="9.5">
          {t.edgeLabels.notify}
        </text>

        {/* Main horizontal spine */}
        <path
          d="M132 138 H216"
          fill="none"
          stroke={isEdgeLit("issue", "agent", true) ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("issue", "agent", true) ? 1.75 : 1.2}
          markerEnd={isEdgeLit("issue", "agent", true) ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <path
          d="M350 138 H430"
          fill="none"
          stroke={isEdgeLit("agent", "run", true) ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("agent", "run", true) ? 1.75 : 1.2}
          markerEnd={isEdgeLit("agent", "run", true) ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />
        <path
          d="M512 138 H540"
          fill="none"
          stroke={isEdgeLit("run", "runtime", true) ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("run", "runtime", true) ? 1.75 : 1.2}
          markerEnd={isEdgeLit("run", "runtime", true) ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />

        {/* Unobstructed write-back loop */}
        <path
          d="M582 164 V232 H112 V166"
          fill="none"
          stroke={isEdgeLit("runtime", "issue", true) ? "var(--primary)" : "var(--docs-faint)"}
          strokeWidth={isEdgeLit("runtime", "issue", true) ? 1.5 : 1.15}
          strokeDasharray="4 3"
          markerEnd={isEdgeLit("runtime", "issue", true) ? "url(#cm-arr-hi)" : "url(#cm-arr-lo)"}
        />

        {/* Edge verb labels */}
        <text x="174" y="130" textAnchor="middle" fill={isEdgeLit("issue", "agent", true) ? "var(--primary)" : "var(--muted-foreground)"} fontSize="10" fontWeight="500">
          {t.edgeLabels.trigger}
        </text>
        <text x="390" y="130" textAnchor="middle" fill={isEdgeLit("agent", "run", true) ? "var(--primary)" : "var(--muted-foreground)"} fontSize="10" fontWeight="500">
          {t.edgeLabels.create}
        </text>
        <text x="526" y="130" textAnchor="middle" fill={isEdgeLit("run", "runtime", true) ? "var(--primary)" : "var(--muted-foreground)"} fontSize="10" fontWeight="500">
          {t.edgeLabels.claim}
        </text>
        <rect x="266" y="222" width="188" height="20" rx="3" fill="var(--card)" />
        <text x="360" y="235" textAnchor="middle" fill={isEdgeLit("runtime", "issue", true) ? "var(--primary)" : "var(--muted-foreground)"} fontSize="10" fontWeight="500">
          {t.edgeLabels.writeback}
        </text>

        {/* Supporting top nodes */}
        {(
          [
            ["project", 16, 40, 112, 36],
            ["triggers", 136, 40, 92, 36],
            ["skills", 240, 40, 74, 36],
            ["squad", 322, 40, 74, 36],
          ] as [NodeId, number, number, number, number][]
        ).map(([id, x, y, w, h]) => (
          <g
            key={id}
            style={{ cursor: "pointer", opacity: nodeOpacity(id) }}
            onClick={() => setActive(active === id ? null : id)}
            onMouseEnter={() => setActive(id)}
            onMouseLeave={() => setActive(null)}
          >
            <rect x={x} y={y} width={w} height={h} rx="4" fill={nodeFill(id)} stroke={nodeStroke(id)} strokeWidth={active === id ? 1.5 : 1} />
            <text x={x + w / 2} y={y + 22} textAnchor="middle" fill="var(--foreground)" fontSize="12" fontWeight="500">
              {t.nodes[id].name}
            </text>
          </g>
        ))}

        {/* Bottom-left Inbox node */}
        <g
          style={{ cursor: "pointer", opacity: nodeOpacity("inbox") }}
          onClick={() => setActive(active === "inbox" ? null : "inbox")}
          onMouseEnter={() => setActive("inbox")}
          onMouseLeave={() => setActive(null)}
        >
          <rect x="16" y="218" width="84" height="36" rx="4" fill={nodeFill("inbox")} stroke={nodeStroke("inbox")} strokeWidth={active === "inbox" ? 1.5 : 1} />
          <text x="58" y="240" textAnchor="middle" fill="var(--foreground)" fontSize="12" fontWeight="500">
            {t.nodes.inbox.name}
          </text>
        </g>

        {/* Primary spine nodes */}
        {(
          [
            ["issue", 16, 112, 116, 52],
            ["agent", 218, 112, 132, 52],
            ["run", 432, 112, 80, 52],
            ["runtime", 542, 112, 82, 52],
          ] as [NodeId, number, number, number, number][]
        ).map(([id, x, y, w, h]) => (
          <g
            key={id}
            style={{ cursor: "pointer", opacity: nodeOpacity(id) }}
            onClick={() => setActive(active === id ? null : id)}
            onMouseEnter={() => setActive(id)}
            onMouseLeave={() => setActive(null)}
          >
            <rect
              x={x}
              y={y}
              width={w}
              height={h}
              rx="4"
              fill={nodeFill(id)}
              stroke={nodeStroke(id)}
              strokeWidth={1.5}
            />
            <text x={x + 11} y={y + 19} fill="var(--primary)" fontFamily="var(--font-mono)" fontSize="9" fontWeight="600">
              {t.nodes[id].code}
            </text>
            <text x={x + 11} y={y + 38} fill="var(--foreground)" fontSize="13.5" fontWeight="600">
              {t.nodes[id].name}
            </text>
          </g>
        ))}
      </svg>
    </EditorialFigure>
  );
}
