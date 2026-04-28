import type { OnboardingDict } from "./types";

export function createZhTwDict(): OnboardingDict {
  return {
    common: {
      back: "上一步",
      next: "下一步",
      skip: "略過",
      skipForNow: "暫時略過",
      continue: "繼續",
      cancel: "取消",
      close: "關閉",
      done: "完成",
      retry: "重試",
      finishOnboardingFailed: "完成設定流程失敗",
    },
    stepHeader: {
      stepOf: (current, total) => `第 ${current} 步，共 ${total} 步`,
    },
    welcome: {
      brand: "歡迎使用 Multica",
      headline1: "你的 AI 隊友，",
      headline2Prefix: "全在",
      headline2Emphasis: "同一個工作區。",
      headline2Suffix: "",
      leadParagraph:
        "像指派同事一樣指派工作給他們 — 他們會接手任務、更新狀態，完成後留言回報。",
      secondaryWeb:
        "桌面版內建 runtime — 不需安裝。在網頁版繼續則可連接你自己的 CLI。",
      secondaryDesktop:
        "完成設定後，會有真正的 Agent 回覆你的第一個 Issue。",
      download: "下載桌面版",
      continueOnWeb: "在網頁版繼續",
      startExploring: "開始探索",
      skipExisting: "我做過了，跳過",
      illustrationCaption:
        "每個 Issue、每段討論、每個決定 — 都由你的團隊與 Agent 共同分享。",
    },
    questionnaire: {
      eyebrow: "在開始之前",
      title: "三個問題，讓我們認識你。",
      answeredCounter: (n, total) => `已回答 ${n} / ${total}`,
      continue: "繼續",
      questions: {
        teamSize: "誰會使用這個工作區？",
        role: "哪個最能描述你？",
        useCase: "你想用 Multica 做什麼？",
      },
      options: {
        teamSize: {
          solo: "只有我",
          team: "我的團隊（2–10 人）",
          otherPlaceholder: "例如：我協助營運的小型社群",
        },
        role: {
          developer: "軟體開發者",
          productLead: "產品或專案負責人",
          writer: "寫作者或內容創作者",
          founder: "創辦人或營運者",
          otherPlaceholder: "例如：研究員、設計師、營運主管",
        },
        useCase: {
          coding: "撰寫並交付程式碼",
          planning: "規劃並管理專案",
          writingResearch: "研究或寫作",
          explore: "我只是先來看看",
          otherPlaceholder: "例如：自動化我的週報",
        },
      },
      sidePanel: {
        whyEyebrow: "為什麼問三個問題",
        whyHeadline: "讓你一上手就能順利開跑。",
        whatGetEyebrow: "你會得到什麼",
        starterTitle: "為你量身打造的入門專案",
        starterBody: "依你的回答打造的 Getting Started 檢查清單。",
        headStartTitle: "Agent 的快速起步",
        headStartBody:
          "連接 runtime 後，我們會依你的角色挑選範本，並幫它寫好第一個任務。",
        learnAgents: "了解 Agent 如何運作 →",
      },
    },
    workspace: {
      eyebrowReusing: "從現有工作區繼續，或重新開始",
      eyebrowFresh: "你的第一個工作區",
      titleReusingPrefix: "繼續使用 ",
      titleReusingSuffix: "，或建立另一個。",
      titleFresh: "為你的工作區命名。",
      descriptionReusing:
        "可以用現有的工作區繼續設定，或在旁邊建立一個新的 — 你可以同時加入任意多個工作區。",
      descriptionFresh:
        "工作區是放置 Issue、Agent 和專案的地方。日後你可以邀請隊友，或建立更多工作區。",
      nameLabel: "工作區名稱",
      namePlaceholder: "Acme Inc、My Lab、Side Projects…",
      urlLabel: "網址",
      slugPlaceholder: "acme",
      issuePrefixLabel: "Issue 前綴",
      issuePrefixHintPrefix: "Issue 看起來會像 ",
      issuePrefixHintSuffix: "。日後可以在設定裡修改。",
      createNewTitle: "建立新的工作區",
      createNewSubtitle: "重新開始 — 為不同領域的工作開一個獨立空間。",
      hintOpeningPrefix: "正在開啟 ",
      hintOpeningSuffix: "。",
      hintCreatingPrefix: "正在建立 ",
      hintCreatingSuffix: "。",
      hintCreatingFallback: "你的工作區",
      hintNeedName: "請為工作區命名以建立。",
      hintPickFirst: "選擇現有工作區，或建立新的。",
      openLabelPrefix: "開啟 ",
      creatingLabel: "建立中…",
      createLabelPrefix: "建立 ",
      createWorkspaceLabel: "建立工作區",
      continueLabel: "繼續",
      chooseDifferentSlug: "請選擇不同的工作區網址",
      createFailed: "建立工作區失敗",
      side: {
        whatLivesEyebrow: "工作區裡有什麼",
        thingsYouDoEyebrow: "你會在這裡做的事",
        yourWorkspaceEyebrow: "你的工作區",
        whatsNextEyebrow: "接下來",
        previewWorkspaceName: "你的工作區",
        previewSlug: "workspace",
        perks: {
          assignAgents: "像指派隊友一樣，把 Issue 指派給 Agent",
          chatAgents: "不必開 Issue，也能直接和任一 Agent 聊天",
          inviteTeammates: "邀請隊友 — 他們只會看到這個工作區",
          switchWorkspaces: "隨時可從左上角切換到其他工作區",
          connectRuntime: "連接 runtime，讓你的 Agent 有地方執行",
          createAgent: "建立第一個與你角色相符的 Agent",
          watchAgent: "看著它接下入門任務並回覆",
        },
        sidebarLabels: {
          inbox: "收件匣",
          inboxMeta: "你的通知",
          issues: "Issue",
          issuesMeta: "共享的任務看板",
          agents: "Agent",
          agentsMeta: "你的 AI 隊友",
          projects: "專案",
          projectsMeta: "把相關 Issue 分組",
          autopilot: "Autopilot",
          autopilotMeta: "排程自動化",
          runtimes: "Runtime",
          runtimesMeta: "Agent 執行的地方",
          skills: "Skill",
          skillsMeta: "可重複使用的腳本",
          andMore: "更多功能",
          andMoreMeta: "更多",
        },
      },
    },
    runtime: {
      scanning: {
        title: "正在尋找你的工具…",
        body: {
          prefix:
            "Multica 可驅動本機 AI 編程工具，例如 Claude Code、Codex、Cursor 等。我們正在等待你的電腦回報已安裝哪些工具。",
          suffix: "",
        },
      },
      found: {
        title: "找到你的 runtime 了。",
        description:
          "我們掃描了你電腦上已設定好的 AI 編程工具。為你的第一個 Agent 選一個吧。",
        summaryRuntimeSingular: "個 runtime",
        summaryRuntimePlural: "個 runtime",
        allOnline: "全部在線",
        noneOnline: "皆未在線",
        countOnline: (n) => `${n} 個在線`,
        online: "在線",
        offline: "離線",
      },
      empty: {
        title: "未偵測到支援的工具。",
        bodyPrefix:
          "Multica 可驅動本機 AI 編程工具，例如 Claude Code、Codex、Cursor 等 — 但這台電腦上找不到這些工具。安裝其中一個再回來，或從下方選擇路徑。",
        bodySuffix: "",
        skipTitle: "暫時略過",
        skipSubtitle:
          "以唯讀模式進入工作區。在 runtime 連接前 Agent 無法執行任務 — 但你仍可瀏覽、規劃並邀請隊友。",
        skipAction: "略過",
        waitlistTitle: "加入雲端 runtime 候補名單",
        waitlistSubtitle:
          "我們將為你代管 runtime — 不必本機安裝、不需設定。尚未上線；點擊留下電子郵件以收到通知。",
        waitlistAction: "加入候補名單",
        waitlistJoined: "已在候補名單上",
      },
      waitlistDialog: {
        title: "加入雲端 runtime 候補名單",
        description:
          "雲端 runtime 尚未上線。留下電子郵件，上線時我們會寄信通知你。",
      },
      footer: {
        hintFoundSelectedPrefix: "已選擇：",
        hintFoundSelectedSuffix: "",
        hintFoundPick: "請從上方挑選 runtime 以繼續。",
        hintScanning: "正在等待第一筆結果…",
        hintWaitlistedSubmitted: "你已在候補名單上 — 略過以繼續探索。",
        hintEmpty:
          "略過以進入工作區，或從上方加入雲端候補名單。",
      },
      aside: {
        whatRuntimeEyebrow: "什麼是 runtime？",
        whatRuntimeBodyPrefix: "",
        whatRuntimeBodyEmphasis: "runtime",
        whatRuntimeBodySuffix:
          " 是執行於你電腦上的小型背景程式。它將你的工作區連接到 Claude Code、Codex 等 AI 編程工具，並執行 Agent 接下的任務。",
        goodToKnowEyebrow: "需要知道的事",
        swapTitle: "隨時可切換",
        swapBody:
          "每個 Agent 的 runtime 只是一項設定，隨時可以變更。",
        addMoreTitle: "之後可以加入更多",
        addMoreBody:
          "可以在另一台機器上連接第二個 runtime 給團隊使用，或為每個 Agent 配置獨立的 runtime。",
        learnLink: "了解 runtime →",
      },
    },
    platformFork: {
      eyebrow: "第 3 步 · Runtime",
      title: "連接 runtime。",
      description:
        "Runtime 是真正執行 Agent 工作的地方。選擇你想設定的方式。",
      footer: {
        hintWaitlisted:
          "你已在候補名單上 — 點選「略過」以繼續探索。",
        hintDownloaded:
          "請在下載頁完成設定，再回到此分頁。",
        hintDefault: "從上方挑選一條路徑 — 或先略過，之後再設定 runtime。",
      },
      primaryDownload: "下載桌面版應用程式",
      primaryDownloaded: "正在下載頁繼續設定…",
      primaryDownloadSubtitle:
        "內建 daemon、零設定。下一頁挑選你的平台。",
      primaryDownloadedSubtitle:
        "已在新分頁開啟。在那裡選擇安裝程式，然後在桌面版完成設定。",
      primaryDownloadCta: "下載",
      cliTitle: "安裝 CLI",
      cliSubtitle:
        "適用於伺服器、遠端開發機與無介面環境。需要使用終端機。",
      cliAction: "顯示步驟",
      cloudTitle: "雲端 runtime",
      cloudSubtitle:
        "我們代管 runtime。尚未上線 — 加入候補名單。",
      cloudActionDefault: "加入候補名單",
      cloudActionSubmitted: "已在名單上",
      cliDialog: {
        title: "安裝 CLI",
        description:
          "與桌面版相同的 daemon，透過終端機安裝。當桌面版不適用時使用 — 像是伺服器、遠端開發機或無介面環境。",
        runtimeConnectedSingular: "個 runtime 已連接",
        runtimeConnectedPlural: "個 runtime 已連接",
        hintSelectedPrefix: "已選擇：",
        hintSelectedSuffix: "",
        hintPickRuntime: "請從上方挑選 runtime。",
        connectAndContinue: "連接並繼續",
        waiting: {
          liveListening: "即時 · 正在等候你的 daemon",
          normalPrefix: "執行上方的指令。當 ",
          normalCommand: "multica setup",
          normalSuffix:
            " 完成瀏覽器登入並啟動 daemon 後，你的 runtime 會自動出現在這裡（通常 10–30 秒）。",
          midwayPrefix: "仍在等候。請確認你已完成 ",
          midwayCommand: "multica setup",
          midwaySuffix:
            " 開啟的瀏覽器分頁 — 它需要你核准登入後 daemon 才會啟動。",
          slowPrefix:
            "比平常稍久。請檢查你執行 ",
          slowCommand: "multica setup",
          slowSuffix: " 的終端機是否有錯誤訊息。",
          stalledPrefix:
            "目前還沒有任何回應。如果你不熟悉終端機，",
          stalledDesktop: "桌面版",
          stalledSuffix:
            " 是更順暢的路徑 — 它已內建 daemon。關閉此對話框並挑選桌面版，或點選「略過」以繼續。",
        },
      },
      cloudDialog: {
        title: "加入雲端 runtime 候補名單",
        description:
          "雲端 runtime 尚未上線。留下電子郵件，上線時我們會寄信通知你。",
      },
    },
    cliInstall: {
      intro:
        "你需要在這台電腦上安裝 AI 編程工具（Claude Code、Codex、Cursor 等）daemon 才能實際運作。也適用於伺服器與遠端開發機。",
      step1Label: "安裝 Multica CLI",
      step2Label: "啟動 daemon",
      copyAriaLabel: "複製",
    },
    cloudWaitlist: {
      intro:
        "雲端 runtime 尚未上線。留下電子郵件，上線時我們會主動聯繫你。",
      introWarning:
        "提醒：沒有 runtime，Agent 將無法執行任務 — 若現在略過，工作區會維持唯讀，直到你回來安裝 runtime。",
      emailLabel: "電子郵件",
      emailPlaceholder: "you@work.com",
      reasonLabel: "為什麼選擇雲端？",
      reasonOptional: "選填",
      reasonPlaceholder:
        "例如：我們希望 Agent 24/7 全天運作，或團隊使用不同裝置。",
      submit: "加入候補名單",
      submitted: "你已在名單上",
      successToast: "你已加入名單。雲端 runtime 上線時，我們會寄信通知。",
      failureToast: "加入候補名單失敗",
    },
    agent: {
      eyebrow: "你的第一個 Agent",
      title: "認識你的第一位隊友。",
      descriptionPrefix: "依你的回答，我們推薦 ",
      descriptionSuffix:
        "。從四個範本中挑一個適合你的 — 每個範本都已可立即接下第一個 Issue。日後可在 Agent 設定頁調整指示內容。",
      recommended: "推薦",
      creating: "建立中",
      createPrefix: "建立 ",
      footerHint: "一個 Agent 就足以開始。日後可從側邊欄新增更多。",
      createFailed: "建立 Agent 失敗",
      side: {
        whatEyebrow: "什麼是 Agent",
        whatHeadline: "住在你工作區裡的 AI 隊友。",
        whatBody:
          "Agent 會出現在每個指派人選單中，就像其他同事一樣 — 不同的是，只要你給它 runtime，它就能 24/7 工作。",
        waysEyebrow: "與 Agent 合作的方式",
        assignTitle: "指派 Issue",
        assignBody: "它會接下任務，並在討論串中回報。",
        mentionTitle: "在留言中 @ 提及",
        mentionBody: "把它拉進對話，請它快速給點意見。",
        chatTitle: "一對一聊天",
        chatBody: "不用開 Issue，也能直接問問題。",
        autopilotTitle: "啟用 Autopilot",
        autopilotBody:
          "每日分流、每週摘要、每月稽核 — 全部按排程自動執行。",
        footerNote:
          "隨時可新增更多 Agent。一支由專業 Agent 組成的小團隊，勝過一個樣樣通的全能 Agent。",
        learnLink: "建立你的第一個 Agent →",
      },
      templates: {
        coding: {
          label: "Coding Agent",
          blurb: "撰寫、重構並交付程式碼。會閱讀你的 repo。",
        },
        planning: {
          label: "Planning Agent",
          blurb: "拆解工作、撰寫規格，讓任務看板保持整齊。",
        },
        writing: {
          label: "Writing Agent",
          blurb: "草稿、摘要、研究。適合長篇內容。",
        },
        assistant: {
          label: "Assistant",
          blurb: "通用型。任務不明確時的好預設。",
        },
      },
    },
    firstIssue: {
      finishingTitle: "正在收尾",
      finishingSubtitle: "快好了 — 正在開啟你的工作區。",
      errorTitle: "出了一點問題",
      retryFailed: "重試失敗",
    },
    starterContent: {
      title: "歡迎 — 要加入入門任務嗎？",
      descriptionPrefix: "一個 ",
      descriptionEmphasis: "Getting Started",
      descriptionSuffix:
        " 專案，內含簡短任務，帶你了解 Multica 中 Agent、Issue 與情境如何運作。",
      addStarter: "加入入門任務",
      startBlank: "從空白工作區開始",
      addedToast: "入門任務已加入 — 請查看側邊欄",
      importFailed: "匯入失敗 — 請重試",
      dismissFailed: "無法關閉 — 請重試",
    },
  };
}
