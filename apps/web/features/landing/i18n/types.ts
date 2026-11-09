import type { SupportedLocale } from "@multica/core/i18n";
export { docsHrefForLocale } from "@/lib/docs-href";

export type Locale = SupportedLocale;
export type LandingDictionaryLocale = "en" | "zh" | "ko" | "ja";

export const locales: Locale[] = ["en", "zh-Hans", "ko", "ja"];

export const localeLabels: Record<Locale, string> = {
  en: "English",
  "zh-Hans": "\u4e2d\u6587",
  // The landing dictionary has no Traditional Chinese variant yet, so
  // `locales` above does not offer it and it reads the Simplified copy.
  "zh-Hant": "\u7e41\u9ad4\u4e2d\u6587",
  ko: "\ud55c\uad6d\uc5b4",
  ja: "\u65e5\u672c\u8a9e",
  // The landing dictionary has no French, Spanish or Russian variant yet.
  // `locales` above still offers four languages; these labels satisfy the Record type.
  fr: "FR",
  es: "ES",
  ru: "\u0420\u0443\u0441\u0441\u043a\u0438\u0439",
};

export function toLandingDictionaryLocale(
  locale: Locale,
): LandingDictionaryLocale {
  if (locale === "ko") return "ko";
  if (locale === "ja") return "ja";
  return isZhLocale(locale) ? "zh" : "en";
}

export function isZhLocale(locale: Locale): boolean {
  return locale === "zh-Hans" || locale === "zh-Hant";
}

// Long-form page copy. Paragraphs and bullets may embed `[label](href)`
// links, rendered by InlineLinks.
export type DocumentSection = {
  heading: string;
  paragraphs?: string[];
  bullets?: string[];
};

export type ContactSalesOption = { value: string; label: string };

type Copy = { title: string; body: string; alt: string };
type HeroWorkflow = { label: string; short: string; description: string };

/** Labels for the marketing site's header, menus, footer, and closing call to action. */
export type SiteDict = {
  nav: {
    product: string;
    solutions: string;
    resources: string;
    customers: string;
    pricing: string;
    documentation: string;
    changelog: string;
    useCases: string;
    blog: string;
    download: string;
    company: string;
    about: string;
    careers: string;
    press: string;
    contactSales: string;
    trust: string;
    security: string;
    licensing: string;
    privacy: string;
  };
  header: {
    home: string;
    /** `{count}` is the formatted star count. */
    githubStars: string;
    logIn: string;
    getStarted: string;
    dashboard: string;
  };
  footer: {
    tagline: string;
    navigation: string;
    language: string;
    /** `{year}` is the current year. */
    copyright: string;
  };
  cta: { title: string; lead: string; getStarted: string; bookDemo: string };
};

/** A workflow the hero board plays: its project, its statuses by key, and its issues' titles by issue key. */
type DemoWorkflow<S extends string> = {
  project: string;
  statuses: Record<S, string>;
  cards: Record<string, string>;
  focus: { title: string; description: string; label: string };
};

/**
 * Text inside the product mockups. `app` is the Multica interface itself and
 * follows the product's own translations; the rest is the fictional Acme Labs
 * team's work. People's and agents' names stay as they are, like any name.
 */
export type DemoDict = {
  app: {
    spaces: { inbox: string; work: string; team: string; chat: string; autopilots: string };
    views: string;
    projects: string;
    myIssues: string;
    myAgents: string;
    allIssues: string;
    allViews: string;
    /** `{count}` is the number of projects. */
    allProjects: string;
    unassigned: string;
    priority: { none: string; urgent: string; high: string; medium: string; low: string };
    activity: string;
    leaveComment: string;
    created: string;
    /** `{from}` and `{to}` are status names. */
    statusChanged: string;
    handedTo: string;
    startedRun: string;
    memberNoRun: string;
    /** `{name}` is an agent's name. */
    isWorking: string;
    /** `{count}` is the number of agents. */
    agentsWorking: string;
    working: string;
    online: string;
    inProgress: string;
    /** `{count}` is the number of minutes, hours or days. */
    ago: { now: string; minutes: string; hours: string; days: string };
  };
  hero: {
    ship: DemoWorkflow<"spec" | "build" | "review" | "approve" | "shipped"> & {
      plan: { comment: string; title: string; items: string[] };
      built: { comment: string; pr: string };
      tested: string;
      approved: string;
    };
    launch: DemoWorkflow<"brief" | "research" | "draft" | "edit" | "published"> & {
      research: { comment: string; title: string; aside: string; bars: string[] };
      draft: { comment: string; meta: string };
      edited: string;
    };
    triage: DemoWorkflow<"inbox" | "repro" | "fix" | "reply" | "resolved"> & {
      /** The autopilot that files the issue. */
      autopilot: string;
      grouped: { comment: string; quotes: string[] };
      reproduced: string;
      fixed: { comment: string; pr: string };
      replied: string;
    };
    /** The fourth project in the side panel, which no workflow plays. */
    otherProject: string;
    blocks: {
      /** `{count}` is the number of steps. */
      steps: string;
      checks: { build: string; lint: string };
      /** `{count}` is the number of tests. */
      tests: string;
      load: { title: string; served: string; limit: string };
      exportTime: { title: string; aside: string; timeout: string };
      reports: { title: string; merged: string };
      sent: string;
    };
  };
  features: {
    runtimes: {
      title: string;
      machines: string;
      cloud: string;
      server: string;
      laptop: string;
      thisDevice: string;
      sandboxes: string;
    };
    workflow: {
      breadcrumb: string;
      onEnter: string;
      next: string;
      instructions: string;
      instructionsBody: string;
      finalStep: string;
    };
    chat: {
      channels: string;
      apps: string;
      /** The message reads `{before} @Backend Agent {after}`. */
      ask: { before: string; after: string };
      reply: string;
      issue: string;
      /** `{channel}` is the channel's name. */
      composer: string;
    };
    autopilots: { title: string; name: string; agent: string; nextRun: string; rows: { name: string; next: string }[] };
    skills: { title: string; name: string; usedBy: string; rows: string[] };
  };
};

/** A page's title, search description, and the card shown when its link is shared. */
export type PageMeta = { title: string; description: string; ogTitle: string; ogDescription: string };

export type MetaPage = "home" | "about" | "changelog" | "contactSales" | "download" | "licensing" | "privacy";

/** Copy for the home page. */
export type HomeDict = {
  hero: {
    /** The headline reads `{headlinePrefix}{agent}{headlineSuffix}`. */
    headlinePrefix: string;
    headlineSuffix: string;
    lead: string;
    download: string;
    /** `{os}` is macOS, Windows or Linux. */
    downloadFor: string;
    /** `{platforms}` lists the other builds, e.g. "Intel · Windows · Linux". */
    alsoFor: string;
    bookDemo: string;
    workflowGroup: string;
    workflows: { ship: HeroWorkflow; launch: HeroWorkflow; triage: HeroWorkflow };
  };
  logoWall: { title: string };
  features: {
    title: string;
    items: { runtimes: Copy; workflows: Copy; chat: Copy; autopilots: Copy; skills: Copy };
  };
  agents: { lead: string; title: string; all: string };
  updates: {
    eyebrow: string;
    title: string;
    latest: string;
    /** `{count}` is how many more changes the release has. */
    more: string;
    all: string;
  };
};

export type LandingDict = {
  site: SiteDict;
  home: HomeDict;
  demo: DemoDict;
  meta: Record<MetaPage, PageMeta>;
  faq: {
    label: string;
    headline: string;
    items: { question: string; answer: string }[];
  };
  about: {
    title: string;
    nameLine: {
      prefix: string;
      mult: string;
      iplexed: string;
      i: string;
      nformationAnd: string;
      c: string;
      omputing: string;
      a: string;
      gent: string;
    };
    paragraphs: string[];
    cta: string;
    team: {
      title: string;
      paragraphs: string[];
      contacts: { label: string; linkLabel: string; href: string }[];
    };
  };
  licensing: {
    title: string;
    intro: string[];
    rule: { title: string; text: string };
    scenarios: {
      title: string;
      scenarioColumn: string;
      licenseColumn: string;
      required: string;
      notRequired: string;
      items: { scenario: string; example?: string; required: boolean }[];
    };
    sections: DocumentSection[];
  };
  privacy: {
    title: string;
    lastUpdated: string;
    intro: string[];
    sections: DocumentSection[];
  };
  changelog: {
    title: string;
    subtitle: string;
    toc: string;
    categories: {
      features: string;
      improvements: string;
      fixes: string;
    };
    entries: {
      version: string;
      date: string;
      title: string;
      changes: string[];
      features?: string[];
      improvements?: string[];
      fixes?: string[];
    }[];
  };
  download: {
    platforms: { macArm64: string; macX64: string; winX64: string; winArm64: string; linuxX64: string; linuxArm64: string };
    hero: {
      eyebrow: string;
      title: { mac: string; windows: string; linux: string; other: string };
      lead: string;
      leadOther: string;
      mobileNote: string;
      primary: {
        macArm64Dmg: string;
        macX64Dmg: string;
        winX64Exe: string;
        winArm64Exe: string;
        linuxAmd64AppImage: string;
        linuxArm64AppImage: string;
      };
      /** The other Mac architecture, offered beside a guessed one. */
      alternate: { macArm64Dmg: string; macX64Dmg: string };
      /** `{date}` is the release date. */
      released: string;
      whatsNew: string;
      versionUnavailable: string;
    };
    allPlatforms: {
      title: string;
      thisDevice: string;
      formatDmg: string;
      formatZip: string;
      formatExe: string;
      formatAppImage: string;
      formatDeb: string;
      formatRpm: string;
      unavailable: string;
      allReleases: string;
    };
    cli: {
      title: string;
      sub: string;
      installLabel: string;
      platformGroup: string;
      platformMacosLinux: string;
      platformWindows: string;
      startLabel: string;
      copyLabel: string;
      copiedLabel: string;
    };
    started: {
      title: string;
      retry: string;
      allDownloads: string;
      install: { title: string; dmg: string; zip: string; exe: string; appImage: string; deb: string; rpm: string };
      signIn: { title: string; body: string; installed: string; open: string };
      agents: { title: string; body: string };
      community: {
        label: string;
        githubTitle: string;
        githubBody: string;
        star: string;
        /** `{count}` is the repo's star count. */
        stars: string;
        discordTitle: string;
        discordBody: string;
        join: string;
        xTitle: string;
        xBody: string;
        follow: string;
      };
    };
  };
  contactSales: {
    pageTitle: string;
    pageDescription: string;
    eyebrow: string;
    title: string;
    fields: {
      firstName: string;
      lastName: string;
      businessEmail: string;
      businessEmailHint: string;
      companyName: string;
      companySize: string;
      countryRegion: string;
      useCase: string;
      goals: string;
      selectPlaceholder: string;
      submit: string;
      submitting: string;
    };
    companySizes: ContactSalesOption[];
    useCases: ContactSalesOption[];
    countries: string[];
    consent: {
      intro: string;
      outreach: string;
      updates: string;
      unsubscribe: string;
      submitConsent: string;
      privacyLinkLabel: string;
      privacyLinkHref: string;
    };
    success: { title: string; message: string; cta: string };
    errors: {
      generic: string;
      rateLimit: string;
      freeEmail: string;
      invalidEmail: string;
    };
  };
};
