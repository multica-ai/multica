import { Command, Gauge, Globe, Layers, ListPlus, PanelLeft, Terminal, Users, Workflow, type IconComponent } from "@multica/ui/icons";
import type { SupportedLocale } from "@multica/core/i18n";
import type { WorkspacePaths } from "@multica/core/paths";
import type { ReleaseStamp } from "@multica/core/whats-new";

export interface HighlightCopy {
  title: string;
  /** How to use it, in one or two sentences — not what changed internally. */
  body: string;
}

export type HighlightAction =
  | { kind: "command-palette" }
  | { kind: "route"; to: (paths: WorkspacePaths) => string }
  | { kind: "external"; href: string };

export interface WhatsNewHighlight {
  /** Stable within its release; used as the React key and in tests. */
  id: string;
  icon: IconComponent;
  /** English is required; a missing locale falls back to it. */
  copy: { en: HighlightCopy } & Partial<Record<SupportedLocale, HighlightCopy>>;
  action?: HighlightAction;
}

export interface WhatsNewRelease extends ReleaseStamp {
  highlights: WhatsNewHighlight[];
}

/**
 * In-app highlights per release, newest first. Not the changelog: only
 * user-visible changes worth discovering, 0–3 per release, with copy that
 * says how to use the feature. Fix-only releases get no entry, so they are
 * never marked as new in the panel. Versions and dates must
 * match the changelog entry the highlights come from.
 */
export const WHATS_NEW_RELEASES: WhatsNewRelease[] = [
  {
    version: "0.8.0",
    date: "2026-11-03",
    highlights: [
      {
        id: "workspace-layout",
        icon: PanelLeft,
        copy: {
          en: {
            title: "A new workspace layout",
            body: "The bar on the left switches between Work, Team, Chat, Inbox, and Auto. Collapse or resize the side panel next to it to make room.",
          },
          "zh-Hans": {
            title: "全新工作区布局",
            body: "左侧的导航栏在 工作、团队、聊天、收件箱 和 自动化 之间切换，旁边的侧边面板可以折叠，也可以拖动调整宽度。",
          },
          "zh-Hant": {
            title: "全新工作區佈局",
            body: "左側的導覽列在 工作、團隊、聊天、收件箱 和 自動化 之間切換，旁邊的側邊面板可以收合，也可以拖曳調整寬度。",
          },
          ja: {
            title: "新しいワークスペースレイアウト",
            body: "左のバーで 作業・チーム・チャット・受信箱・自動化 を切り替えられます。隣のサイドパネルは折りたたんだり、幅を変えたりできます。",
          },
          ko: {
            title: "새 워크스페이스 레이아웃",
            body: "왼쪽 바에서 작업, 팀, 채팅, 인박스, 자동화를 오갈 수 있습니다. 옆의 사이드 패널은 접거나 너비를 조절할 수 있습니다.",
          },
          fr: {
            title: "Une nouvelle mise en page",
            body: "La barre de gauche bascule entre Travail, Équipe, Chat, Boîte et Auto. Repliez ou redimensionnez le panneau latéral voisin pour gagner de la place.",
          },
        },
      },
      {
        id: "views",
        icon: Layers,
        action: { kind: "route", to: (p) => p.views() },
        copy: {
          en: {
            title: "Rebuilt views",
            body: "Save views as favorites, combine or exclude filters, and filter by agent status or “Me and my agents”.",
          },
          "zh-Hans": {
            title: "重做的视图",
            body: "可以收藏视图、组合或排除筛选条件，还能按智能体状态或“我和我的智能体”筛选。",
          },
          "zh-Hant": {
            title: "重做的檢視",
            body: "可以收藏檢視、組合或排除篩選條件，還能按 AI 代理狀態或「我和我的 AI 代理」篩選。",
          },
          ja: {
            title: "作り直したビュー",
            body: "ビューをお気に入りに保存し、フィルターを組み合わせたり除外したりできます。エージェントの状態や「自分と自分のエージェント」でも絞り込めます。",
          },
          ko: {
            title: "새로 만든 뷰",
            body: "뷰를 즐겨찾기에 저장하고, 필터를 조합하거나 제외할 수 있습니다. 에이전트 상태나 ‘나와 내 에이전트’로도 필터링할 수 있습니다.",
          },
          fr: {
            title: "Des vues repensées",
            body: "Ajoutez des vues en favori, combinez ou excluez des filtres, et filtrez par statut d'agent ou « Moi et mes agents ».",
          },
        },
      },
      {
        id: "runtime-concurrency",
        icon: Gauge,
        action: { kind: "route", to: (p) => p.runtimes() },
        copy: {
          en: {
            title: "Limit concurrent runs per runtime",
            body: "Open a runtime and set how many runs it handles at once. Extra runs wait in the queue.",
          },
          "zh-Hans": {
            title: "限制运行时的并发数",
            body: "打开某个运行时，设置它同时处理多少个运行，超出的运行会排队等待。",
          },
          "zh-Hant": {
            title: "限制執行時的並行數",
            body: "打開某個執行時，設定它同時處理多少個執行，超出的執行會排隊等待。",
          },
          ja: {
            title: "ランタイムの同時実行数を制限",
            body: "ランタイムを開いて、同時に処理する実行の数を設定できます。上限を超えた実行はキューで待機します。",
          },
          ko: {
            title: "런타임별 동시 실행 제한",
            body: "런타임을 열어 한 번에 처리할 실행 수를 정할 수 있습니다. 한도를 넘는 실행은 대기열에서 기다립니다.",
          },
          fr: {
            title: "Limitez les exécutions simultanées par runtime",
            body: "Ouvrez un runtime et fixez combien d'exécutions il traite à la fois. Les suivantes attendent dans la file.",
          },
        },
      },
    ],
  },
  {
    version: "0.7.2",
    date: "2026-10-30",
    highlights: [
      {
        id: "command-bar",
        icon: Command,
        action: { kind: "command-palette" },
        copy: {
          en: {
            title: "A new command bar",
            body: "Select a few issues and open the command bar to change them all at once. It also searches the workspace and turns a sentence into a new issue for an agent.",
          },
          "zh-Hans": {
            title: "全新命令栏",
            body: "选中几个任务后打开命令栏，就能一起修改。它还能搜索整个工作区，或把一句话变成交给智能体的新任务。",
          },
          "zh-Hant": {
            title: "全新命令列",
            body: "選取幾個任務後打開命令列，就能一起修改。它還能搜尋整個工作區，或把一句話變成交給 AI 代理的新任務。",
          },
          ja: {
            title: "新しいコマンドバー",
            body: "タスクをいくつか選んでコマンドバーを開くと、まとめて変更できます。ワークスペース全体の検索や、一文からエージェント向けの新しいタスクを作ることもできます。",
          },
          ko: {
            title: "새 명령 바",
            body: "태스크 몇 개를 선택하고 명령 바를 열면 한 번에 변경할 수 있습니다. 워크스페이스 전체를 검색하거나 한 문장으로 에이전트에게 맡길 새 태스크를 만들 수도 있습니다.",
          },
          fr: {
            title: "Une nouvelle barre de commandes",
            body: "Sélectionnez quelques tâches et ouvrez la barre de commandes pour les modifier en une fois. Elle cherche aussi dans tout l'espace de travail et transforme une phrase en nouvelle tâche pour un agent.",
          },
        },
      },
      {
        id: "property-options",
        icon: ListPlus,
        copy: {
          en: {
            title: "Up to 200 property options",
            body: "Single- and multi-select properties can hold up to 200 options, searchable by text or pinyin.",
          },
          "zh-Hans": {
            title: "属性选项增至 200 个",
            body: "单选和多选属性最多可设 200 个选项，支持按文字或拼音搜索。",
          },
          "zh-Hant": {
            title: "屬性選項增至 200 個",
            body: "單選和多選屬性最多可設 200 個選項，支援按文字或拼音搜尋。",
          },
          ja: {
            title: "プロパティの選択肢が 200 個まで",
            body: "単一選択・複数選択のプロパティに最大 200 個の選択肢を設定でき、文字やピンインで検索できます。",
          },
          ko: {
            title: "속성 옵션 최대 200개",
            body: "단일 선택과 다중 선택 속성에 옵션을 최대 200개까지 둘 수 있고, 텍스트나 병음으로 검색할 수 있습니다.",
          },
          fr: {
            title: "Jusqu'à 200 options par propriété",
            body: "Les propriétés à choix unique ou multiple acceptent jusqu'à 200 options, que l'on retrouve par texte ou en pinyin.",
          },
        },
      },
    ],
  },
  {
    version: "0.7.1",
    date: "2026-10-27",
    highlights: [
      {
        id: "devin-cli",
        icon: Terminal,
        action: { kind: "route", to: (p) => p.runtimes() },
        copy: {
          en: {
            title: "Run tasks on Devin CLI",
            body: "Agents can run on your local Devin CLI, with session resume, project skills, and MCP.",
          },
          "zh-Hans": {
            title: "用本机的 Devin CLI 运行任务",
            body: "智能体可以在你本机的 Devin CLI 上运行，支持恢复会话、项目 skill 和 MCP。",
          },
          "zh-Hant": {
            title: "用本機的 Devin CLI 執行任務",
            body: "AI 代理可以在你本機的 Devin CLI 上執行，支援恢復工作階段、專案 skill 和 MCP。",
          },
          ja: {
            title: "Devin CLI でタスクを実行",
            body: "エージェントがローカルの Devin CLI で動くようになりました。セッションの再開、プロジェクトの skill、MCP に対応しています。",
          },
          ko: {
            title: "Devin CLI로 태스크 실행",
            body: "에이전트가 로컬 Devin CLI에서 실행됩니다. 세션 재개, 프로젝트 skill, MCP를 지원합니다.",
          },
          fr: {
            title: "Exécutez des tâches sur Devin CLI",
            body: "Les agents peuvent tourner sur votre Devin CLI local, avec reprise de session, skills de projet et MCP.",
          },
        },
      },
      {
        id: "traditional-chinese",
        icon: Globe,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=preferences` },
        copy: {
          en: {
            title: "Multica in Traditional Chinese",
            body: "Switch the app language in Settings → Preferences.",
          },
          "zh-Hans": {
            title: "支持繁体中文",
            body: "在 设置 → 偏好设置 中切换界面语言。",
          },
          "zh-Hant": {
            title: "支援繁體中文",
            body: "在 設定 → 偏好設定 中切換介面語言。",
          },
          ja: {
            title: "繁体字中国語に対応",
            body: "設定 → 環境設定 で表示言語を切り替えられます。",
          },
          ko: {
            title: "번체 중국어 지원",
            body: "설정 → 환경설정에서 앱 언어를 바꿀 수 있습니다.",
          },
          fr: {
            title: "Multica en chinois traditionnel",
            body: "Changez la langue de l'application dans Paramètres → Préférences.",
          },
        },
      },
    ],
  },
  {
    version: "0.7.0",
    date: "2026-10-23",
    highlights: [
      {
        id: "workflows-for-everyone",
        icon: Workflow,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=workflows` },
        copy: {
          en: {
            title: "Every member can create workflows",
            body: "Any member can create and duplicate workflows. Creators and admins can edit them and set up handoffs.",
          },
          "zh-Hans": {
            title: "所有成员都能创建工作流",
            body: "任何成员都可以创建和复制工作流，创建者和管理员可以编辑并设置交接。",
          },
          "zh-Hant": {
            title: "所有成員都能建立工作流",
            body: "任何成員都可以建立和複製工作流，建立者和管理員可以編輯並設定交接。",
          },
          ja: {
            title: "全メンバーがワークフローを作成可能に",
            body: "どのメンバーもワークフローを作成・複製できます。作成者と管理者は編集や引き継ぎの設定ができます。",
          },
          ko: {
            title: "모든 멤버가 워크플로 생성 가능",
            body: "모든 멤버가 워크플로를 만들고 복제할 수 있습니다. 생성자와 관리자는 편집하고 인계를 설정할 수 있습니다.",
          },
          fr: {
            title: "Tous les membres créent des workflows",
            body: "Chaque membre peut créer et dupliquer des workflows. Les créateurs et les admins peuvent les modifier et configurer les transferts.",
          },
        },
      },
      {
        id: "runtime-sharing",
        icon: Users,
        action: { kind: "route", to: (p) => p.runtimes() },
        copy: {
          en: {
            title: "Share a runtime with chosen members",
            body: "Pick exactly who can use a runtime, not just everyone or only you. Tightening access shows the affected agents first.",
          },
          "zh-Hans": {
            title: "把运行时共享给指定成员",
            body: "可以精确选择谁能使用某个运行时，而不只是所有人或仅自己。收紧权限前会先列出受影响的智能体。",
          },
          "zh-Hant": {
            title: "把執行時共享給指定成員",
            body: "可以精確選擇誰能使用某個執行時，而不只是所有人或僅自己。收緊權限前會先列出受影響的 AI 代理。",
          },
          ja: {
            title: "ランタイムを特定のメンバーと共有",
            body: "全員か自分だけかではなく、ランタイムを使える人を個別に選べます。権限を絞る前に影響を受けるエージェントが表示されます。",
          },
          ko: {
            title: "선택한 멤버와 런타임 공유",
            body: "모두 또는 나만이 아니라 런타임을 쓸 사람을 직접 고를 수 있습니다. 권한을 줄이기 전에 영향을 받는 에이전트를 먼저 보여 줍니다.",
          },
          fr: {
            title: "Partagez un runtime avec des membres choisis",
            body: "Choisissez précisément qui peut utiliser un runtime, pas seulement tout le monde ou vous seul. Restreindre l'accès affiche d'abord les agents concernés.",
          },
        },
      },
    ],
  },
];

export function highlightCopy(highlight: WhatsNewHighlight, language: string): HighlightCopy {
  return highlight.copy[language as SupportedLocale] ?? highlight.copy.en;
}
