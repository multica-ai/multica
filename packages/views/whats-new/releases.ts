import { Command, Globe, ListPlus, Terminal, Users, Workflow, type IconComponent } from "@multica/ui/icons";
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
  /** Image shown above the expanded highlight in the update card. */
  media?: { src: string };
}

export interface WhatsNewRelease extends ReleaseStamp {
  highlights: WhatsNewHighlight[];
}

/**
 * In-app highlights per release, newest first. Not the changelog: only
 * user-visible changes worth discovering, 0–3 per release, with copy that
 * says how to use the feature. Fix-only releases get no entry, so they
 * neither open the card nor light the rail dot. Versions and dates must
 * match the changelog entry the highlights come from.
 */
export const WHATS_NEW_RELEASES: WhatsNewRelease[] = [
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
