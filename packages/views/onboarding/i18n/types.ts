export type OnboardingDict = {
  common: {
    back: string;
    next: string;
    skip: string;
    skipForNow: string;
    continue: string;
    cancel: string;
    close: string;
    done: string;
    retry: string;
    finishOnboardingFailed: string;
  };
  stepHeader: {
    stepOf: (current: number, total: number) => string;
  };
  welcome: {
    brand: string;
    headline1: string;
    headline2Prefix: string;
    headline2Emphasis: string;
    headline2Suffix: string;
    leadParagraph: string;
    secondaryWeb: string;
    secondaryDesktop: string;
    download: string;
    continueOnWeb: string;
    startExploring: string;
    skipExisting: string;
    illustrationCaption: string;
  };
  questionnaire: {
    eyebrow: string;
    title: string;
    answeredCounter: (n: number, total: number) => string;
    continue: string;
    questions: {
      teamSize: string;
      role: string;
      useCase: string;
    };
    options: {
      teamSize: {
        solo: string;
        team: string;
        otherPlaceholder: string;
      };
      role: {
        developer: string;
        productLead: string;
        writer: string;
        founder: string;
        otherPlaceholder: string;
      };
      useCase: {
        coding: string;
        planning: string;
        writingResearch: string;
        explore: string;
        otherPlaceholder: string;
      };
    };
    sidePanel: {
      whyEyebrow: string;
      whyHeadline: string;
      whatGetEyebrow: string;
      starterTitle: string;
      starterBody: string;
      headStartTitle: string;
      headStartBody: string;
      learnAgents: string;
    };
  };
  workspace: {
    eyebrowReusing: string;
    eyebrowFresh: string;
    titleReusingPrefix: string;
    titleReusingSuffix: string;
    titleFresh: string;
    descriptionReusing: string;
    descriptionFresh: string;
    nameLabel: string;
    namePlaceholder: string;
    urlLabel: string;
    slugPlaceholder: string;
    issuePrefixLabel: string;
    issuePrefixHintPrefix: string;
    issuePrefixHintSuffix: string;
    createNewTitle: string;
    createNewSubtitle: string;
    hintOpeningPrefix: string;
    hintOpeningSuffix: string;
    hintCreatingPrefix: string;
    hintCreatingSuffix: string;
    hintCreatingFallback: string;
    hintNeedName: string;
    hintPickFirst: string;
    openLabelPrefix: string;
    creatingLabel: string;
    createLabelPrefix: string;
    createWorkspaceLabel: string;
    continueLabel: string;
    chooseDifferentSlug: string;
    createFailed: string;
    side: {
      whatLivesEyebrow: string;
      thingsYouDoEyebrow: string;
      yourWorkspaceEyebrow: string;
      whatsNextEyebrow: string;
      previewWorkspaceName: string;
      previewSlug: string;
      perks: {
        assignAgents: string;
        chatAgents: string;
        inviteTeammates: string;
        switchWorkspaces: string;
        connectRuntime: string;
        createAgent: string;
        watchAgent: string;
      };
      sidebarLabels: {
        inbox: string;
        inboxMeta: string;
        issues: string;
        issuesMeta: string;
        agents: string;
        agentsMeta: string;
        projects: string;
        projectsMeta: string;
        autopilot: string;
        autopilotMeta: string;
        runtimes: string;
        runtimesMeta: string;
        skills: string;
        skillsMeta: string;
        andMore: string;
        andMoreMeta: string;
      };
    };
  };
  runtime: {
    scanning: {
      title: string;
      body: {
        prefix: string;
        suffix: string;
      };
    };
    found: {
      title: string;
      description: string;
      summaryRuntimeSingular: string;
      summaryRuntimePlural: string;
      allOnline: string;
      noneOnline: string;
      countOnline: (n: number) => string;
      online: string;
      offline: string;
    };
    empty: {
      title: string;
      bodyPrefix: string;
      bodySuffix: string;
      skipTitle: string;
      skipSubtitle: string;
      skipAction: string;
      waitlistTitle: string;
      waitlistSubtitle: string;
      waitlistAction: string;
      waitlistJoined: string;
    };
    waitlistDialog: {
      title: string;
      description: string;
    };
    footer: {
      hintFoundSelectedPrefix: string;
      hintFoundSelectedSuffix: string;
      hintFoundPick: string;
      hintScanning: string;
      hintWaitlistedSubmitted: string;
      hintEmpty: string;
    };
    aside: {
      whatRuntimeEyebrow: string;
      whatRuntimeBodyPrefix: string;
      whatRuntimeBodyEmphasis: string;
      whatRuntimeBodySuffix: string;
      goodToKnowEyebrow: string;
      swapTitle: string;
      swapBody: string;
      addMoreTitle: string;
      addMoreBody: string;
      learnLink: string;
    };
  };
  platformFork: {
    eyebrow: string;
    title: string;
    description: string;
    footer: {
      hintWaitlisted: string;
      hintDownloaded: string;
      hintDefault: string;
    };
    primaryDownload: string;
    primaryDownloaded: string;
    primaryDownloadSubtitle: string;
    primaryDownloadedSubtitle: string;
    primaryDownloadCta: string;
    cliTitle: string;
    cliSubtitle: string;
    cliAction: string;
    cloudTitle: string;
    cloudSubtitle: string;
    cloudActionDefault: string;
    cloudActionSubmitted: string;
    cliDialog: {
      title: string;
      description: string;
      runtimeConnectedSingular: string;
      runtimeConnectedPlural: string;
      hintSelectedPrefix: string;
      hintSelectedSuffix: string;
      hintPickRuntime: string;
      connectAndContinue: string;
      waiting: {
        liveListening: string;
        normalPrefix: string;
        normalCommand: string;
        normalSuffix: string;
        midwayPrefix: string;
        midwayCommand: string;
        midwaySuffix: string;
        slowPrefix: string;
        slowCommand: string;
        slowSuffix: string;
        stalledPrefix: string;
        stalledDesktop: string;
        stalledSuffix: string;
      };
    };
    cloudDialog: {
      title: string;
      description: string;
    };
  };
  cliInstall: {
    intro: string;
    step1Label: string;
    step2Label: string;
    copyAriaLabel: string;
  };
  cloudWaitlist: {
    intro: string;
    introWarning: string;
    emailLabel: string;
    emailPlaceholder: string;
    reasonLabel: string;
    reasonOptional: string;
    reasonPlaceholder: string;
    submit: string;
    submitted: string;
    successToast: string;
    failureToast: string;
  };
  agent: {
    eyebrow: string;
    title: string;
    descriptionPrefix: string;
    descriptionSuffix: string;
    recommended: string;
    creating: string;
    createPrefix: string;
    footerHint: string;
    createFailed: string;
    side: {
      whatEyebrow: string;
      whatHeadline: string;
      whatBody: string;
      waysEyebrow: string;
      assignTitle: string;
      assignBody: string;
      mentionTitle: string;
      mentionBody: string;
      chatTitle: string;
      chatBody: string;
      autopilotTitle: string;
      autopilotBody: string;
      footerNote: string;
      learnLink: string;
    };
    templates: {
      coding: { label: string; blurb: string };
      planning: { label: string; blurb: string };
      writing: { label: string; blurb: string };
      assistant: { label: string; blurb: string };
    };
  };
  firstIssue: {
    finishingTitle: string;
    finishingSubtitle: string;
    errorTitle: string;
    retryFailed: string;
  };
  starterContent: {
    title: string;
    descriptionPrefix: string;
    descriptionEmphasis: string;
    descriptionSuffix: string;
    addStarter: string;
    startBlank: string;
    addedToast: string;
    importFailed: string;
    dismissFailed: string;
  };
};
