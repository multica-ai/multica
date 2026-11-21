import { AlarmClock, BellOff, CircleDot, Command, Folder, FolderKanban, Gauge, Globe, History, Layers, ListChecks, ListPlus, Lock, MessageSquarePlus, Mic, Palette, PanelLeft, Pin, Quote, Server, Sparkles, Terminal, Users, WeChat, Workflow, type IconComponent } from "@multica/ui/icons";
import type { SupportedLocale } from "@multica/core/i18n";
import type { WorkspacePaths } from "@multica/core/paths";
import type { ReleaseStamp } from "@multica/core/whats-new";
import { ANNOTATIONS_HERO, CHAT_STEERING_HERO, CHAT_WAKEUP_HERO, ENVIRONMENTS_HERO, PROJECT_FOLDERS_HERO, SNOOZE_HERO, THEMES_HERO, VOICE_HERO, WEIXIN_HERO, type WhatsNewHero } from "./heroes";

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
  /** English is required; production entries in WHATS_NEW_RELEASES must cover every SupportedLocale. */
  copy: { en: HighlightCopy } & Partial<Record<SupportedLocale, HighlightCopy>>;
  action?: HighlightAction;
  /** Puts the highlight in the What's new deck; highlights without one stay in the panel. */
  hero?: WhatsNewHero;
}

export interface WhatsNewRelease extends ReleaseStamp {
  highlights: WhatsNewHighlight[];
}

/** The first release dealt as a corner deck; see WHATS_NEW_RELEASES. */
export const WHATS_NEW_DECK_SINCE = "0.9.0";

/**
 * In-app highlights per release, newest first. Not the changelog: only
 * user-visible changes worth discovering, with copy that
 * says how to use the feature. Fix-only releases get no entry, so they are
 * never marked as new in the panel. Versions and dates must
 * match the changelog entry the highlights come from.
 *
 * From WHATS_NEW_DECK_SINCE on, every entry needs at least one highlight with
 * hero art: only those reach the corner deck, and an entry without one would
 * reach users only through the Help menu.
 */
export type CompleteWhatsNewRelease = Omit<WhatsNewRelease, "highlights"> & {
  highlights: (Omit<WhatsNewHighlight, "copy"> & {
    copy: Record<SupportedLocale, HighlightCopy>;
  })[];
};

export const WHATS_NEW_RELEASES: CompleteWhatsNewRelease[] = [
  {
    version: "0.11.0",
    date: "2026-11-18",
    highlights: [
      {
        id: "chat-wakeups",
        icon: AlarmClock,
        hero: CHAT_WAKEUP_HERO,
        action: { kind: "route", to: (p) => p.chat() },
        copy: {
          en: {
            title: "Agents come back to report in chat",
            body: "Ask an agent in chat to tell you when an issue is done or a PR merges, and it reports back in the same conversation; it also follows up when it asks someone else for you. The strip above the message box shows what it's waiting for.",
          },
          "zh-Hans": {
            title: "智能体会回到聊天里汇报",
            body: "在聊天中让智能体在任务完成或 PR 合并时告诉你，它会回到同一个对话里汇报；替你找人要答复或评审时，它也会自动跟进。输入框上方的提示条会显示它正在等什么。",
          },
          "zh-Hant": {
            title: "AI 代理會回到聊天裡匯報",
            body: "在聊天中請 AI 代理在任務完成或 PR 合併時告訴你，它會回到同一個對話裡匯報；替你找人要答覆或審查時，它也會自動跟進。輸入框上方的提示列會顯示它正在等什麼。",
          },
          ja: {
            title: "エージェントがチャットに戻って報告",
            body: "チャットでエージェントに、タスクの完了や PR のマージを知らせるよう頼むと、同じ会話に戻って報告します。あなたの代わりに誰かへ回答やレビューを頼んだときも自動でフォローし、待っている内容は入力欄の上のバーに表示されます。",
          },
          ko: {
            title: "에이전트가 채팅으로 돌아와 알려 주기",
            body: "채팅에서 에이전트에게 태스크가 끝나거나 PR이 머지되면 알려 달라고 하면, 같은 대화로 돌아와 알려 줍니다. 나 대신 다른 사람에게 답변이나 리뷰를 요청할 때도 알아서 후속 확인하며, 기다리는 내용은 입력창 위 막대에 표시됩니다.",
          },
          fr: {
            title: "Les agents reviennent faire le point dans la discussion",
            body: "Dans une discussion, demandez à un agent de vous prévenir quand une tâche est terminée ou qu'une PR est fusionnée : il revient faire le point dans la même conversation. Il assure aussi le suivi quand il sollicite quelqu'un pour vous, et le bandeau au-dessus de la zone de saisie montre ce qu'il attend.",
          },
          es: {
            title: "Los agentes vuelven al chat para informar",
            body: "Pide a un agente en el chat que te avise cuando una tarea termine o se fusione un PR, y volverá a informar en la misma conversación; también hace seguimiento cuando consulta a otra persona por ti. La barra sobre el cuadro de texto muestra qué está esperando.",
          },
          ru: {
            title: "Агенты возвращаются с отчётом в чат",
            body: "Попросите агента в чате сообщить, когда задача завершится или PR будет слит, — он отчитается в том же диалоге и напомнит о себе, если ждёт ответа от коллеги. Полоса над полем ввода показывает, чего именно он ждёт.",
          },
        },
      },
      {
        id: "chat-steering",
        icon: MessageSquarePlus,
        hero: CHAT_STEERING_HERO,
        action: { kind: "route", to: (p) => p.chat() },
        copy: {
          en: {
            title: "Add to a reply while it's running",
            body: "While an agent replies in chat, keep typing and send: when the box says Add to current run, your message joins that reply without stopping it. With an attachment, among other cases, it says Start after this run and waits for the next turn.",
          },
          "zh-Hans": {
            title: "在回复进行中补充要求",
            body: "智能体在聊天中回复时，可以继续输入并发送：输入框显示「插话到当前运行」时，消息会并入这次回复，不会打断它。带附件等情况会显示「本轮结束后开始」，消息会等到下一轮再处理。",
          },
          "zh-Hant": {
            title: "在回覆進行中補充要求",
            body: "AI 代理在聊天中回覆時，可以繼續輸入並送出：輸入框顯示「插話到當前執行」時，訊息會併入這次回覆，不會打斷它。帶附件等情況會顯示「本輪結束後開始」，訊息會等到下一輪再處理。",
          },
          ja: {
            title: "返信の途中で指示を追加",
            body: "チャットでエージェントが返信している間も入力して送信でき、入力欄に「現在の実行に追加」と出ていれば、返信を止めずにその返信に加わります。添付ファイルがある場合などは「この実行の後に開始」と表示され、次のターンで処理されます。",
          },
          ko: {
            title: "답변 도중에 지시 추가하기",
            body: "에이전트가 채팅에서 답하는 동안에도 입력해 보낼 수 있으며, 입력창에 '현재 실행에 추가'가 보이면 답변을 멈추지 않고 그 답변에 반영됩니다. 첨부 파일이 있을 때처럼 '이번 실행 후 시작'이 보이면 다음 차례에 처리됩니다.",
          },
          fr: {
            title: "Complétez une réponse en cours",
            body: "Pendant qu'un agent répond dans une discussion, continuez à écrire et envoyez : si la zone de saisie indique Ajouter à l'exécution en cours, votre message rejoint cette réponse sans l'interrompre. Avec une pièce jointe, entre autres, elle indique Démarrer après cette exécution et le message attend le tour suivant.",
          },
          es: {
            title: "Añade indicaciones mientras responde",
            body: "Mientras un agente responde en el chat, sigue escribiendo y envía: si el cuadro indica Añadir a la ejecución actual, tu mensaje se suma sin detenerla. Con un archivo adjunto, entre otros casos, indica Empezar tras esta ejecución y espera al siguiente turno.",
          },
          ru: {
            title: "Дополняйте ответ прямо во время выполнения",
            body: "Пока агент отвечает в чате, просто напишите и отправьте сообщение: если поле показывает «Добавить в текущий запуск», оно вольётся в ответ без его остановки. С вложением или в иных случаях оно начнётся в следующем запуске.",
          },
        },
      },
      {
        id: "weixin-channel",
        icon: WeChat,
        hero: WEIXIN_HERO,
        action: { kind: "route", to: (p) => p.agents() },
        copy: {
          en: {
            title: "Chat with an agent from WeChat",
            body: "Open an agent you own, choose Add messaging channel under Messaging, pick WeChat, and scan the QR code with your own WeChat. Then message the bot privately in WeChat; replies and files come back there.",
          },
          "zh-Hans": {
            title: "在微信里和智能体对话",
            body: "打开你拥有的智能体，在「消息渠道」中选择「添加消息渠道」→「微信」，用你自己的微信扫码。之后在微信里私聊这个机器人，回复和文件都会发回微信。",
          },
          "zh-Hant": {
            title: "在微信裡和 AI 代理對話",
            body: "打開你擁有的 AI 代理，在「訊息渠道」中選擇「新增訊息管道」→「微信」，用你自己的微信掃碼。之後在微信裡私訊這個機器人，回覆和檔案都會傳回微信。",
          },
          ja: {
            title: "WeChat からエージェントと話す",
            body: "自分のエージェントを開き、「メッセージング」で「メッセージチャネルを追加」→「WeChat」を選び、自分の WeChat で QR コードをスキャンします。あとは WeChat でボットに個別メッセージを送ると、返信やファイルも WeChat に届きます。",
          },
          ko: {
            title: "WeChat에서 에이전트와 대화하기",
            body: "내가 소유한 에이전트를 열고 '메신저'에서 '메시지 채널 추가' → 'WeChat'을 고른 뒤, 내 WeChat으로 QR 코드를 스캔하세요. 이후 WeChat에서 봇에게 개인 메시지를 보내면 답변과 파일도 WeChat으로 돌아옵니다.",
          },
          fr: {
            title: "Discutez avec un agent depuis WeChat",
            body: "Ouvrez un agent dont vous êtes propriétaire, choisissez Ajouter un canal de messagerie sous Messagerie, puis WeChat, et scannez le code QR avec votre propre compte WeChat. Écrivez ensuite au bot en privé dans WeChat : les réponses et les fichiers y reviennent.",
          },
          es: {
            title: "Chatea con un agente desde WeChat",
            body: "Abre un agente propio, elige Añadir canal de mensajería en Mensajería, selecciona WeChat y escanea el código QR con tu cuenta. Luego escribe al bot en privado; las respuestas y archivos volverán allí.",
          },
          ru: {
            title: "Общайтесь с агентом через WeChat",
            body: "Откройте своего агента, выберите «Добавить канал сообщений» в разделе «Сообщения», укажите WeChat и отсканируйте QR-код. После этого пишите боту в личные сообщения — ответы и файлы придут туда же.",
          },
        },
      },
      {
        id: "project-folders",
        icon: Folder,
        hero: PROJECT_FOLDERS_HERO,
        action: { kind: "route", to: (p) => p.projects() },
        copy: {
          en: {
            title: "Organize projects into folders",
            body: "In the sidebar, click + next to Projects and choose New folder, then drag projects into it or pick Move to folder from a project's menu. On Projects, open Display settings to group by folder.",
          },
          "zh-Hans": {
            title: "用文件夹整理项目",
            body: "在侧栏「项目」旁点「+」→「新建文件夹」，再把项目拖进去，或在项目菜单里选「移动到文件夹」。在「项目」页打开「显示设置」，还可以按文件夹分组。",
          },
          "zh-Hant": {
            title: "用資料夾整理專案",
            body: "在側欄「專案」旁點「+」→「新增資料夾」，再把專案拖進去，或在專案選單裡選「移動到資料夾」。在「專案」頁打開「顯示設定」，還可以按資料夾分組。",
          },
          ja: {
            title: "フォルダでプロジェクトを整理",
            body: "サイドバーの「プロジェクト」横の「+」から「新しいフォルダ」を選び、プロジェクトをドラッグして入れるか、プロジェクトのメニューで「フォルダに移動」を選びます。「プロジェクト」ページの「表示設定」では、フォルダごとにグループ化もできます。",
          },
          ko: {
            title: "폴더로 프로젝트 정리하기",
            body: "사이드바의 '프로젝트' 옆 '+'에서 '새 폴더'를 고른 뒤, 프로젝트를 끌어다 넣거나 프로젝트 메뉴에서 '폴더로 이동'을 고르세요. '프로젝트' 페이지의 '표시 설정'에서 폴더별로 그룹화할 수도 있습니다.",
          },
          fr: {
            title: "Rangez vos projets dans des dossiers",
            body: "Dans la barre latérale, cliquez sur + à côté de Projets puis sur Nouveau dossier, et glissez-y des projets ou choisissez Déplacer vers un dossier dans le menu d'un projet. Dans Projets, ouvrez Affichage pour regrouper par dossier.",
          },
          es: {
            title: "Organiza proyectos en carpetas",
            body: "En la barra lateral, haz clic en + junto a Proyectos y elige Nueva carpeta; luego arrastra proyectos o usa Mover a carpeta desde su menú. En Proyectos, abre Mostrar para agrupar por carpeta.",
          },
          ru: {
            title: "Организуйте проекты в папки",
            body: "На боковой панели нажмите + рядом с «Проекты» и выберите «Новая папка», затем перетащите туда проекты или выберите «Переместить в папку» в меню проекта. В настройках отображения можно сгруппировать проекты по папкам.",
          },
        },
      },
    ],
  },
  {
    version: "0.10.0",
    date: "2026-11-16",
    highlights: [
      {
        id: "inbox-snooze",
        icon: AlarmClock,
        hero: SNOOZE_HERO,
        action: { kind: "route", to: (p) => p.inbox() },
        copy: {
          en: {
            title: "Snooze a notification until later",
            body: "Your Inbox now sits at the top of the Work panel. Hover a notification and click the clock, or press H on an open issue, to pick when it comes back; until then it waits in the Snoozed view.",
          },
          "zh-Hans": {
            title: "把通知延后处理",
            body: "收件箱现在位于「工作」面板顶部。悬停一条通知并点闹钟按钮，或在打开的任务上按 H，选好它回来的时间；在那之前，它会待在「已延后」视图里。",
          },
          "zh-Hant": {
            title: "把通知延後處理",
            body: "收件箱現在位於「工作」面板頂部。將游標移到一則通知上並點鬧鐘按鈕，或在開啟的任務上按 H，選好它回來的時間；在那之前，它會待在「已延後」檢視裡。",
          },
          ja: {
            title: "通知をあとで処理する",
            body: "インボックスは「作業」パネルの一番上に移りました。通知にカーソルを合わせて時計ボタンを押すか、開いているタスクで H を押して、戻ってくる時間を選びます。それまでは「スヌーズ中」ビューで待機します。",
          },
          ko: {
            title: "알림을 나중으로 미루기",
            body: "인박스가 이제 '작업' 패널 맨 위에 있습니다. 알림에 마우스를 올려 시계 버튼을 누르거나 열린 태스크에서 H를 눌러 다시 알릴 시간을 고르세요. 그때까지는 '미뤄둔 항목' 보기에 있습니다.",
          },
          fr: {
            title: "Reportez une notification à plus tard",
            body: "La boîte de réception est désormais en haut du panneau Travail. Survolez une notification et cliquez sur l'horloge, ou appuyez sur H dans une tâche ouverte, pour choisir quand elle revient ; d'ici là, elle attend dans la vue Reportés.",
          },
          es: {
            title: "Pospón notificaciones para más tarde",
            body: "Tu bandeja de entrada ahora aparece arriba del panel Trabajo. Pasa el cursor por una notificación y haz clic en el reloj, o pulsa H en una tarea abierta, para elegir cuándo volverá.",
          },
          ru: {
            title: "Откладывайте уведомления на потом",
            body: "Входящие теперь находятся в верхней части панели «Работа». Наведите на уведомление и нажмите на часы — или нажмите H в открытой задаче — чтобы выбрать время возврата; до тех пор оно ждёт в разделе «Отложенные».",
          },
        },
      },
      {
        id: "desktop-voice-input",
        icon: Mic,
        hero: VOICE_HERO,
        copy: {
          en: {
            title: "Dictate instead of typing on desktop",
            body: "In the desktop app, turn on Settings → Voice input, then tap the mic in chat, a comment, or a reply to dictate, or hold it while you talk. The first time, pick a speech model to download; audio is transcribed on your computer.",
          },
          "zh-Hans": {
            title: "在桌面端用语音代替打字",
            body: "在桌面端打开「设置 → 语音输入」，然后在聊天、评论或回复的输入框里点麦克风开始听写，也可以按住麦克风说话。第一次使用时选一个语音模型下载，录音只在你的电脑上转写。",
          },
          "zh-Hant": {
            title: "在桌面端用語音代替打字",
            body: "在桌面端打開「設定 → 語音輸入」，然後在聊天、留言或回覆的輸入框裡點麥克風開始聽寫，也可以按住麥克風說話。第一次使用時選一個語音模型下載，錄音只在你的電腦上轉寫。",
          },
          ja: {
            title: "デスクトップでは入力の代わりに話す",
            body: "デスクトップアプリで「設定 → 音声入力」をオンにし、チャット、コメント、返信の入力欄でマイクを押すと音声入力が始まります。押し続けながら話すこともできます。初回は音声モデルを選んでダウンロードし、音声はこのコンピューター上で文字に変換されます。",
          },
          ko: {
            title: "데스크톱에서 타이핑 대신 말하기",
            body: "데스크톱 앱에서 '설정 → 음성 입력'을 켠 뒤 채팅, 댓글, 답글 입력창의 마이크를 눌러 받아쓰거나 누른 채로 말하세요. 처음에는 음성 모델을 골라 내려받으며, 음성은 이 컴퓨터에서 텍스트로 바뀝니다.",
          },
          fr: {
            title: "Dictez au lieu de taper dans l'app de bureau",
            body: "Dans l'app de bureau, activez Paramètres → Saisie vocale, puis cliquez sur le micro d'une discussion, d'un commentaire ou d'une réponse pour dicter, ou maintenez-le pendant que vous parlez. La première fois, choisissez un modèle vocal à télécharger ; l'audio est transcrit sur votre ordinateur.",
          },
          es: {
            title: "Dicta en lugar de escribir en escritorio",
            body: "En la app de escritorio, activa Ajustes → Entrada de voz y toca el micrófono en el chat o en un comentario para dictar, o mantenlo pulsado mientras hablas. La primera vez elige un modelo de voz; la transcripción ocurre en tu equipo.",
          },
          ru: {
            title: "Диктуйте вместо набора текста в десктопе",
            body: "В десктопном приложении включите «Настройки → Голосовой ввод» и нажмите на микрофон в чате или комментарии (или удерживайте его во время речи). При первом запуске выберите модель — распознавание выполняется локально.",
          },
        },
      },
      {
        id: "project-board-archive",
        icon: FolderKanban,
        action: { kind: "route", to: (p) => p.projects() },
        copy: {
          en: {
            title: "Plan projects on a board, and archive finished ones",
            body: "On Projects, open Display settings to switch to Board or group by status, priority, or lead, and search from the header. Archive a project from its menu to hide it, and restore it any time from Archived projects.",
          },
          "zh-Hans": {
            title: "用看板管理项目，并归档已完成的项目",
            body: "在「项目」页打开「显示设置」，可以切换到看板，或按状态、优先级、负责人分组，也可以在页头搜索。在项目菜单里选「归档」即可把它隐藏，之后随时可以在「已归档的项目」里恢复。",
          },
          "zh-Hant": {
            title: "用看板管理專案，並歸檔已完成的專案",
            body: "在「專案」頁打開「顯示設定」，可以切換到看板，或按狀態、優先順序、負責人分組，也可以在頁首搜尋。在專案選單裡選「歸檔」即可把它隱藏，之後隨時可以在「已歸檔的專案」裡還原。",
          },
          ja: {
            title: "プロジェクトをボードで管理し、終わったものはアーカイブ",
            body: "「プロジェクト」ページで「表示設定」を開くと、ボードに切り替えたり、ステータス、優先度、リードでグループ化したりでき、ヘッダーから検索もできます。プロジェクトのメニューから「アーカイブ」を選ぶと非表示になり、「アーカイブ済みのプロジェクト」からいつでも復元できます。",
          },
          ko: {
            title: "보드로 프로젝트를 관리하고 끝난 프로젝트는 보관하기",
            body: "'프로젝트' 페이지에서 '표시 설정'을 열어 보드로 바꾸거나 상태, 우선순위, 리드별로 그룹화하고, 헤더에서 검색할 수 있습니다. 프로젝트 메뉴에서 '보관'을 고르면 숨겨지고, '보관된 프로젝트'에서 언제든 복원할 수 있습니다.",
          },
          fr: {
            title: "Organisez vos projets en tableau et archivez ceux qui sont terminés",
            body: "Dans Projets, ouvrez Affichage pour passer en Tableau ou regrouper par statut, priorité ou responsable, et cherchez depuis l'en-tête. Archivez un projet depuis son menu pour le masquer, et restaurez-le à tout moment depuis Projets archivés.",
          },
          es: {
            title: "Planifica proyectos en tablero y archiva los terminados",
            body: "En Proyectos, abre Mostrar para cambiar a Tablero o agrupar por estado, prioridad o responsable, y busca desde la cabecera. Archiva un proyecto desde su menú para ocultarlo y restáuralo cuando quieras desde Proyectos archivados.",
          },
          ru: {
            title: "Планируйте проекты на доске и архивируйте завершённые",
            body: "В разделе «Проекты» переключитесь на доску или сгруппируйте по статусу, приоритету или руководителю. Завершённый проект можно скрыть через архив и в любой момент восстановить из архивных проектов.",
          },
        },
      },
      {
        id: "quieter-others-issues",
        icon: BellOff,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=notifications` },
        copy: {
          en: {
            title: "Hear less from other people's issues",
            body: "At the top of someone else's issue, click Unsubscribe & archive or press Shift+E to stop its updates in one step. Their issues now send only key updates; to get everything, set Other people's issues to All activity in Settings → Notifications.",
          },
          "zh-Hans": {
            title: "少收一些别人任务的通知",
            body: "在别人的任务顶部点「取消订阅并归档」，或按 Shift+E，一步停掉它的更新。别人的任务现在默认只通知关键更新；想收到全部，可在「设置 → 通知」把「别人的任务」改为「全部动态」。",
          },
          "zh-Hant": {
            title: "少收一些別人任務的通知",
            body: "在別人的任務頂部點「取消訂閱並歸檔」，或按 Shift+E，一步停掉它的更新。別人的任務現在預設只通知關鍵更新；想收到全部，可在「設定 → 通知」把「別人的任務」改為「全部動態」。",
          },
          ja: {
            title: "他の人のタスクの通知を減らす",
            body: "他の人のタスクの上部で「購読解除してアーカイブ」を押すか Shift+E を押すと、更新をまとめて止められます。他の人のタスクは重要な更新だけが届くようになりました。すべて受け取るには「設定 → 通知」で「他の人のタスク」を「すべてのアクティビティ」にします。",
          },
          ko: {
            title: "다른 사람의 태스크 알림 줄이기",
            body: "다른 사람의 태스크 상단에서 '구독 해제 후 보관'을 누르거나 Shift+E를 누르면 업데이트를 한 번에 끌 수 있습니다. 다른 사람의 태스크는 이제 주요 업데이트만 알립니다. 모두 받으려면 '설정 → 알림'에서 '다른 사람의 태스크'를 '모든 활동'으로 바꾸세요.",
          },
          fr: {
            title: "Moins de bruit venant des tâches des autres",
            body: "En haut de la tâche d'une autre personne, cliquez sur Se désabonner et archiver ou appuyez sur Maj+E pour couper ses mises à jour en une fois. Les tâches des autres n'envoient plus que les mises à jour clés ; pour tout recevoir, réglez Tâches des autres sur Toute l'activité dans Paramètres → Notifications.",
          },
          es: {
            title: "Recibe menos ruido de tareas ajenas",
            body: "En la parte superior de una tarea ajena, haz clic en Darse de baja y archivar o pulsa Shift+E para silenciarla en un paso. Ahora solo envían novedades clave; puedes cambiarlo en Ajustes → Notificaciones.",
          },
          ru: {
            title: "Меньше шума от чужих задач",
            body: "Вверху чужой задачи нажмите «Отписаться и архивировать» или Shift+E, чтобы отключить уведомления в один клик. По умолчанию приходят только ключевые обновления — режим меняется в «Настройки → Уведомления».",
          },
        },
      },
      {
        id: "batch-edit",
        icon: ListChecks,
        action: { kind: "route", to: (p) => p.issues() },
        copy: {
          en: {
            title: "Edit many issues at once",
            body: "Select issues in a list, then use the bar at the bottom to change labels, project, or due date; More actions covers start date, parent, and custom properties. If some are skipped, click Select only these to fix and retry them.",
          },
          "zh-Hans": {
            title: "一次修改多个任务",
            body: "在列表里选中多个任务，用底部的操作栏修改标签、项目或截止日期；开始日期、父任务和自定义属性在「更多操作」里。有任务被跳过时，点「只选中这些」单独处理它们。",
          },
          "zh-Hant": {
            title: "一次修改多個任務",
            body: "在列表裡選取多個任務，用底部的操作列修改標籤、專案或截止日期；開始日期、父任務和自訂屬性在「更多操作」裡。有任務被略過時，點「只選取這些」單獨處理它們。",
          },
          ja: {
            title: "複数のタスクをまとめて編集",
            body: "リストでタスクを複数選び、下部のバーでラベル、プロジェクト、期限を変更します。開始日、親タスク、カスタムプロパティは「その他の操作」にあります。スキップされたタスクがあれば「この件だけを選択」で選び直して対応できます。",
          },
          ko: {
            title: "여러 태스크를 한 번에 편집하기",
            body: "목록에서 태스크를 여러 개 선택한 뒤 하단 바에서 라벨, 프로젝트, 마감일을 바꾸세요. 시작일, 상위 태스크, 사용자 지정 속성은 '더 보기'에 있습니다. 건너뛴 태스크가 있으면 '이 항목만 선택'으로 골라 따로 처리할 수 있습니다.",
          },
          fr: {
            title: "Modifiez plusieurs tâches à la fois",
            body: "Sélectionnez des tâches dans une liste, puis utilisez la barre du bas pour changer les étiquettes, le projet ou la date d'échéance ; Plus d'actions couvre la date de début, le parent et les propriétés personnalisées. Si certaines sont ignorées, cliquez sur Sélectionner seulement ces tâches pour les reprendre.",
          },
          es: {
            title: "Edita varias tareas a la vez",
            body: "Selecciona tareas en una lista y usa la barra inferior para cambiar etiquetas, proyecto o fecha límite; Más acciones incluye fecha de inicio, tarea padre y propiedades. Si alguna se omite, haz clic en Seleccionar solo estas.",
          },
          ru: {
            title: "Редактируйте сразу несколько задач",
            body: "Отметьте задачи в списке и измените метки, проект или срок на нижней панели; в меню «Ещё» доступны дата начала, родительская задача и свойства. Пропущенные задачи можно выделить одной кнопкой и повторить.",
          },
        },
      },
      {
        id: "agent-made-themes",
        icon: Sparkles,
        action: { kind: "route", to: (p) => `${p.settings()}?tab=appearance` },
        copy: {
          en: {
            title: "Have an agent make your theme",
            body: "In a chat or an issue, ask any agent for a theme and describe the colors, background, or glass look you want. It builds and checks the theme and hands it to you; then switch to it in Settings → Appearance.",
          },
          "zh-Hans": {
            title: "让智能体帮你做主题",
            body: "在聊天或任务里请任意智能体做一个主题，说清想要的颜色、背景或玻璃效果。它会制作并检查好主题交给你，之后在「设置 → 外观」里切换即可。",
          },
          "zh-Hant": {
            title: "讓 AI 代理幫你做主題",
            body: "在聊天或任務裡請任一 AI 代理做一個主題，說清想要的顏色、背景或玻璃效果。它會製作並檢查好主題交給你，之後在「設定 → 外觀」裡切換即可。",
          },
          ja: {
            title: "エージェントにテーマを作ってもらう",
            body: "チャットやタスクで、好きなエージェントにテーマを頼み、色や背景、ガラス風の見た目など希望を伝えます。エージェントがテーマを作って確認し、渡してくれるので、「設定 → 外観」で切り替えます。",
          },
          ko: {
            title: "에이전트에게 테마 만들기 맡기기",
            body: "채팅이나 태스크에서 아무 에이전트에게 테마를 요청하고 원하는 색, 배경, 유리 효과를 설명하세요. 에이전트가 테마를 만들고 확인한 뒤 전달하면 '설정 → 모양'에서 바꿔 쓰면 됩니다.",
          },
          fr: {
            title: "Faites créer votre thème par un agent",
            body: "Dans une discussion ou une tâche, demandez un thème à n'importe quel agent en décrivant les couleurs, le fond ou l'effet verre souhaités. Il crée et vérifie le thème, puis vous le remet ; activez-le ensuite dans Paramètres → Apparence.",
          },
          es: {
            title: "Pide a un agente que cree tu tema",
            body: "En un chat o tarea, pide a cualquier agente un tema describiendo los colores, fondo o estilo que prefieras. El agente lo crea, lo valida y te lo entrega para activarlo en Ajustes → Apariencia.",
          },
          ru: {
            title: "Попросите агента создать тему оформления",
            body: "В чате или задаче опишите агенту нужные цвета, фон или стиль интерфейса. Он соберёт и проверит тему, после чего её можно включить в «Настройки → Внешний вид».",
          },
        },
      },
    ],
  },
  {
    version: "0.9.0",
    date: "2026-11-13",
    highlights: [
      {
        id: "community-themes",
        icon: Palette,
        hero: THEMES_HERO,
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
          es: {
            title: "Instala, importa o crea un tema",
            body: "En Ajustes → Apariencia, haz clic en Gestionar junto a Tema para instalar temas de la comunidad, o usa Importar tema para un archivo .zip o carpeta. Cambia de tema en cualquier momento desde el menú de cuenta.",
          },
          ru: {
            title: "Устанавливайте и импортируйте темы",
            body: "В «Настройки → Внешний вид» нажмите «Управление» рядом с темой, чтобы установить темы сообщества или импортировать .zip / папку. Переключать темы можно в любой момент из меню аккаунта.",
          },
        },
      },
      {
        id: "environment-controls",
        icon: Server,
        hero: ENVIRONMENTS_HERO,
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
          es: {
            title: "Ejecuta los servicios de una tarea desde su barra lateral",
            body: "Abre una tarea y usa Iniciar un entorno en Entornos. El menú de cada servicio permite ver registros, reiniciar o detenerlo, y en escritorio puedes traer ventanas al frente con Cambiar a ventana.",
          },
          ru: {
            title: "Управляйте сервисами задачи с боковой панели",
            body: "Откройте задачу и запустите окружение в разделе «Окружения». Из меню каждого сервиса можно открыть логи, перезапустить или остановить его, а на десктопе — перейти прямо в окно приложения.",
          },
        },
      },
      {
        id: "chat-annotations",
        icon: Quote,
        hero: ANNOTATIONS_HERO,
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
          es: {
            title: "Anota la respuesta de un agente en el chat",
            body: "En el chat, selecciona texto de una respuesta del agente y añade una nota. Las notas guardadas esperan en el cuadro de texto y se envían con tu siguiente mensaje.",
          },
          ru: {
            title: "Оставляйте пометки к ответам агента",
            body: "В чате выделите фрагмент ответа агента и добавьте заметку. Сохранённые заметки ждут в поле ввода и отправляются вместе со следующим сообщением.",
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
          es: {
            title: "Elige tema y tipografía en Apariencia",
            body: "Ve a Ajustes → Apariencia para elegir entre cinco temas integrados, configurar fuentes y tamaños de interfaz y contenido, y ajustar densidad y contraste en este dispositivo.",
          },
          ru: {
            title: "Выбирайте тему и шрифты во «Внешнем виде»",
            body: "Откройте «Настройки → Внешний вид», чтобы выбрать одну из пяти встроенных тем, настроить шрифты, размеры текста, плотность и контрастность для этого устройства.",
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
          es: {
            title: "Limita un proyecto a sus miembros",
            body: "Al crear un proyecto o en sus propiedades, establece Acceso en Solo miembros del proyecto y añade a las personas necesarias. Sus tareas, comentarios y adjuntos solo serán visibles para ellos y los administradores.",
          },
          ru: {
            title: "Ограничьте доступ к проекту его участниками",
            body: "При создании проекта или в его свойствах выберите «Только участники проекта» и добавьте нужных людей. Задачи, комментарии и вложения увидят только они, а также владельцы и администраторы рабочего пространства.",
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
          es: {
            title: "Consulta los servicios iniciados por una tarea",
            body: "Abre una tarea y revisa Entornos en su barra lateral para ver los servicios que iniciaron sus agentes, con estado, enlace y tiempo restante. En macOS y Linux los agentes pueden mantener servicios con multica env start.",
          },
          ru: {
            title: "Отслеживайте сервисы, запущенные задачей",
            body: "Откройте задачу и проверьте раздел «Окружения» на боковой панели — там показаны сервисы, запущенные агентами, их статус, ссылка и оставшееся время работы.",
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
          es: {
            title: "Restaura una versión anterior de una habilidad",
            body: "Abre una habilidad y ve a Historial para inspeccionar cada versión guardada. Al restaurar una, pasa a ser la versión más reciente para las próximas ejecuciones.",
          },
          ru: {
            title: "Восстанавливайте прошлые версии навыков",
            body: "Откройте навык и перейдите в «Историю», чтобы посмотреть сохранённые версии. Восстановленная версия станет актуальной для следующих запусков агентов.",
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
          es: {
            title: "Muestra solo tareas sin leer",
            body: "En Vistas, marca con estrella la vista Sin leer para añadirla a la barra lateral. También puedes filtrar cualquier vista por Estado de lectura → Sin leer.",
          },
          ru: {
            title: "Показывайте только непрочитанные задачи",
            body: "В «Представлениях» добавьте встроенный вид «Непрочитанные» на боковую панель или отфильтруйте любой список по «Статус прочтения → Непрочитанные».",
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
          es: {
            title: "Fija tus espacios de trabajo principales",
            body: "Abre el selector de espacios de trabajo y haz clic en el pin junto a un espacio para mantenerlo arriba. Los indicadores de no leídos siguen visibles.",
          },
          ru: {
            title: "Закрепляйте основные рабочие пространства",
            body: "Откройте переключатель рабочих пространств и нажмите значок булавки, чтобы держать нужные сверху. Индикаторы непрочитанных сохраняются.",
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
          es: {
            title: "Nuevo diseño del espacio de trabajo",
            body: "La barra izquierda alterna entre Trabajo, Equipo, Chat, Bandeja de entrada y Autopilot. Contrae o redimensiona el panel lateral para ganar espacio.",
          },
          ru: {
            title: "Новый макет рабочего пространства",
            body: "Левая панель переключает разделы «Работа», «Команда», «Чат», «Входящие» и «Автопилот». Боковую панель рядом можно свернуть или изменить её ширину.",
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
          es: {
            title: "Vistas renovadas",
            body: "Guarda vistas en favoritos, combina o excluye filtros y filtra por estado del agente o «Yo y mis agentes».",
          },
          ru: {
            title: "Обновлённые представления",
            body: "Добавляйте представления в избранное, комбинируйте или исключайте фильтры, а также фильтруйте по статусу агента или «Я и мои агенты».",
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
          es: {
            title: "Limita ejecuciones simultáneas por entorno de ejecución",
            body: "Abre un entorno de ejecución y define cuántas ejecuciones procesa a la vez. Las adicionales esperan en la cola.",
          },
          ru: {
            title: "Ограничивайте число одновременных запусков в среде выполнения",
            body: "Откройте среду выполнения и задайте лимит параллельных запусков. Остальные запуски будут ожидать в очереди.",
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
          es: {
            title: "Nueva barra de comandos",
            body: "Selecciona varias tareas y abre la barra de comandos para editarlas juntas. También busca en el espacio de trabajo y convierte una frase en una tarea para un agente.",
          },
          ru: {
            title: "Новая командная панель",
            body: "Выделите несколько задач и измените их разом через командную панель. Она также ищет по рабочему пространству и превращает фразу в новую задачу для агента.",
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
          es: {
            title: "Hasta 200 opciones por propiedad",
            body: "Las propiedades de selección única y múltiple admiten hasta 200 opciones con búsqueda por texto o pinyin.",
          },
          ru: {
            title: "До 200 вариантов в свойствах",
            body: "Свойства с одиночным и множественным выбором теперь поддерживают до 200 вариантов с поиском по тексту и пиньиню.",
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
          es: {
            title: "Ejecuta tareas en Devin CLI",
            body: "Los agentes pueden ejecutarse en tu Devin CLI local con reanudación de sesión, habilidades del proyecto y MCP.",
          },
          ru: {
            title: "Запускайте задачи через Devin CLI",
            body: "Агенты могут работать через локальный Devin CLI с продолжением сессии, навыками проекта и MCP.",
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
          es: {
            title: "Multica en chino tradicional",
            body: "Cambia el idioma de la aplicación en Ajustes → Preferencias.",
          },
          ru: {
            title: "Multica на традиционном китайском",
            body: "Переключить язык интерфейса можно в разделе «Настройки» → «Язык».",
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
          es: {
            title: "Cualquier miembro puede crear flujos de trabajo",
            body: "Todos los miembros pueden crear y duplicar flujos de trabajo. Los creadores y administradores pueden editarlos y configurar traspasos.",
          },
          ru: {
            title: "Рабочие процессы доступны каждому участнику",
            body: "Любой участник может создавать и дублировать рабочие процессы. Создатели и администраторы могут редактировать их и настраивать передачу задач.",
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
          es: {
            title: "Comparte un entorno de ejecución con miembros concretos",
            body: "Elige exactamente quién puede usar un entorno de ejecución, no solo todos o únicamente tú. Al restringir el acceso, primero verás los agentes afectados.",
          },
          ru: {
            title: "Делитесь средой выполнения с выбранными участниками",
            body: "Указывайте конкретных пользователей с доступом к среде выполнения, а не только «все» или «только я». При сужении доступа сначала показываются затронутые агенты.",
          },
        },
      },
    ],
  },
];

export function highlightCopy(highlight: WhatsNewHighlight, language: string): HighlightCopy {
  return highlight.copy[language as SupportedLocale] ?? highlight.copy.en;
}
