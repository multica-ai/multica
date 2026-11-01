import type { SupportedLocale } from "@multica/core/i18n";
export { docsHrefForLocale } from "@/lib/docs-href";

export type Locale = SupportedLocale;
export type LandingDictionaryLocale = "en" | "zh" | "ko" | "ja";

export const locales: Locale[] = ["en", "zh-Hans", "ko", "ja"];

export const localeLabels: Record<Locale, string> = {
  en: "EN",
  "zh-Hans": "\u4e2d\u6587",
  // The landing dictionary has no Traditional Chinese variant yet, so
  // `locales` above does not offer it and it reads the Simplified copy.
  "zh-Hant": "\u7e41\u9ad4\u4e2d\u6587",
  ko: "\ud55c\uad6d\uc5b4",
  ja: "\u65e5\u672c\u8a9e",
  // The landing dictionary has no French variant yet, so `locales` above still
  // offers four languages; this label only satisfies the Record type.
  fr: "FR",
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
type Outcome = { value: string; caption: string };
type Story = { title: string; outcomes: [Outcome, Outcome]; before: string; after: string };
type HeroWorkflow = { label: string; short: string; description: string };

/** Labels for the marketing site's header, menus, footer, and closing call to action. */
export type SiteDict = {
  nav: {
    product: string;
    solutions: string;
    resources: string;
    customers: string;
    pricing: string;
    docs: string;
    worksWith: string;
    allAgents: string;
    learn: string;
    documentation: string;
    changelog: string;
    useCases: string;
    blog: string;
    download: string;
    community: string;
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
    primary: string;
    menu: string;
    openMenu: string;
    closeMenu: string;
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

/** Copy for the home page. Product mockups stay in English, like screenshots of the app. */
export type HomeDict = {
  hero: {
    /** The headline reads `{headlinePrefix}{agent}{headlineSuffix}`. */
    headlinePrefix: string;
    headlineSuffix: string;
    lead: string;
    getStarted: string;
    openDashboard: string;
    bookDemo: string;
    workflowGroup: string;
    workflows: { ship: HeroWorkflow; launch: HeroWorkflow; triage: HeroWorkflow };
  };
  logoWall: { title: string };
  stories: {
    title: string;
    lead: string;
    beforeLabel: string;
    afterLabel: string;
    compare: string;
    /** `{value}` is the share of the stage showing the before scene. */
    compareValue: string;
    items: { board: Story; digest: Story; pipeline: Story };
  };
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
    hero: {
      macArm64: {
        title: string;
        sub: string;
        primary: string;
        altZip: string;
      };
      macIntel: {
        title: string;
        sub: string;
        primary: string;
        altZip: string;
      };
      winX64: { title: string; sub: string; primary: string };
      winArm64: { title: string; sub: string; primary: string };
      linux: {
        title: string;
        sub: string;
        primary: string;
        altFormats: string;
      };
      unknown: { title: string; sub: string };
      safariMacHint: string;
      archFallbackHint: string;
    };
    allPlatforms: {
      title: string;
      macArm64Label: string;
      macX64Label: string;
      winX64Label: string;
      winArm64Label: string;
      linuxX64Label: string;
      linuxArm64Label: string;
      formatDmg: string;
      formatZip: string;
      formatExe: string;
      formatAppImage: string;
      formatDeb: string;
      formatRpm: string;
      unavailable: string;
    };
    cli: {
      title: string;
      sub: string;
      installLabel: string;
      platformGroup: string;
      platformMacosLinux: string;
      platformWindows: string;
      startLabel: string;
      sshNote: string;
      copyLabel: string;
      copiedLabel: string;
    };
    cloud: { title: string; sub: string };
    footer: {
      releaseNotes: string;
      allReleases: string;
      currentVersion: string;
      versionUnavailable: string;
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
