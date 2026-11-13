import { notFound } from "next/navigation";
import type { SiteDict } from "../i18n/types";

/**
 * Information architecture of the marketing site: every page and the footer
 * are derived from the registries below, so a page added here appears in
 * navigation, gets a route, and lands in `sitemap.xml` in one edit.
 *
 * Copy is English-only placeholder text while positioning is being settled.
 */

export type SiteLink = { label: string; href: string; external?: boolean };
export type SiteLinkGroup = { title: string; links: SiteLink[] };

export type SitePage = {
  slug: string;
  title: string;
  /** One sentence shown under the page title and used as its meta description. */
  summary: string;
};

export type PageGroup = { title: string; pages: SitePage[] };

export const PRODUCT_GROUPS: PageGroup[] = [
  {
    title: "Surfaces",
    pages: [
      { slug: "cloud", title: "Multica Cloud", summary: "Run agents on cloud agent services, with no computer of yours left on." },
      { slug: "desktop", title: "Multica Desktop", summary: "A native app for macOS, Windows, and Linux that turns your computer into a runtime." },
      { slug: "cli", title: "Multica CLI", summary: "Connect a computer as a runtime and work with Multica from the shell." },
      { slug: "chat", title: "Chat integrations", summary: "Work with agents from Slack, Feishu, DingTalk, WeCom, and Telegram." },
      { slug: "mobile", title: "Mobile", summary: "Your inbox, issues, and agent chats on iPhone." },
    ],
  },
  {
    title: "Workforce",
    pages: [
      { slug: "agents", title: "Agents", summary: "Coding agents that take issues, report progress, and open PRs." },
      { slug: "squads", title: "Squads", summary: "A leader agent that routes work across a team." },
      { slug: "skills", title: "Skills", summary: "Reusable know-how every agent on the team can use." },
      { slug: "autopilots", title: "Autopilots", summary: "Recurring work that runs on a schedule." },
      { slug: "workflows", title: "Workflows", summary: "The steps a project's issues follow, and who owns each one." },
    ],
  },
  {
    title: "Platform",
    pages: [
      { slug: "issues", title: "Issues & Projects", summary: "One board for work done by people and agents." },
      { slug: "runtimes", title: "Runtimes", summary: "Local and cloud compute for your agents, in one panel." },
      { slug: "self-hosting", title: "Self-hosting", summary: "Run Multica on infrastructure you control." },
      { slug: "integrations", title: "Integrations", summary: "GitHub, GitLab, and the tools your team already uses." },
      { slug: "analytics", title: "Analytics", summary: "See what your agents shipped and what it cost." },
      { slug: "api", title: "API & SDK", summary: "Build on Multica from your own code." },
    ],
  },
];

export const SOLUTION_GROUPS: PageGroup[] = [
  {
    title: "Use cases",
    pages: [
      { slug: "software-delivery", title: "Software delivery", summary: "From issue to merged PR with agents doing the legwork." },
      { slug: "code-review", title: "Code review", summary: "Every PR gets a careful first review." },
      { slug: "bug-triage", title: "Bug triage & on-call", summary: "Agents that pick up alerts and propose fixes." },
      { slug: "data-analysis", title: "Data analysis", summary: "A data analyst that answers questions around the clock." },
    ],
  },
  {
    title: "Teams",
    pages: [
      { slug: "engineering", title: "Engineering", summary: "Ship more with the same headcount." },
      { slug: "product-teams", title: "Product", summary: "Turn specs into working software faster." },
      { slug: "operations", title: "Operations", summary: "Hand routine operational work to agents." },
      { slug: "agencies", title: "Agencies", summary: "Deliver more client work with a hybrid team." },
    ],
  },
  {
    title: "Company",
    pages: [
      { slug: "startups", title: "Startups", summary: "Move like a team ten times your size." },
      { slug: "enterprise", title: "Enterprise", summary: "Security, control, and support for large organizations." },
    ],
  },
  {
    title: "Programs",
    pages: [
      { slug: "open-source", title: "Open source", summary: "Multica for open-source maintainers." },
      { slug: "partners", title: "Partners", summary: "Build, resell, or implement Multica with us." },
    ],
  },
];

/** Single pages that exist only as placeholders so far. */
export const PLACEHOLDER_PAGES = {
  pricing: { slug: "pricing", title: "Pricing", summary: "Plans for teams of every size." },
  customers: { slug: "customers", title: "Customers", summary: "Teams shipping with Multica." },
  blog: { slug: "blog", title: "Blog", summary: "Notes on building with human and agent teams." },
  careers: { slug: "careers", title: "Careers", summary: "Help us build the workspace for human and agent teams." },
  press: { slug: "press", title: "Press & brand", summary: "Logos, product imagery, and press contacts." },
  security: { slug: "security", title: "Security", summary: "How Multica protects your code and data." },
} satisfies Record<string, SitePage>;

/** Pages that already have real content and keep their existing routes. */
const EXISTING_ROUTES = [
  "/about",
  "/changelog",
  "/contact-sales",
  "/download",
  "/licensing",
  "/privacy",
  "/usecases",
] as const;

/** Existing pages taken down for now. They are treated like unreleased pages until removed from here. */
const WITHDRAWN_ROUTES: ReadonlySet<string> = new Set(["/usecases"]);

/**
 * Routes released to the public. Every other page is still being built: it is
 * left out of the menus, the footer, and the sitemap, and its route answers
 * 404, unless the build sets NEXT_PUBLIC_SITE_PREVIEW=1. Release a page by
 * adding its route here.
 */
const LIVE_ROUTES: ReadonlySet<string> = new Set([
  "/",
  ...EXISTING_ROUTES.filter((route) => !WITHDRAWN_ROUTES.has(route)),
]);

export const SITE_PREVIEW = process.env.NEXT_PUBLIC_SITE_PREVIEW === "1";

export function isLiveRoute(href: string): boolean {
  return SITE_PREVIEW || LIVE_ROUTES.has(href);
}

/** Call at the top of a page that may not be released yet. */
export function requireLiveRoute(href: string) {
  if (!isLiveRoute(href)) notFound();
}

// The not-found fallback streams after the page's metadata, so an unreleased
// page must not publish its own title and description.
export function liveMetadata<T extends object>(href: string, metadata: T): T | Record<string, never> {
  return isLiveRoute(href) ? metadata : {};
}

export const productHref = (slug: string) => `/product/${slug}`;
export const solutionHref = (slug: string) => `/solutions/${slug}`;

export function findPage(groups: PageGroup[], slug: string): SitePage | undefined {
  return groups.flatMap((group) => group.pages).find((page) => page.slug === slug);
}

type NavLabels = SiteDict["nav"];
type IsLive = (href: string) => boolean;

/** Drops unreleased links, then the groups they leave empty. */
function released(groups: SiteLinkGroup[], isLive: IsLive): SiteLinkGroup[] {
  return groups
    .map((group) => ({ ...group, links: group.links.filter((link) => link.external || isLive(link.href)) }))
    .filter((group) => group.links.length > 0);
}

export function buildFooterGroups(docsHref: string, nav: NavLabels, isLive: IsLive = isLiveRoute): SiteLinkGroup[] {
  const pick = (groups: PageGroup[], slugs: string[], href: (slug: string) => string) =>
    slugs.map((slug) => {
      const page = findPage(groups, slug);
      if (!page) throw new Error(`Unknown page in footer: ${slug}`);
      return { label: page.title, href: href(page.slug) };
    });
  return released(
    [
      {
        title: nav.product,
        links: [
          ...pick(
            PRODUCT_GROUPS,
            ["cloud", "desktop", "cli", "agents", "squads", "skills", "autopilots", "runtimes", "self-hosting"],
            productHref,
          ),
          { label: nav.pricing, href: "/pricing" },
        ],
      },
      {
        title: nav.solutions,
        links: pick(
          SOLUTION_GROUPS,
          ["software-delivery", "code-review", "bug-triage", "data-analysis", "startups", "enterprise"],
          solutionHref,
        ),
      },
      {
        title: nav.resources,
        links: [
          { label: nav.documentation, href: docsHref, external: true },
          { label: nav.changelog, href: "/changelog" },
          { label: nav.useCases, href: "/usecases" },
          { label: nav.blog, href: "/blog" },
          { label: nav.download, href: "/download" },
        ],
      },
      {
        title: nav.company,
        links: [
          { label: nav.about, href: "/about" },
          { label: nav.customers, href: "/customers" },
          { label: nav.careers, href: "/careers" },
          { label: nav.press, href: "/press" },
          { label: nav.contactSales, href: "/contact-sales" },
        ],
      },
      {
        title: nav.trust,
        links: [
          { label: nav.security, href: "/security" },
          { label: nav.licensing, href: "/licensing" },
          { label: nav.privacy, href: "/privacy" },
        ],
      },
    ],
    isLive,
  );
}

/** Every internal route the marketing site serves, for `sitemap.xml` and link checks. */
export function siteRoutes(): string[] {
  return [
    "/",
    ...EXISTING_ROUTES,
    ...Object.values(PLACEHOLDER_PAGES).map((page) => `/${page.slug}`),
    ...PRODUCT_GROUPS.flatMap((group) => group.pages.map((page) => productHref(page.slug))),
    ...SOLUTION_GROUPS.flatMap((group) => group.pages.map((page) => solutionHref(page.slug))),
  ];
}
