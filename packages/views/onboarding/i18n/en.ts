import type { OnboardingDict } from "./types";

export function createEnDict(): OnboardingDict {
  return {
    common: {
      back: "Back",
      next: "Next",
      skip: "Skip",
      skipForNow: "Skip for now",
      continue: "Continue",
      cancel: "Cancel",
      close: "Close",
      done: "Done",
      retry: "Retry",
      finishOnboardingFailed: "Failed to finish onboarding",
    },
    stepHeader: {
      stepOf: (current, total) => `Step ${current} of ${total}`,
    },
    welcome: {
      brand: "Welcome to Multica",
      headline1: "Your AI teammates,",
      headline2Prefix: "in ",
      headline2Emphasis: "one workspace.",
      headline2Suffix: "",
      leadParagraph:
        "Assign them work like you'd assign a colleague — they pick it up, update status, and comment when done.",
      secondaryWeb:
        "Desktop bundles the runtime — nothing to install. Continue on web to connect your own CLI.",
      secondaryDesktop:
        "By the end, a real agent will be replying to your first issue.",
      download: "Download Desktop",
      continueOnWeb: "Continue on web",
      startExploring: "Start exploring",
      skipExisting: "I've done this before",
      illustrationCaption:
        "Every issue, every thread, every decision — shared by your team and agents.",
    },
    questionnaire: {
      eyebrow: "Before we start",
      title: "Three questions to get to know you.",
      answeredCounter: (n, total) => `${n} of ${total} answered`,
      continue: "Continue",
      questions: {
        teamSize: "Who will use this workspace?",
        role: "What best describes you?",
        useCase: "What do you want to do with Multica?",
      },
      options: {
        teamSize: {
          solo: "Just me",
          team: "My team (2–10 people)",
          otherPlaceholder: "e.g. a small community I help run",
        },
        role: {
          developer: "Software developer",
          productLead: "Product or project lead",
          writer: "Writer or content creator",
          founder: "Founder or operator",
          otherPlaceholder: "e.g. researcher, designer, ops lead",
        },
        useCase: {
          coding: "Write and ship code",
          planning: "Plan and manage projects",
          writingResearch: "Research or write",
          explore: "I'm just exploring for now",
          otherPlaceholder: "e.g. automate my weekly reports",
        },
      },
      sidePanel: {
        whyEyebrow: "Why three questions",
        whyHeadline: "So you land running.",
        whatGetEyebrow: "What you get",
        starterTitle: "A starter project, tailored",
        starterBody:
          "A Getting Started checklist shaped by your answers.",
        headStartTitle: "A head start with agents",
        headStartBody:
          "Connect a runtime and we'll pick a template for your role — plus write its first task.",
        learnAgents: "Learn how agents work →",
      },
    },
    workspace: {
      eyebrowReusing: "Pick up or start fresh",
      eyebrowFresh: "Your first workspace",
      titleReusingPrefix: "Continue with ",
      titleReusingSuffix: ", or start another.",
      titleFresh: "Name your workspace.",
      descriptionReusing:
        "Resume setup with the workspace you already have, or create a new one alongside it — you can belong to any number of workspaces.",
      descriptionFresh:
        "A workspace is where your issues, agents, and projects live. You can invite teammates or spin up more workspaces later.",
      nameLabel: "Workspace name",
      namePlaceholder: "Acme Inc, My Lab, Side Projects…",
      urlLabel: "URL",
      slugPlaceholder: "acme",
      issuePrefixLabel: "Issue prefix",
      issuePrefixHintPrefix: "Issues will look like ",
      issuePrefixHintSuffix:
        ". You can change this later in settings.",
      createNewTitle: "Create a new workspace",
      createNewSubtitle:
        "Start fresh — a separate space for a different side of your work.",
      hintOpeningPrefix: "Opening ",
      hintOpeningSuffix: ".",
      hintCreatingPrefix: "Creating ",
      hintCreatingSuffix: ".",
      hintCreatingFallback: "your workspace",
      hintNeedName: "Name your workspace to create it.",
      hintPickFirst: "Pick your workspace or start a new one.",
      openLabelPrefix: "Open ",
      creatingLabel: "Creating…",
      createLabelPrefix: "Create ",
      createWorkspaceLabel: "Create workspace",
      continueLabel: "Continue",
      chooseDifferentSlug: "Choose a different workspace URL",
      createFailed: "Failed to create workspace",
      side: {
        whatLivesEyebrow: "What lives inside a workspace",
        thingsYouDoEyebrow: "Things you'll do here",
        yourWorkspaceEyebrow: "Your workspace",
        whatsNextEyebrow: "What's next",
        previewWorkspaceName: "Your workspace",
        previewSlug: "workspace",
        perks: {
          assignAgents: "Assign issues to agents like you would a teammate",
          chatAgents: "Chat with any agent without creating an issue",
          inviteTeammates: "Invite teammates — they see only this workspace",
          switchWorkspaces:
            "Switch to other workspaces anytime from the top-left",
          connectRuntime:
            "Connect a runtime so your agents have somewhere to run",
          createAgent: "Create your first agent matched to your role",
          watchAgent: "Watch it pick up a starter task and reply",
        },
        sidebarLabels: {
          inbox: "Inbox",
          inboxMeta: "your notifications",
          issues: "Issues",
          issuesMeta: "shared task board",
          agents: "Agents",
          agentsMeta: "your AI teammates",
          projects: "Projects",
          projectsMeta: "group related issues",
          autopilot: "Autopilot",
          autopilotMeta: "scheduled automation",
          runtimes: "Runtimes",
          runtimesMeta: "where agents run",
          skills: "Skills",
          skillsMeta: "reusable playbooks",
          andMore: "And more",
          andMoreMeta: "and more",
        },
      },
    },
    runtime: {
      scanning: {
        title: "Looking for your tools…",
        body: {
          prefix:
            "Multica drives local AI coding tools like Claude Code, Codex, Cursor, and others. We're waiting to hear back from your machine about which ones are installed.",
          suffix: "",
        },
      },
      found: {
        title: "We found your runtimes.",
        description:
          "We scanned your machine for AI coding tools you've already set up. Pick one for your first agent.",
        summaryRuntimeSingular: "runtime",
        summaryRuntimePlural: "runtimes",
        allOnline: "all online",
        noneOnline: "none online",
        countOnline: (n) => `${n} online`,
        online: "online",
        offline: "offline",
      },
      empty: {
        title: "No supported tools detected.",
        bodyPrefix:
          "Multica drives local AI coding tools like Claude Code, Codex, Cursor, and others — we didn't find any on this machine. Install one and come back, or pick a path below.",
        bodySuffix: "",
        skipTitle: "Skip for now",
        skipSubtitle:
          "Enter your workspace in read-only mode. Agents can't execute tasks until a runtime connects — but you can still browse, plan, and invite teammates.",
        skipAction: "Skip",
        waitlistTitle: "Join the cloud runtime waitlist",
        waitlistSubtitle:
          "We'll host the runtime for you — no local install, no setup. Not live yet; click to leave your email and get notified.",
        waitlistAction: "Join waitlist",
        waitlistJoined: "On the waitlist",
      },
      waitlistDialog: {
        title: "Join the cloud runtime waitlist",
        description:
          "Cloud runtimes aren't live yet. Leave your email and we'll email you when they are.",
      },
      footer: {
        hintFoundSelectedPrefix: "Selected: ",
        hintFoundSelectedSuffix: "",
        hintFoundPick: "Pick a runtime above to continue.",
        hintScanning: "Waiting for the first result…",
        hintWaitlistedSubmitted:
          "You're on the waitlist — skip to keep exploring.",
        hintEmpty:
          "Skip to enter your workspace, or join the cloud waitlist above.",
      },
      aside: {
        whatRuntimeEyebrow: "What's a runtime?",
        whatRuntimeBodyPrefix: "A ",
        whatRuntimeBodyEmphasis: "runtime",
        whatRuntimeBodySuffix:
          " is a small background process that runs on your machine. It connects your workspace to AI coding tools like Claude Code or Codex, and executes the tasks your agents pick up.",
        goodToKnowEyebrow: "Good to know",
        swapTitle: "Swap anytime",
        swapBody:
          "Each agent's runtime is just a setting. Change it whenever you want.",
        addMoreTitle: "Add more later",
        addMoreBody:
          "You can connect a second runtime on another machine for a team, or a dedicated one per agent.",
        learnLink: "Learn about runtimes →",
      },
    },
    platformFork: {
      eyebrow: "Step 3 · Runtime",
      title: "Connect a runtime.",
      description:
        "A runtime is what actually runs your agents' work. Pick how you'd like to set one up.",
      footer: {
        hintWaitlisted:
          "You're on the waitlist — pick Skip to keep exploring.",
        hintDownloaded:
          "Finish setup on the download page, then come back to this tab.",
        hintDefault:
          "Pick a path above — or skip and configure a runtime later.",
      },
      primaryDownload: "Download the desktop app",
      primaryDownloaded: "Continuing on the download page…",
      primaryDownloadSubtitle:
        "Bundled daemon, zero setup. Pick your platform on the next page.",
      primaryDownloadedSubtitle:
        "Opened in a new tab. Pick your installer there, then finish setup on desktop.",
      primaryDownloadCta: "Download",
      cliTitle: "Install the CLI",
      cliSubtitle:
        "For servers, remote dev boxes, and headless setups. Terminal required.",
      cliAction: "Show steps",
      cloudTitle: "Cloud runtime",
      cloudSubtitle:
        "We host the runtime. Not live yet — join the waitlist.",
      cloudActionDefault: "Join waitlist",
      cloudActionSubmitted: "On the list",
      cliDialog: {
        title: "Install the CLI",
        description:
          "Same daemon as Desktop, installed via terminal. Use it when Desktop doesn't fit — servers, remote dev boxes, or headless setups.",
        runtimeConnectedSingular: "runtime connected",
        runtimeConnectedPlural: "runtimes connected",
        hintSelectedPrefix: "Selected: ",
        hintSelectedSuffix: "",
        hintPickRuntime: "Pick a runtime above.",
        connectAndContinue: "Connect & continue",
        waiting: {
          liveListening: "Live · Listening for your daemon",
          normalPrefix: "Run the command above. As soon as ",
          normalCommand: "multica setup",
          normalSuffix:
            " finishes browser sign-in and the daemon starts, your runtime will appear here automatically (usually 10–30 seconds).",
          midwayPrefix: "Still listening. Make sure you finished the browser tab that ",
          midwayCommand: "multica setup",
          midwaySuffix:
            " opened — it needs you to approve the sign-in before the daemon can start.",
          slowPrefix:
            "Taking longer than usual. Check the terminal where you ran ",
          slowCommand: "multica setup",
          slowSuffix: " for errors.",
          stalledPrefix:
            "Nothing coming through yet. If you're not comfortable with the terminal, ",
          stalledDesktop: "Desktop",
          stalledSuffix:
            " is the smoother path — it bundles the daemon. Close this dialog and pick Desktop, or hit Skip to continue.",
        },
      },
      cloudDialog: {
        title: "Join the cloud runtime waitlist",
        description:
          "Cloud runtimes aren't live yet. Leave your email and we'll email you when they are.",
      },
    },
    cliInstall: {
      intro:
        "You'll need an AI coding tool on this machine (Claude Code, Codex, Cursor, …) for the daemon to do real work. Also works on servers and remote dev boxes.",
      step1Label: "Install the Multica CLI",
      step2Label: "Start the daemon",
      copyAriaLabel: "Copy",
    },
    cloudWaitlist: {
      intro:
        "Cloud runtimes aren't live yet. Leave your email and we'll reach out when they are.",
      introWarning:
        "Heads-up: agents can't execute tasks without a runtime — if you hit Skip now, your workspace is read-only until you come back and install one.",
      emailLabel: "Email",
      emailPlaceholder: "you@work.com",
      reasonLabel: "Why cloud?",
      reasonOptional: "Optional",
      reasonPlaceholder:
        "e.g. we want agents running 24/7, or my team works across different devices.",
      submit: "Join waitlist",
      submitted: "You're on the list",
      successToast: "You're on the list. We'll email when cloud runtimes are live.",
      failureToast: "Failed to join waitlist",
    },
    agent: {
      eyebrow: "Your first agent",
      title: "Meet your first teammate.",
      descriptionPrefix: "Your answers point to a ",
      descriptionSuffix:
        ". Pick whichever of the four fits you — each template ships ready to take its first issue. You can retune its instructions from the agent settings page later.",
      recommended: "Recommended",
      creating: "Creating",
      createPrefix: "Create ",
      footerHint: "One agent is enough to start. Add more from the sidebar later.",
      createFailed: "Failed to create agent",
      side: {
        whatEyebrow: "What's an agent",
        whatHeadline: "An AI teammate that lives in your workspace.",
        whatBody:
          "Agents show up in every assignee picker, just like any other colleague — except they can work 24/7 on whatever runtime you give them.",
        waysEyebrow: "Ways to work with an agent",
        assignTitle: "Assign it an issue",
        assignBody: "It picks up the task and reports back in the thread.",
        mentionTitle: "@mention in a comment",
        mentionBody: "Pull it into a conversation for a quick take.",
        chatTitle: "Chat one-on-one",
        chatBody: "Ask quick questions without creating an issue.",
        autopilotTitle: "Put it on Autopilot",
        autopilotBody:
          "Daily triage, weekly digest, monthly audit — on a schedule.",
        footerNote:
          "Add more agents anytime. A small team of specialized agents beats one jack-of-all-trades.",
        learnLink: "Creating your first agent →",
      },
      templates: {
        coding: {
          label: "Coding Agent",
          blurb: "Writes, refactors, and ships code. Reads your repo.",
        },
        planning: {
          label: "Planning Agent",
          blurb: "Breaks down work, drafts specs, keeps the board tidy.",
        },
        writing: {
          label: "Writing Agent",
          blurb: "Drafts, summarizes, researches. Long-form friendly.",
        },
        assistant: {
          label: "Assistant",
          blurb: "General-purpose. Good default when the task is unclear.",
        },
      },
    },
    firstIssue: {
      finishingTitle: "Finishing up",
      finishingSubtitle: "Almost there — opening your workspace.",
      errorTitle: "Something went wrong",
      retryFailed: "Retry failed",
    },
    starterContent: {
      title: "Welcome — add starter tasks?",
      descriptionPrefix: "A ",
      descriptionEmphasis: "Getting Started",
      descriptionSuffix:
        " project with short tasks that walk through how agents, issues, and context work in Multica.",
      addStarter: "Add starter tasks",
      startBlank: "Start blank workspace",
      addedToast: "Starter tasks added — check your sidebar",
      importFailed: "Import failed — please retry",
      dismissFailed: "Could not dismiss — please retry",
    },
  };
}
