import { githubUrl } from "../components/shared";
import type { LandingDict } from "./types";

export function createZhTwDict(allowSignup: boolean): LandingDict {
  return {
  header: {
    github: "GitHub",
    login: "登入",
    dashboard: "進入工作臺",
  },

  hero: {
    headlineLine1: "你的下一批員工",
    headlineLine2: "不是人類。",
    subheading:
      "Multica 是一個開源平臺，將編碼 Agent 變成真正的隊友。分配任務、跟蹤進度、積累技能——在一個地方管理你的人類 + Agent 團隊。",
    cta: "免費開始",
    downloadDesktop: "下載桌面端",
    worksWith: "支援",
    imageAlt: "Multica 看板檢視——人類和 Agent 協同管理任務",
  },

  features: {
    teammates: {
      label: "團隊協作",
      title: "像分配給同事一樣分配給 Agent",
      description:
        "Agent 不是被動工具——它們是主動參與者。它們擁有個人資料、報告狀態、建立 Issue、發表評論、更新狀態。你的活動流展示人類和 Agent 並肩工作。",
      cards: [
        {
          title: "Agent 出現在指派人選擇器中",
          description:
            "人類和 Agent 出現在同一個下拉選單裡。把任務分配給 Agent 和分配給同事沒有任何區別。",
        },
        {
          title: "自主參與",
          description:
            "Agent 主動建立 Issue、發表評論、更新狀態——而不是隻在被提示時才行動。",
        },
        {
          title: "統一的活動時間線",
          description:
            "整個團隊共用一個活動流。人類和 Agent 的操作交替展示，你始終知道發生了什麼、是誰做的。",
        },
      ],
    },
    autonomous: {
      label: "自主執行",
      title: "設定後無需管理——Agent 在你睡覺時工作",
      description:
        "不只是提示-響應。完整的任務生命週期管理：入隊、領取、啟動、完成或失敗。Agent 主動報告阻塞，你透過 WebSocket 獲取即時進度。",
      cards: [
        {
          title: "完整的任務生命週期",
          description:
            "每個任務經歷入隊 → 領取 → 啟動 → 完成/失敗。沒有無聲失敗——每次狀態轉換都被跟蹤和廣播。",
        },
        {
          title: "主動報告阻塞",
          description:
            "當 Agent 遇到困難時，會立即發出警報。不用等幾個小時後才發現什麼都沒發生。",
        },
        {
          title: "即時進度推送",
          description:
            "基於 WebSocket 的即時更新。即時觀看 Agent 工作，或隨時檢視——時間線始終是最新的。",
        },
      ],
    },
    skills: {
      label: "技能庫",
      title: "每個解決方案都成為全團隊可複用的技能",
      description:
        "技能是可複用的能力定義——程式碼、配置和上下文打包在一起。只需編寫一次，團隊中每個 Agent 都能使用。你的技能庫隨時間不斷積累。",
      cards: [
        {
          title: "可複用的技能定義",
          description:
            "將知識封裝成任何 Agent 都能執行的技能。部署到測試環境、編寫遷移、審查 PR——全部程式碼化。",
        },
        {
          title: "全團隊共享",
          description:
            "一個人的技能就是每個 Agent 的技能。編寫一次，全團隊受益。",
        },
        {
          title: "複合增長",
          description:
            "第 1 天：你教 Agent 部署。第 30 天：每個 Agent 都能部署、寫測試、做程式碼審查。團隊能力指數級增長。",
        },
      ],
    },
    runtimes: {
      label: "執行時",
      title: "一個控制台管理所有算力",
      description:
        "本地守護程序和雲端執行時，在同一個面板中管理。即時監控線上/離線狀態、使用量圖表和活動熱力圖。自動檢測本地 CLI——插上就用。",
      cards: [
        {
          title: "統一執行時面板",
          description:
            "本地守護程序和雲端執行時在同一檢視中。無需在不同管理介面之間切換。",
        },
        {
          title: "即時監控",
          description:
            "線上/離線狀態、使用量圖表和活動熱力圖。隨時瞭解你的算力在做什麼。",
        },
        {
          title: "自動檢測與即插即用",
          description:
            "Multica 自動檢測 Claude Code、Codex、OpenClaw 和 OpenCode 等可用 CLI。連線一臺機器，即可開始工作。",
        },
      ],
    },
  },

  howItWorks: {
    label: "開始使用",
    headlineMain: "招募你的第一個 AI 員工",
    headlineFaded: "只需一小時。",
    steps: [
      {
        title: allowSignup ? "註冊並建立您的工作空間" : "登入到您的工作空間",
        description: allowSignup
          ? "輸入您的郵箱，驗證程式碼後即可使用。工作空間會自動建立——無需設定嚮導或配置表單。"
          : "輸入您的郵箱，驗證程式碼後即可登入到您的工作空間——無需設定嚮導或配置表單。",
      },
      {
        title: "安裝 CLI 並連線你的機器",
        description:
          "執行 multica setup 一鍵完成配置、認證和啟動。守護程序自動檢測你機器上的 Claude Code、Codex、OpenClaw 和 OpenCode——插上就用。",
      },
      {
        title: "建立你的第一個 Agent",
        description:
          "給它起個名字，寫好指令，附加技能，設定觸發器。選擇它何時啟用：被指派時、有評論時、被 @提及時。",
      },
      {
        title: "指派一個 Issue 並觀察它工作",
        description:
          "從指派人下拉選單中選擇你的 Agent——就像指派給同事一樣。任務自動入隊、領取、執行。即時觀看進度。",
      },
    ],
    cta: "開始使用",
    ctaGithub: "在 GitHub 上檢視",
    ctaDocs: "閱讀文件",
  },

  openSource: {
    label: "開源",
    headlineLine1: "開源",
    headlineLine2: "為所有人。",
    description:
      "Multica 完全開源。審查每一行程式碼，按你的方式自託管，塑造人類 + Agent 協作的未來。",
    cta: "在 GitHub 上 Star",
    highlights: [
      {
        title: "隨處自託管",
        description:
          "在你自己的基礎設施上執行 Multica。Docker Compose、單個二進位制或 Kubernetes——你的資料永遠不會離開你的網路。",
      },
      {
        title: "無供應商鎖定",
        description:
          "自帶 LLM 提供商、更換 Agent 後端、擴充套件 API。你擁有整個技術棧的控制權。",
      },
      {
        title: "預設透明",
        description:
          "每一行程式碼都可審計。確切瞭解你的 Agent 如何做決策、任務如何路由、資料流向何方。",
      },
      {
        title: "社群驅動",
        description:
          "與社群一起建設，而不僅僅是為社群建設。貢獻技能、整合和 Agent 後端，讓每個人受益。",
      },
    ],
  },

  faq: {
    label: "常見問題",
    headline: "問與答。",
    items: [
      {
        question: "Multica 支援哪些編碼 Agent？",
        answer:
          "Multica 目前開箱即用支援 Claude Code、Codex、OpenClaw 和 OpenCode。守護程序自動檢測你安裝的 CLI。因為開源，你也可以自己新增後端。",
      },
      {
        question: "需要自託管嗎，還是有云版本？",
        answer:
          "兩者都有。你可以用 Docker Compose 或 Kubernetes 在自己的基礎設施上自託管 Multica，也可以使用我們的託管雲版本。你的資料，你選擇。",
      },
      {
        question:
          "這和直接用編碼 Agent 有什麼區別？",
        answer:
          "編碼 Agent 擅長執行。Multica 新增的是管理層：任務佇列、團隊協作、技能複用、執行時監控，以及每個 Agent 在做什麼的統一檢視。把它想象成你的 Agent 的專案經理。",
      },
      {
        question: "Agent 能自主處理長時間任務嗎？",
        answer:
          "可以。Multica 管理完整的任務生命週期——入隊、領取、執行、完成或失敗。Agent 主動報告阻塞並即時推送進度。你可以隨時檢視，也可以讓它們執行整晚。",
      },
      {
        question: "我的程式碼安全嗎？Agent 在哪裡執行？",
        answer:
          "Agent 在你的機器（本地守護程序）或你自己的雲基礎設施上執行。程式碼永遠不會經過 Multica 伺服器。平臺只協調任務狀態和廣播事件。",
      },
      {
        question: "我可以執行多少個 Agent？",
        answer:
          "取決於你的硬體。每個 Agent 有可配置的併發限制，你可以連線多臺機器作為執行時。開源版本沒有任何人為限制。",
      },
    ],
  },

  footer: {
    tagline:
      "人類 + Agent 團隊的專案管理。開源、可自託管、為未來的工作方式而建。",
    cta: "開始使用",
    groups: {
      product: {
        label: "產品",
        links: [
          { label: "功能特性", href: "#features" },
          { label: "如何工作", href: "#how-it-works" },
          { label: "更新日誌", href: "/changelog" },
          { label: "下載", href: "/download" },
        ],
      },
      resources: {
        label: "資源",
        links: [
          { label: "文件", href: "/docs/zh" },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
        ],
      },
      company: {
        label: "關於",
        links: [
          { label: "關於我們", href: "/about" },
          { label: "開源", href: "#open-source" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "© {year} Multica. 保留所有權利。",
  },

  about: {
    title: "關於 Multica",
    nameLine: {
      prefix: "Multica——",
      mul: "Mul",
      tiplexed: "tiplexed ",
      i: "I",
      nformationAnd: "nformation and ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent。",
    },
    paragraphs: [
      "這個名字是在向 20 世紀 60 年代具有開創意義的作業系統 Multics 致意。Multics 首創了分時系統，讓多個使用者能夠共享同一臺機器，同時又像各自獨佔它一樣使用。Unix 則是在有意簡化 Multics 的基礎上誕生的，強調一個使用者、一個任務、一種優雅的哲學。",
      "我們認為，類似的轉折點正在再次出現。幾十年來，軟體團隊一直處於一種單執行緒的工作模式，一個工程師處理一個任務，一次只專注於一個上下文。AI agents 改變了這個等式。Multica 將“分時”重新帶回這個時代，只不過今天在系統中進行多路複用的“使用者”，既包括人類，也包括自主代理。",
      "在 Multica 中，agents 是一級團隊成員。它們會被分配 issue，彙報進展，提出阻塞，並交付程式碼，就像人類同事一樣。任務分配、活動時間線、任務生命週期，以及執行時基礎設施，Multica 從第一天起就是圍繞這一理念構建的。",
      "和當年的 Multics 一樣，這一判斷建立在“多路複用”之上。一個小團隊不該因為人數少就顯得能力有限。有了合適的系統，兩名工程師加上一組 agents，就能發揮出二十人團隊的推進速度。",
      "這個平臺是完全開源並支援自託管的。你的資料始終保留在自己的基礎設施中。你可以審查每一行程式碼，擴充套件 API，接入自己的 LLM providers，也可以向社群貢獻程式碼。",
    ],
    cta: "在 GitHub 上檢視",
  },

  changelog: {
    title: "更新日誌",
    subtitle: "Multica 的最新更新和改進。",
    toc: "歷史版本",
    categories: {
      features: "新功能",
      improvements: "改進",
      fixes: "問題修復",
    },
    entries: [
      {
        version: "0.2.18",
        date: "2026-04-27",
        title: "Issue 標籤、Labs 設定頁與邀請紅點",
        changes: [],
        features: [
          "Issue 標籤——給 Issue 上色、分類，列表、看板和詳情頁都能用",
          "新增 Labs 設定頁，集中放實驗性開關",
          "有未讀工作區邀請時，側邊欄會出現紅點提示",
        ],
        improvements: [
          "Project 選擇器會顯示當前所選 Project 的圖示",
          "進入詳情頁時，側邊欄父級選單保持高亮",
          "自託管部署正確讀取註冊放行相關的環境變數",
        ],
        fixes: [
          "Agent 評論的換行恢復正常顯示",
          "桌面端 RPM 不再與 Slack / VS Code 在 Fedora 上衝突",
          "Windows 下 Agent 能正確處理多行 prompt",
        ],
      },
      {
        version: "0.2.17",
        date: "2026-04-26",
        title: "Agent 自定義環境變數、更清晰的失敗資訊與一系列穩定性修復",
        changes: [],
        features: [
          "`multica agent create/update --custom-env KEY=VALUE` 支援為 Agent 注入自定義環境變數",
          "Agent 失敗資訊會帶上 Runtime CLI 的 stderr 末尾片段，排查 Runtime 報錯更直接",
          "CLI 更新下載超時支援配置，弱網下 `multica update` 不再被預設超時切斷",
        ],
        improvements: [
          "Daemon 把取消的任務上報為 `cancelled` 而非 `timeout`，並在按 Issue 取消任務時同步對齊 Agent 狀態",
          "Server 心跳拆成 probe/claim 兩步，並補上慢日誌和 model-list running-timeout，丟心跳不再卡住 UI",
        ],
        fixes: [
          "Server 在 Issue 建立/更新時校驗 `assignee_id` 真實存在；DeleteIssue 改用解析後的 Issue ID",
          "Pi Runtime 改為讀寫 `.pi/skills`，不再使用舊的 `.pi/agent/skills` 路徑",
          "Windows 下 Daemon 啟動 Agent 改用 `CREATE_NEW_CONSOLE`，孫子程序不再彈出額外終端視窗",
          "Autopilot 的 run-only 上下文正確傳給被調起的 Agent",
        ],
      },
      {
        version: "0.2.16",
        date: "2026-04-24",
        title: "Chat V2、Issue 右鍵選單與應用內反饋",
        changes: [],
        features: [
          "Chat V2——側邊欄新增 Chat 入口，主區域提供完整的 AI 對話頁面",
          "Issue 支援右鍵選單，列表、看板和詳情的操作入口統一收斂",
          "應用內反饋流程及全新的 Help 啟動器，集中託管文件、支援和反饋入口",
          "Autopilot 彈窗重設計——更簡的欄位配置，建立與編輯共享一致的排期介面",
          "Skills 頁面重設計——列表+詳情、卡片化佈局、滾動漸隱和共享 PageHeader / 移動端導航",
          "文件站重寫為雙語扁平內容樹——中英文章節共用一棵目錄",
        ],
        improvements: [
          "懸停 Agent 頭像即可彈出資料卡，快速瞭解上下文",
          "桌面應用新增原生右鍵選單，支援複製 / 貼上 / 剪下 / 全選等剪貼簿操作",
          "Daemon 強化 Agent 提示，避免 Agent 之間形成自互 @ 的迴圈",
          "Server 新增就緒態健康檢查端點，可對接灰度釋出和 Ingress 探針",
          "Daemon GC 預設引數收緊，並支援靈活的時長字尾（如 `7d`、`12h`）",
          "移除 Runtime 的 Test Connection / Ping 功能，可達性改為自動檢測",
        ],
        fixes: [
          "Chat 流式回覆結束時不再閃爍，傳送第一條訊息時輸入框不再跳動",
          "桌面應用啟動時正確恢復上次的工作區，而不是預設回到第一個",
          "編輯器只讀渲染路徑正確保留巢狀有序列表",
          "CLI `browser-login` 現在可以從未執行 Server 的機器上發起",
          "Windows 下 Daemon 啟動 Agent 不再拉起額外終端視窗；本地 Skill 上報在服務端瞬時錯誤時會自動重試",
          "`/api/config` 重新對未登入客戶端可達，方便初次 bootstrap",
          "DeleteWorkspace 增加防禦性 owner 校驗；`/health/realtime` 指標限定授權訪問（安全）",
          "Hermes ACP Runtime 正確傳遞配置的模型；OpenClaw Agent 發現超時提高到 30s",
        ],
      },
      {
        version: "0.2.15",
        date: "2026-04-22",
        title: "本地 Skills、LaTeX、Focus 模式與孤兒任務自恢復",
        changes: [],
        features: [
          "支援將 Runtime 本地 Skills 匯入工作區,成為一等工作區資產",
          "孤兒任務自動恢復——意外中斷的 Agent 執行會自動重試,必要時可手動重跑",
          "Issue、評論與 Chat 支援 LaTeX 渲染",
          "Chat Focus 模式——將當前頁面作為上下文分享給對話",
        ],
        improvements: [
          "子 Issue 的 `status_changed` 事件不再向父 Issue 訂閱者刷屏",
          "Docker 釋出映象改為按架構原生構建,免 QEMU",
          "側邊欄 Pin 欄位在客戶端派生,排序更跟手",
          "擴充保留 slug 列表,新工作區 slug 不會再和產品路由衝突",
        ],
        fixes: [
          "Gemini Runtime 模型列表補上 Gemini 3 及若干 CLI 別名",
          "沒有錨點的頁面上 Chat focus 按鈕改為停用",
          "修復 Onboarding 中 Pin 同步、歡迎頁佈局與 Runtime bootstrap 狀態",
          "`install.ps1` 的系統架構探測更穩健,覆蓋更多 Windows 環境",
          "`/download` 在 1 小時新鮮度視窗內可回退到上一版本,避免撞上半釋出狀態",
        ],
      },
      {
        version: "0.2.11",
        date: "2026-04-21",
        title: "桌面應用跨平臺打包、CLI 自更新與看板分頁",
        changes: [],
        features: [
          "桌面應用跨平臺打包——同一條釋出流水線產出 macOS、Windows 和 Linux 安裝包",
          "新增 `multica update` 自更新命令——無需重灌即可升級 CLI 和本地 Daemon",
          "Issue 看板所有狀態列都支援分頁（不再只是 Done 列），大積壓下依然流暢",
        ],
        fixes: [
          "本地 Daemon 對 Agent 執行強制端到端工作區隔離（安全）",
          "Windows 下 Daemon 終端關閉後繼續常駐，後臺 Agent 不再被意外終止",
          "看板卡片重新顯示描述預覽——列表查詢不再丟掉 description 欄位",
          "OpenClaw Agent 改為從 Agent 後設資料讀取真實模型，不再回退到預設值",
          "評論 Markdown 全鏈路保留——移除會誤傷格式的 HTML sanitizer",
        ],
      },
      {
        version: "0.2.8",
        date: "2026-04-20",
        title: "Agent 模型選擇、Kimi Runtime 與自部署登入",
        changes: [],
        features: [
          "Agent 新增 `model` 欄位及按 Provider 聚合的模型下拉框——可在介面或透過 `multica agent create/update --model` 為每個 Agent 選擇 LLM 模型，並從各 Runtime CLI 即時發現可用模型",
          "新增 Kimi CLI Agent Runtime（Moonshot AI 的 `kimi-cli`，基於 ACP），支援模型選擇、自動授權工具許可權以及流式工具呼叫渲染",
          "評論和回覆編輯器新增放大按鈕，便於撰寫長文本",
        ],
        fixes: [
          "Agent 工作流將“釋出結果評論”提升為獨立的顯式步驟，確保最終回覆送達 Issue 而不是隻留在終端輸出",
          "透過 Cmd+K 切換 Issue 時不再出現其他 Issue 的 Agent 即時狀態殘留",
          "自部署會話 Cookie 的 Secure 標誌改由 `FRONTEND_ORIGIN` 協議決定——HTTP 部署不再因瀏覽器丟棄 Cookie 導致登入失敗；`COOKIE_DOMAIN=<ip>` 會自動回退到 host-only 並輸出警告",
        ],
      },
      {
        version: "0.2.7",
        date: "2026-04-18",
        title: "編輯器建立子 Issue、自部署門禁與 MCP",
        changes: [],
        features: [
          "直接從編輯器氣泡選單將選中文本建立為子 Issue",
          "自部署例項賬戶門禁——`ALLOW_SIGNUP` 和 `ALLOWED_EMAIL_*` 環境變數限制註冊",
          "Agent 新增 `mcp_config` 欄位恢復 MCP 支援",
          "桌面應用每小時檢查更新，設定中新增手動檢查按鈕",
        ],
        fixes: [
          "網頁已登入時將會話交接給桌面應用",
          "修復 `?next=` 開放重定向漏洞",
          "OpenClaw 停止傳遞不支援的引數，正確傳遞 AgentInstructions",
        ],
      },
      {
        version: "0.2.5",
        date: "2026-04-17",
        title: "CLI Autopilot、Cmd+K 與 Daemon 身份",
        changes: [],
        features: [
          "CLI `autopilot` 命令，管理定時和觸發式自動化",
          "CLI `issue subscriber` 訂閱管理命令",
          "Cmd+K 命令面板擴充套件——主題切換、快速建立 Issue/專案、複製連結、切換工作區",
          "Issue 列表卡片可選顯示專案和子 Issue 進度",
          "Daemon 持久化 UUID 身份——CLI 和桌面應用共用同一個 daemon，跨重啟和機器遷移保持一致",
          "唯一所有者退出工作區的前置檢查",
          "評論摺疊狀態跨會話持久化",
        ],
        fixes: [
          "Agent 現在在任意 Issue 狀態下都會響應評論觸發",
          "修復 Codex 沙箱在 macOS 上的網路訪問配置",
          "編輯器氣泡選單改用 @floating-ui/dom 重寫，滾動時正確隱藏",
          "Autopilot 建立者自動訂閱其生成的 Issue",
          "Autopilot run-only 任務正確解析工作區 ID",
          "桌面應用 `shell.openExternal` 限制僅允許 http/https 協議（安全）",
          "重名 Agent 建立返回 409 而非靜默失敗",
          "桌面應用新建標籤頁繼承當前工作區",
        ],
      },
      {
        version: "0.2.1",
        date: "2026-04-16",
        title: "新增 Agent 執行時",
        changes: [],
        features: [
          "支援 GitHub Copilot CLI 執行時",
          "支援 Cursor Agent CLI 執行時",
          "支援 Pi Agent 執行時",
          "工作區 URL 改造——slug 優先路由（`/{slug}/issues`），舊連結自動重定向",
        ],
        fixes: [
          "Codex 同一 Issue 下跨任務恢復會話執行緒",
          "Codex 回合錯誤正確丟擲，不再報告空輸出",
          "工作區用量按任務完成時間正確分桶",
          "Autopilot 執行歷史行整行可點選",
          "Daemon 和 GC 端點加強工作區隔離校驗（安全）",
          "邀請郵件中的工作區和邀請人名稱進行 HTML 轉義",
          "桌面應用開發版和生產版現在可以同時執行",
        ],
      },
      {
        version: "0.2.0",
        date: "2026-04-15",
        title: "桌面應用、Autopilot 與邀請",
        changes: [],
        features: [
          "macOS 桌面應用——原生 Electron 應用，支援標籤頁系統、內建 Daemon 管理、沉浸模式和自動更新",
          "Autopilot——Agent 定時和觸發式自動化任務",
          "工作區邀請，支援郵件通知和專用接受頁面",
          "Agent 自定義 CLI 引數，支援高階執行時配置",
          "聊天介面重設計，新增未讀追蹤和會話管理最佳化",
          "建立 Agent 對話方塊顯示執行時所有者和 Mine/All 篩選",
        ],
        improvements: [
          "Inter 字型 + CJK 回退，中英文自動間距",
          "側邊欄使用者選單改為整行彈出面板",
          "WebSocket ping/pong 心跳檢測斷線連線",
          "普通成員現在可以建立 Agent 和管理自己的 Skills",
        ],
        fixes: [
          "Agent 在已參與的執行緒收到回覆時正確觸發",
          "自部署：Docker 本地上傳檔案持久化，WebSocket URL 自動適配區域網",
          "Cmd+K 最近 Issue 列表狀態過期",
        ],
      },
      {
        version: "0.1.33",
        date: "2026-04-14",
        title: "Gemini CLI 與 Agent 環境變數",
        changes: [],
        features: [
          "Google Gemini CLI 作為新的 Agent 執行時，支援即時日誌流",
          "Agent 自定義環境變數（router/proxy 模式），新增專用設定標籤頁",
          "Issue 右鍵選單新增「設定父 Issue」和「新增子 Issue」",
          "CLI `--parent` 更新父 Issue，`--content-stdin` 管道輸入評論內容",
          "子 Issue 自動繼承父級專案",
        ],
        improvements: [
          "編輯器氣泡選單和連結預覽重寫",
          "OpenClaw 後端 P0+P1 最佳化（多行 JSON、增量解析）",
          "自部署 WebSocket URL 自動適配區域網訪問",
        ],
        fixes: [
          "S3 上傳路徑按工作區隔離（安全）",
          "訂閱和上傳新增工作區成員身份校驗（安全）",
          "Issue 狀態改為已取消時自動終止進行中的任務",
          "Agent 程序 stdout 掛起導致任務卡住",
          "Daemon 觸發提示現在嵌入實際的觸發評論內容",
          "登入和儀表盤跳轉穩定性改進",
        ],
      },
      {
        version: "0.1.28",
        date: "2026-04-13",
        title: "Windows 支援、認證與引導",
        changes: [],
        features: [
          "Windows 支援——CLI 安裝、Daemon 執行和釋出構建",
          "認證遷移至 HttpOnly Cookie，WebSocket 新增 Origin 白名單",
          "新工作區全屏引導向導",
          "Master Agent 聊天視窗可調整大小，會話歷史體驗最佳化",
          "OpenCode、OpenClaw 和 Hermes 執行時 Token 用量日誌掃描",
        ],
        fixes: [
          "WebSocket 首條訊息認證安全修復",
          "新增 Content-Security-Policy 響應頭",
          "子 Issue 進度改為從資料庫計算而非分頁客戶端快取",
        ],
      },
      {
        version: "0.1.27",
        date: "2026-04-12",
        title: "一鍵安裝、自部署與穩定性",
        changes: [],
        features: [
          "一鍵安裝與配置——`curl | bash` 安裝 CLI，`--with-server` 完整自部署，`multica setup` 配置連線環境",
          "自部署儲存——無 S3 時本地檔案儲存回退，支援自定義 S3 端點（MinIO）",
          "專案列表頁支援行內編輯屬性（優先順序、狀態、負責人）",
        ],
        improvements: [
          "過期 Agent 任務自動清掃；執行卡片立即顯示，無需等待首條訊息",
          "透過 CLI 上傳的評論附件現在可在 UI 中顯示",
          "置頂項按使用者隔離，修復側邊欄置頂操作",
        ],
        fixes: [
          "Daemon API 路由和附件上傳新增工作區所有權校驗",
          "Markdown 清洗器保留程式碼塊不被 HTML 實體轉義",
          "Next.js 升級至 ^16.2.3 修復 CVE-2026-23869",
          "OpenClaw 後端重寫以匹配實際 CLI 介面",
        ],
      },
      {
        version: "0.1.24",
        date: "2026-04-11",
        title: "安全加固與通知",
        changes: [],
        features: [
          "子 Issue 變更時通知父 Issue 的訂閱者",
          "CLI `--project` 篩選 Issue 列表",
        ],
        improvements: [
          "Meta-skill 工作流改為委託 Agent Skills 而非硬編碼邏輯",
        ],
        fixes: [
          "Daemon API 路由新增工作區所有權校驗",
          "附件上傳和查詢新增工作區所有權驗證",
          "回覆評論不再繼承父級執行緒的 Agent 提及",
          "Agent 建立評論缺少 workspace ID",
          "自部署 Docker 構建問題修復（檔案許可權、CRLF 換行、缺失依賴）",
        ],
      },
      {
        version: "0.1.23",
        date: "2026-04-11",
        title: "置頂、Cmd+K 與專案增強",
        changes: [],
        features: [
          "Issue 和專案置頂到側邊欄，支援拖拽排序",
          "Cmd+K 命令面板——最近訪問的 Issue、頁面導航、專案搜尋",
          "專案詳情側邊欄屬性面板（替代原概覽標籤頁）",
          "Issues 列表新增專案篩選",
          "專案列表顯示完成進度",
          "在專案頁按 'C' 建立 Issue 時自動填充專案",
          "指派人下拉按使用者分配頻率排序",
        ],
        fixes: [
          "Markdown XSS 漏洞——評論渲染增加 rehype-sanitize 和服務端 bluemonday 清洗",
          "專案看板 Issue 計數不正確",
          "自部署 Docker 構建缺少 tsconfig 依賴",
          "Cmd+K 需要按兩次 ESC 才能關閉",
        ],
      },
      {
        version: "0.1.22",
        date: "2026-04-10",
        title: "自部署、ACP 與文件站",
        changes: [],
        features: [
          "全棧 Docker Compose 一鍵自部署",
          "透過 ACP 協議接入 Hermes Agent Provider",
          "基於 Fumadocs 搭建文件站（快速入門、CLI 參考、Agent 指南）",
          "側邊欄和收件箱移動端響應式佈局",
          "Issue 詳情側邊欄展示 Token 用量",
          "支援在 UI 中切換 Agent 執行時",
          "'C' 快捷鍵快速建立 Issue",
          "聊天會話歷史面板，檢視已歸檔對話",
          "Daemon 新增 Claude Code 和 Codex 最低版本檢查",
          "官網新增 OpenClaw 和 OpenCode 展示",
          "`make dev` 一鍵本地開發環境搭建",
        ],
        improvements: [
          "側邊欄重新設計——個人/工作區分組、使用者檔案底欄、⌘K 搜尋入口",
          "搜尋排序最佳化——大小寫無關匹配、識別符號搜尋（MUL-123）、多詞匹配",
          "搜尋結果關鍵詞高亮",
          "每日 Token 用量圖表最佳化，Y 軸標籤更清晰，新增分類 Tooltip",
          "Master Agent 支援多行輸入",
          "統一選擇器元件（狀態、優先順序、截止日期、專案、指派人）",
          "工作區級別儲存隔離，切換工作區時自動載入對應資料",
          "自部署環境變數缺失時給出啟動警告",
        ],
        fixes: [
          "刪除子 Issue 後父級列表未重新整理",
          "搜尋索引相容 RDS 上的 pg_bigm 1.2",
          "建立 Agent 對話方塊錯誤顯示「無可用執行時」",
          "Claude stream-json 啟動卡住",
          "多個 Agent 無法同時為同一 Issue 排隊任務",
          "退出登入未清除工作區和查詢快取",
          "編輯器為空時拖放區域過小",
          "Skills 匯入硬編碼 main 分支導致 404",
          "WebSocket 端點不支援 PAT 認證",
          "所有 Agent 已歸檔時無法刪除執行時",
        ],
      },
      {
        version: "0.1.21",
        date: "2026-04-09",
        title: "專案、搜尋與 Monorepo",
        changes: [
          "專案實體全棧 CRUD——建立、編輯專案並按專案組織 Issue",
          "建立 Issue 彈窗新增專案選擇器，CLI 新增專案命令",
          "基於 pg_bigm 的 Issue 全文搜尋",
          "Monorepo 拆包——共享 core、UI、views 三個包（Turborepo）",
          "全屏 Agent 執行日誌檢視",
          "編輯器支援拖拽上傳檔案並展示檔案卡片",
          "Issue 新增附件區域，支援圖片網格和檔案卡片展示",
          "執行時支援所有者追蹤、篩選、頭像展示和點對點更新通知",
          "列表檢視行內顯示子 Issue 進度",
          "列表檢視支援已完成 Issue 分頁載入",
          "Codex 會話日誌掃描以報告 token 用量",
          "修復守護程序 repo 快取卡在初始快照的問題",
        ],
      },
      {
        version: "0.1.20",
        date: "2026-04-08",
        title: "子 Issue、TanStack Query 與用量追蹤",
        changes: [
          "子 Issue 支援——在任意 Issue 內建立、檢視和管理子任務",
          "全面遷移至 TanStack Query 管理服務端狀態（Issue、收件箱、工作區、執行時）",
          "按任務維度追蹤所有 Agent 提供商的 token 用量",
          "同一 Issue 支援多個 Agent 併發執行",
          "看板檢視：Done 列顯示總數並支援無限滾動",
          "新增 ReadonlyContent 元件，輕量渲染評論中的 Markdown",
          "表情反應和變更操作支援樂觀更新與回滾",
          "WebSocket 驅動快取失效，替代輪詢和焦點重新整理",
          "CLI 登入流程中瀏覽器會話保持不丟失",
          "守護程序複用已有 worktree 時自動拉取最新遠端程式碼",
          "修復動態根佈局導致的標籤頁切換卡頓問題",
        ],
      },
      {
        version: "0.1.18",
        date: "2026-04-07",
        title: "OAuth、OpenClaw 與 Issue 載入最佳化",
        changes: [
          "支援 Google OAuth 登入",
          "新增 OpenClaw 執行時，支援在 OpenClaw 基礎設施上執行 Agent",
          "Agent 即時卡片重新設計——始終吸頂，支援手動展開/收起",
          "開啟的 Issue 不再分頁限制全量載入，已關閉的 Issue 滾動分頁",
          "JWT 和 CloudFront Cookie 有效期從 72 小時延長至 30 天",
          "重新登入後記住上次選擇的工作區",
          "守護程序確保 Agent 任務環境中 multica CLI 在 PATH 上",
          "新增 PR 模板和麵向 Agent 的 CLI 安裝指南",
        ],
      },
      {
        version: "0.1.17",
        date: "2026-04-05",
        title: "評論分頁與 CLI 最佳化",
        changes: [
          "評論列表支援分頁，API 和 CLI 均已適配",
          "收件箱歸檔操作現在一次性歸檔同一 Issue 的所有通知",
          "CLI 幫助輸出重新設計，匹配 gh CLI 風格並增加示例",
          "附件使用 UUIDv7 作為 S3 key，建立 Issue/評論時自動關聯附件",
          "支援在已完成或已取消的 Issue 上 @提及已分配的 Agent",
          "回覆僅 @提及成員時跳過父級提及繼承邏輯",
          "Worktree 環境配置保留已有的 .env.worktree 變數",
        ],
      },
      {
        version: "0.1.15",
        date: "2026-04-03",
        title: "編輯器重構與 Agent 生命週期",
        changes: [
          "統一 Tiptap 編輯器，編輯和展示共用單一 Markdown 渲染管線",
          "Markdown 貼上、行內程式碼間距和連結樣式修復",
          "Agent 支援歸檔和恢復——軟刪除替代硬刪除",
          "預設列表隱藏已歸檔的 Agent",
          "全應用新增骨架屏載入態、錯誤提示和確認對話方塊",
          "新增 OpenCode 作為支援的 Agent 提供商",
          "回覆觸發的 Agent 任務自動繼承主執行緒 @提及",
          "Issue 和收件箱即時事件細粒度處理，不再全量重新整理",
          "編輯器中統一圖片上傳流程，支援貼上和按鈕上傳",
        ],
      },
      {
        version: "0.1.14",
        date: "2026-04-02",
        title: "提及與許可權",
        changes: [
          "評論中支援 @提及 Issue，服務端自動展開",
          "支援 @all 提及工作區所有成員",
          "收件箱通知點選後自動滾動到對應評論",
          "倉庫管理獨立為設定頁單獨標籤頁",
          "支援從網頁端執行時頁面更新 CLI，非 Homebrew 安裝支援直接下載更新",
          "新增 CLI 命令檢視 Issue 執行記錄和執行訊息",
          "Agent 許可權模型最佳化——所有者和管理員管理 Agent，成員可管理自己 Agent 的技能",
          "每個 Issue 序列執行，防止併發任務衝突",
          "檔案上傳支援所有檔案型別",
          "README 重新設計，新增快速入門指南",
        ],
      },
      {
        version: "0.1.13",
        date: "2026-04-01",
        title: "我的 Issue 與國際化",
        changes: [
          "我的 Issue 頁面，支援看板、列表檢視和範圍標籤",
          "落地頁新增簡體中文本地化",
          "新增關於頁面和更新日誌頁面",
          "Agent 設定頁支援頭像上傳",
          "CLI 評論和 Issue/評論 API 的附件支援",
          "統一頭像渲染，所有選擇器使用 ActorAvatar 元件",
          "落地頁 SEO 最佳化和登入流程改進",
          "CLI 預設使用生產環境 API 地址",
          "許可證變更為 Apache 2.0",
        ],
      },
      {
        version: "0.1.3",
        date: "2026-03-31",
        title: "Agent 智慧",
        changes: [
          "透過評論中的 @提及觸發 Agent",
          "將 Agent 即時輸出推送到 Issue 詳情頁",
          "富文本編輯器——提及、連結貼上、表情反應、可摺疊執行緒",
          "檔案上傳，支援 S3 + CloudFront 簽名 URL 和附件跟蹤",
          "Agent 驅動的程式碼倉庫檢出，帶 bare clone 快取的任務隔離",
          "Issue 列表檢視的批次操作",
          "守護程序身份認證和安全加固",
        ],
      },
      {
        version: "0.1.2",
        date: "2026-03-28",
        title: "協作",
        changes: [
          "郵箱驗證登入和基於瀏覽器的 CLI 認證",
          "多工作區守護程序，支援熱過載",
          "執行時儀表板，含使用量圖表和活動熱力圖",
          "基於訂閱者的通知模型，替代硬編碼觸發器",
          "統一的活動時間線，支援評論執行緒回覆",
          "看板重新設計，支援拖拽排序、篩選和顯示設定",
          "人類可讀的 Issue 識別符號（如 JIA-1）",
          "從 ClawHub 和 Skills.sh 匯入技能",
        ],
      },
      {
        version: "0.1.1",
        date: "2026-03-25",
        title: "核心平臺",
        changes: [
          "多工作區切換和建立",
          "Agent 管理 UI，支援技能、工具和觸發器",
          "統一的 Agent SDK，支援 Claude Code 和 Codex 後端",
          "評論 CRUD，支援即時 WebSocket 更新",
          "任務服務層和守護程序 REST 協議",
          "事件匯流排，支援工作區級別的 WebSocket 隔離",
          "收件箱通知，支援未讀徽章和歸檔",
          "CLI 支援 cobra 子命令，用於工作區和 Issue 管理",
        ],
      },
      {
        version: "0.1.0",
        date: "2026-03-22",
        title: "基礎架構",
        changes: [
          "Go 後端，支援 REST API、JWT 認證和即時 WebSocket",
          "Next.js 前端，Linear 風格 UI",
          "Issue 支援看板和列表檢視，含拖拽看板",
          "Agent、收件箱和設定頁面",
          "一鍵設定、遷移 CLI 和種子工具",
          "全面測試套件——Go 單元/整合測試、Vitest、Playwright E2E",
        ],
      },
    ],
  },
  download: {
    hero: {
      macArm64: {
        title: "Multica for macOS",
        sub: "Apple Silicon · 內建 daemon，無需配置",
        primary: "下載 (.dmg)",
        altZip: "或下載 .zip",
      },
      macIntel: {
        title: "Multica for macOS",
        sub: "需要 Apple Silicon——暫不支援 Intel Mac。",
        disabledCta: "需要 Apple Silicon",
        intelHint: "在 Intel Mac 上？請使用下方 CLI——底層跑的是同一個 daemon。",
      },
      winX64: {
        title: "Multica for Windows",
        sub: "內建 daemon，無需配置",
        primary: "下載 (.exe)",
      },
      winArm64: {
        title: "Multica for Windows",
        sub: "ARM · 內建 daemon，無需配置",
        primary: "下載 (.exe)",
      },
      linux: {
        title: "Multica for Linux",
        sub: "內建 daemon，無需配置",
        primary: "下載 AppImage",
        altFormats: "或 .deb / .rpm",
      },
      unknown: {
        title: "選擇你的平臺",
        sub: "下方是所有支援的安裝包。",
      },
      safariMacHint: "在 Intel Mac 上？請使用下方 CLI。",
      archFallbackHint: "架構不對？下方是所有可選格式。",
    },
    allPlatforms: {
      title: "所有平臺",
      macLabel: "macOS · Apple Silicon",
      winX64Label: "Windows · x64",
      winArm64Label: "Windows · ARM64",
      linuxX64Label: "Linux · x64",
      linuxArm64Label: "Linux · ARM64",
      formatDmg: ".dmg",
      formatZip: ".zip",
      formatExe: ".exe",
      formatAppImage: ".AppImage",
      formatDeb: ".deb",
      formatRpm: ".rpm",
      intelNote: "僅支援 Apple Silicon——Intel Mac 目前暫不支援。",
      unavailable: "暫不可用",
    },
    cli: {
      title: "想用 CLI？",
      sub: "適合伺服器、遠端開發機、無圖形介面環境。底層 daemon 與 Desktop 相同，透過終端安裝。",
      installLabel: "安裝",
      startLabel: "啟動 daemon",
      sshNote: "已經在伺服器上？透過 SSH 執行同樣的命令即可。",
      copyLabel: "複製",
      copiedLabel: "已複製",
    },
    cloud: {
      title: "Cloud runtime（等待名單）",
      sub: "我們將為你託管 runtime，目前尚未上線——留下郵箱，上線後通知你。",
    },
    footer: {
      releaseNotes: "v{version} 更新內容",
      allReleases: "檢視所有版本",
      currentVersion: "當前版本：{version}",
      versionUnavailable: "版本獲取失敗——請前往 GitHub 檢視",
    },
  },
  };
}
