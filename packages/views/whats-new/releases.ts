import { CircleDot, Command, Gauge, Globe, History, Layers, ListPlus, Lock, Palette, PanelLeft, Pin, Quote, Server, Terminal, Users, Workflow, type IconComponent } from "@multica/ui/icons";
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
    version: "0.9.0",
    date: "2026-11-13",
    highlights: [
      {
        id: "community-themes",
        icon: Palette,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=appearance` },
        copy: {
          en: {
            title: "Install, import, or make a theme",
            body: "In Settings → Appearance, click Manage next to Theme to browse and install community themes, or use Import theme for a .zip or folder. Switch themes any time from Theme in the account menu.",
          },
          "zh-Hans": {
            title: "安装、导入或制作主题",
            body: "在「设置 → 外观」里点「主题」旁的「管理」，浏览并安装社区主题；也可以用「导入主题」导入 .zip 文件或文件夹。之后随时在账户菜单的「主题」里切换。",
          },
          "zh-Hant": {
            title: "安裝、匯入或製作主題",
            body: "在「設定 → 外觀」裡點「主題」旁的「管理」，瀏覽並安裝社群主題；也可以用「匯入主題」匯入 .zip 檔案或資料夾。之後隨時在帳戶選單的「主題」裡切換。",
          },
          ja: {
            title: "テーマをインストール、読み込み、作成",
            body: "「設定 → 外観」で「テーマ」の横の「管理」を押すと、コミュニティのテーマを探してインストールできます。.zip やフォルダーは「テーマを読み込む」から追加できます。切り替えはアカウントメニューの「テーマ」からいつでもできます。",
          },
          ko: {
            title: "테마 설치, 가져오기, 만들기",
            body: "'설정 → 모양'에서 '테마' 옆의 '관리'를 누르면 커뮤니티 테마를 둘러보고 설치할 수 있습니다. .zip 파일이나 폴더는 '테마 가져오기'로 추가하세요. 테마는 계정 메뉴의 '테마'에서 언제든 바꿀 수 있습니다.",
          },
          fr: {
            title: "Installez, importez ou créez un thème",
            body: "Dans Paramètres → Apparence, cliquez sur Gérer à côté de Thème pour parcourir et installer des thèmes de la communauté, ou utilisez Importer un thème pour un .zip ou un dossier. Changez de thème à tout moment depuis Thème dans le menu du compte.",
          },
        },
      },
      {
        id: "environment-controls",
        icon: Server,
        copy: {
          en: {
            title: "Run an issue's services from its sidebar",
            body: "Open an issue and use Start an environment under Environments in its sidebar. Each service's menu can view its logs, restart, or stop it, and desktop apps can be brought forward with Switch to window.",
          },
          "zh-Hans": {
            title: "在任务侧栏里管理服务",
            body: "打开一个任务，在侧栏「环境」里点「启动环境」。每个服务的菜单里可以查看日志、重启或停止；桌面应用可以用「切到窗口」切到前台。",
          },
          "zh-Hant": {
            title: "在任務側欄裡管理服務",
            body: "打開一個任務，在側欄「環境」裡點「啟動環境」。每個服務的選單裡可以查看日誌、重新啟動或停止；桌面應用程式可以用「切到視窗」切到前景。",
          },
          ja: {
            title: "タスクのサイドバーからサービスを操作",
            body: "タスクを開き、サイドバーの「環境」で「環境を起動」を選びます。各サービスのメニューからログの表示、再起動、停止ができ、デスクトップアプリは「ウィンドウに切り替え」で前面に出せます。",
          },
          ko: {
            title: "태스크 사이드바에서 서비스 관리",
            body: "태스크를 열고 사이드바의 '환경'에서 '환경 시작'을 누르세요. 서비스마다 메뉴에서 로그 보기, 재시작, 중지를 할 수 있고, 데스크톱 앱은 '창으로 전환'으로 앞으로 가져올 수 있습니다.",
          },
          fr: {
            title: "Pilotez les services d'une tâche depuis sa barre latérale",
            body: "Ouvrez une tâche et choisissez Démarrer un environnement sous Environnements dans sa barre latérale. Le menu de chaque service permet de voir les journaux, de le redémarrer ou de l'arrêter, et Afficher la fenêtre met une app de bureau au premier plan.",
          },
        },
      },
      {
        id: "chat-annotations",
        icon: Quote,
        action: { kind: "route", to: (p) => p.chat() },
        copy: {
          en: {
            title: "Annotate an agent's chat reply",
            body: "In Chat, select text in an agent's reply and add a note. Saved notes wait in the message box and are sent with your next message, with or without more text.",
          },
          "zh-Hans": {
            title: "批注智能体的聊天回复",
            body: "在「聊天」里选中智能体回复中的文字并添加批注。保存的批注会留在输入框里，随下一条消息一起发送，也可以不另写文字直接发送。",
          },
          "zh-Hant": {
            title: "批註 AI 代理的聊天回覆",
            body: "在「聊天」裡選取 AI 代理回覆中的文字並新增批註。儲存的批註會留在輸入框裡，隨下一則訊息一起送出，也可以不另寫文字直接送出。",
          },
          ja: {
            title: "エージェントのチャット返信に注釈を付ける",
            body: "「チャット」でエージェントの返信のテキストを選び、注釈を追加します。保存した注釈は入力欄に残り、次のメッセージと一緒に送られます。注釈だけを送ることもできます。",
          },
          ko: {
            title: "에이전트 채팅 답변에 주석 달기",
            body: "'채팅'에서 에이전트 답변의 텍스트를 선택하고 주석을 추가하세요. 저장한 주석은 입력창에 남아 다음 메시지와 함께 보내지며, 주석만 보낼 수도 있습니다.",
          },
          fr: {
            title: "Annotez la réponse d'un agent dans une discussion",
            body: "Dans Discussion, sélectionnez du texte dans la réponse d'un agent et ajoutez une note. Les notes enregistrées restent dans la zone de saisie et partent avec votre prochain message, avec ou sans texte en plus.",
          },
        },
      },
    ],
  },
  {
    version: "0.8.2",
    date: "2026-11-10",
    highlights: [
      {
        id: "appearance-themes",
        icon: Palette,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=appearance` },
        copy: {
          en: {
            title: "Pick a theme and fonts in Appearance",
            body: "Go to Settings → Appearance to choose one of five built-in themes. You can also set fonts and sizes for the interface and for content, and adjust density and contrast. Your choices are saved on this device.",
          },
          "zh-Hans": {
            title: "在「外观」里选择主题和字体",
            body: "进入「设置 → 外观」，从 5 套内置主题中选一套。还可以分别设置界面文字和内容文字的字体与字号，并调整信息密度和对比度。这些选择保存在本设备上。",
          },
          "zh-Hant": {
            title: "在「外觀」裡選擇主題和字型",
            body: "進入「設定 → 外觀」，從 5 套內建主題中選一套。還可以分別設定介面文字和內容文字的字型與字級，並調整資訊密度和對比度。這些選擇儲存在本裝置上。",
          },
          ja: {
            title: "「外観」でテーマとフォントを選ぶ",
            body: "「設定 → 外観」で 5 つの組み込みテーマから選べます。インターフェースと本文のフォントや文字サイズ、表示密度やコントラストも調整できます。設定はこのデバイスに保存されます。",
          },
          ko: {
            title: "'모양'에서 테마와 글꼴 고르기",
            body: "'설정 → 모양'에서 기본 제공 테마 5가지 중 하나를 고르세요. 인터페이스와 본문의 글꼴과 글자 크기, 밀도와 대비도 조절할 수 있습니다. 설정은 이 기기에 저장됩니다.",
          },
          fr: {
            title: "Choisissez un thème et des polices dans Apparence",
            body: "Allez dans Paramètres → Apparence pour choisir l'un des cinq thèmes intégrés. Vous pouvez aussi régler les polices et tailles de l'interface et du contenu, ainsi que la densité et le contraste. Vos choix sont enregistrés sur cet appareil.",
          },
        },
      },
      {
        id: "private-projects",
        icon: Lock,
        action: { kind: "route", to: (p) => p.projects() },
        copy: {
          en: {
            title: "Keep a project to its members",
            body: "When you create a project, or later in its properties, set Access to Project members only and add the people who need it. Its issues, comments, and attachments are then visible only to them and to workspace owners and admins.",
          },
          "zh-Hans": {
            title: "让项目只对成员可见",
            body: "创建项目时，或之后在项目属性里，把「访问权限」设为「仅项目成员」，再添加需要的人。项目中的任务、评论和附件就只有他们以及工作区所有者和管理员能看到。",
          },
          "zh-Hant": {
            title: "讓專案只對成員可見",
            body: "建立專案時，或之後在專案屬性裡，把「存取權限」設為「僅專案成員」，再新增需要的人。專案中的任務、評論和附件就只有他們以及工作區所有者和管理員能看到。",
          },
          ja: {
            title: "プロジェクトをメンバーだけに公開",
            body: "プロジェクトを作るとき、またはあとからプロパティで「アクセス権」を「プロジェクトメンバーのみ」にし、必要な人を追加します。そのタスク、コメント、添付ファイルは、メンバーとワークスペースのオーナー・管理者だけが見られます。",
          },
          ko: {
            title: "프로젝트를 멤버에게만 공개",
            body: "프로젝트를 만들 때나 나중에 속성에서 '접근 권한'을 '프로젝트 멤버만'으로 정하고 필요한 사람을 추가하세요. 그 프로젝트의 태스크, 댓글, 첨부 파일은 멤버와 워크스페이스 소유자·관리자만 볼 수 있습니다.",
          },
          fr: {
            title: "Réservez un projet à ses membres",
            body: "À la création d'un projet, ou plus tard dans ses propriétés, réglez Accès sur Membres du projet uniquement et ajoutez les personnes concernées. Ses tâches, commentaires et pièces jointes ne sont alors visibles que par elles et par les propriétaires et admins de l'espace de travail.",
          },
        },
      },
      {
        id: "issue-environments",
        icon: Server,
        copy: {
          en: {
            title: "See the services an issue started",
            body: "Open an issue and check Environments in its sidebar for the services its agent runs started, with status, link, and time left. On macOS and Linux, agents can keep a service running with multica env start.",
          },
          "zh-Hans": {
            title: "查看任务启动的服务",
            body: "打开一个任务，在侧栏的「环境」里查看智能体运行时启动的服务，以及状态、链接和剩余时间。在 macOS 和 Linux 上，智能体可以用 multica env start 让服务持续运行。",
          },
          "zh-Hant": {
            title: "查看任務啟動的服務",
            body: "打開一個任務，在側欄的「環境」裡查看 AI 代理執行時啟動的服務，以及狀態、連結和剩餘時間。在 macOS 和 Linux 上，AI 代理可以用 multica env start 讓服務持續執行。",
          },
          ja: {
            title: "タスクが起動したサービスを確認",
            body: "タスクを開き、サイドバーの「環境」で、エージェントの実行が起動したサービスとその状態、リンク、残り時間を確認できます。macOS と Linux では、エージェントが multica env start でサービスを動かし続けられます。",
          },
          ko: {
            title: "태스크가 시작한 서비스 보기",
            body: "태스크를 열고 사이드바의 '환경'에서 에이전트 실행이 시작한 서비스와 상태, 링크, 남은 시간을 확인하세요. macOS와 Linux에서는 에이전트가 multica env start로 서비스를 계속 실행해 둘 수 있습니다.",
          },
          fr: {
            title: "Voyez les services lancés par une tâche",
            body: "Ouvrez une tâche et consultez Environnements dans sa barre latérale : services lancés par les exécutions de l'agent, avec leur état, leur lien et le temps restant. Sur macOS et Linux, les agents peuvent garder un service actif avec multica env start.",
          },
        },
      },
    ],
  },
  {
    version: "0.8.1",
    date: "2026-11-06",
    highlights: [
      {
        id: "skill-versions",
        icon: History,
        action: { kind: "route", to: (p) => p.skills() },
        copy: {
          en: {
            title: "Restore an earlier version of a skill",
            body: "Open a skill and go to History to see what each saved version contained. Restore one, and it becomes the newest version for your agents' next runs.",
          },
          "zh-Hans": {
            title: "把 skill 恢复到旧版本",
            body: "打开一个 skill，进入「历史」查看每个已保存版本的内容。恢复其中一个后，它会成为最新版本，智能体下次运行时使用。",
          },
          "zh-Hant": {
            title: "把 skill 還原到舊版本",
            body: "打開一個 skill，進入「歷史」查看每個已儲存版本的內容。還原其中一個後，它會成為最新版本，AI 代理下次執行時使用。",
          },
          ja: {
            title: "スキルを以前のバージョンに復元",
            body: "スキルを開いて「履歴」に進むと、保存した各バージョンの内容を確認できます。復元したバージョンが最新になり、エージェントは次回の実行から使います。",
          },
          ko: {
            title: "스킬을 이전 버전으로 복원",
            body: "스킬을 열고 '기록'으로 가면 저장된 버전마다 내용을 볼 수 있습니다. 하나를 복원하면 최신 버전이 되고, 에이전트는 다음 실행부터 사용합니다.",
          },
          fr: {
            title: "Restaurez une ancienne version d'un skill",
            body: "Ouvrez un skill et allez dans Historique pour voir le contenu de chaque version enregistrée. Restaurez-en une : elle devient la version la plus récente pour les prochaines exécutions de vos agents.",
          },
        },
      },
      {
        id: "unread-view",
        icon: CircleDot,
        action: { kind: "route", to: (p) => p.views() },
        copy: {
          en: {
            title: "See only unread issues",
            body: "In Views, star the built-in Unread view to add it to the sidebar. You can also filter any view by Read state → Unread.",
          },
          "zh-Hans": {
            title: "只看未读任务",
            body: "在「视图」里给内置的「未读」视图加星标，就能把它放进侧栏。也可以在任何视图里按「阅读状态 → 未读」筛选。",
          },
          "zh-Hant": {
            title: "只看未讀任務",
            body: "在「檢視」裡為內建的「未讀」檢視加上星號，就能把它放進側欄。也可以在任何檢視裡按「閱讀狀態 → 未讀」篩選。",
          },
          ja: {
            title: "未読のタスクだけを表示",
            body: "「ビュー」で組み込みの「未読」ビューにスターを付けると、サイドバーに追加されます。どのビューでも「既読状態 → 未読」で絞り込めます。",
          },
          ko: {
            title: "읽지 않은 태스크만 보기",
            body: "'뷰'에서 기본 제공 '읽지 않음' 뷰에 별표를 달면 사이드바에 추가됩니다. 어느 뷰에서든 '읽음 상태 → 읽지 않음'으로 필터링할 수도 있습니다.",
          },
          fr: {
            title: "N'affichez que les tâches non lues",
            body: "Dans Vues, ajoutez l'étoile à la vue intégrée Non lu pour l'afficher dans la barre latérale. Vous pouvez aussi filtrer n'importe quelle vue par État de lecture → Non lu.",
          },
        },
      },
      {
        id: "pin-workspaces",
        icon: Pin,
        copy: {
          en: {
            title: "Pin your main workspaces",
            body: "Open the workspace switcher and click the pin next to a workspace to keep it at the top. Unread dots still show on pinned workspaces.",
          },
          "zh-Hans": {
            title: "置顶常用工作区",
            body: "打开工作区切换菜单，点工作区旁的图钉即可把它固定在顶部。置顶的工作区照样显示未读提示。",
          },
          "zh-Hant": {
            title: "置頂常用工作區",
            body: "打開工作區切換選單，點工作區旁的圖釘即可把它固定在頂部。置頂的工作區照樣顯示未讀提示。",
          },
          ja: {
            title: "よく使うワークスペースをピン留め",
            body: "ワークスペースの切り替えメニューを開き、ワークスペースの横にあるピンをクリックすると上部に固定されます。ピン留めしても未読の表示は残ります。",
          },
          ko: {
            title: "자주 쓰는 워크스페이스 고정",
            body: "워크스페이스 전환 메뉴를 열고 워크스페이스 옆의 핀을 누르면 맨 위에 고정됩니다. 고정한 워크스페이스에도 읽지 않음 표시는 그대로 나옵니다.",
          },
          fr: {
            title: "Épinglez vos espaces de travail principaux",
            body: "Ouvrez le sélecteur d'espaces de travail et cliquez sur l'épingle à côté d'un espace pour le garder en haut. Les indicateurs de non-lu restent visibles.",
          },
        },
      },
    ],
  },
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
