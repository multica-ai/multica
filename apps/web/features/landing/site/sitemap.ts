import { discordUrl, githubUrl, twitterUrl } from "../components/shared";

/**
 * Information architecture of the marketing site: every page, the header
 * menus, and the footer are derived from the registries below, so a page added
 * here appears in navigation, gets a route, and lands in `sitemap.xml` in one
 * edit.
 *
 * Copy is English-only placeholder text while positioning is being settled.
 */

export type SiteLink = { label: string; href: string; external?: boolean; description?: string };
export type SiteLinkGroup = { title: string; links: SiteLink[] };
export type SiteMenuId = "product" | "solutions" | "resources";
export type SiteMenu = { id: SiteMenuId; label: string; groups: SiteLinkGroup[] };

export type SitePage = {
  slug: string;
  title: string;
  /** One sentence shown under the page title and used as its meta description. */
  summary: string;
  /** A few words shown under the title in the header menu. */
  tagline?: string;
};

export type PageGroup = { title: string; pages: SitePage[] };

export const PRODUCT_GROUPS: PageGroup[] = [
  {
    title: "Surfaces",
    pages: [
      { slug: "cloud", title: "Multica Cloud", summary: "The hosted workspace for your human and agent team.", tagline: "The hosted team workspace" },
      { slug: "desktop", title: "Multica Desktop", summary: "A native app for macOS, Windows, and Linux.", tagline: "Native app for every desktop" },
      { slug: "cli", title: "Multica CLI", summary: "Connect your machine and run agents from the terminal.", tagline: "Run agents from your terminal" },
      { slug: "chat", title: "Chat integrations", summary: "Work with agents from Slack, Lark, DingTalk, WeCom, and Telegram.", tagline: "Slack, Lark, Telegram, and more" },
      { slug: "mobile", title: "Mobile", summary: "Follow and steer your team's work from your phone.", tagline: "Steer work from your phone" },
    ],
  },
  {
    title: "Workforce",
    pages: [
      { slug: "agents", title: "Agents", summary: "Coding agents that take issues, report progress, and open PRs.", tagline: "Take issues and open PRs" },
      { slug: "squads", title: "Squads", summary: "A leader agent that routes work across a team.", tagline: "A lead agent routes the work" },
      { slug: "skills", title: "Skills", summary: "Reusable know-how every agent on the team can use.", tagline: "Know-how shared by every agent" },
      { slug: "autopilots", title: "Autopilots", summary: "Recurring work that runs on a schedule.", tagline: "Recurring work on a schedule" },
      { slug: "workflows", title: "Workflows", summary: "The steps a project's issues follow, and who owns each one.", tagline: "Steps and owners for each issue" },
    ],
  },
  {
    title: "Platform",
    pages: [
      { slug: "issues", title: "Issues & Projects", summary: "One board for work done by people and agents.", tagline: "One board for people and agents" },
      { slug: "runtimes", title: "Runtimes", summary: "Local and cloud compute for your agents, in one panel.", tagline: "Local and cloud compute" },
      { slug: "self-hosting", title: "Self-hosting", summary: "Run Multica on infrastructure you control.", tagline: "Your servers, your control" },
      { slug: "integrations", title: "Integrations", summary: "GitHub, GitLab, and the tools your team already uses.", tagline: "GitHub, GitLab, and your tools" },
      { slug: "analytics", title: "Analytics", summary: "See what your agents shipped and what it cost.", tagline: "What agents shipped and cost" },
      { slug: "api", title: "API & SDK", summary: "Build on Multica from your own code.", tagline: "Build on Multica in code" },
    ],
  },
];

export const SOLUTION_GROUPS: PageGroup[] = [
  {
    title: "Use cases",
    pages: [
      { slug: "software-delivery", title: "Software delivery", summary: "From issue to merged PR with agents doing the legwork.", tagline: "From issue to merged PR" },
      { slug: "code-review", title: "Code review", summary: "Every PR gets a careful first review.", tagline: "A careful first pass on every PR" },
      { slug: "bug-triage", title: "Bug triage & on-call", summary: "Agents that pick up alerts and propose fixes.", tagline: "Alerts picked up, fixes proposed" },
      { slug: "data-analysis", title: "Data analysis", summary: "A data analyst that answers questions around the clock.", tagline: "Answers around the clock" },
    ],
  },
  {
    title: "Teams",
    pages: [
      { slug: "engineering", title: "Engineering", summary: "Ship more with the same headcount.", tagline: "Ship more with the same team" },
      { slug: "product-teams", title: "Product", summary: "Turn specs into working software faster.", tagline: "Specs into working software" },
      { slug: "operations", title: "Operations", summary: "Hand routine operational work to agents.", tagline: "Hand off routine ops work" },
      { slug: "agencies", title: "Agencies", summary: "Deliver more client work with a hybrid team.", tagline: "More client work, same team" },
    ],
  },
  {
    title: "Company",
    pages: [
      { slug: "startups", title: "Startups", summary: "Move like a team ten times your size.", tagline: "Move like a bigger team" },
      { slug: "enterprise", title: "Enterprise", summary: "Security, control, and support for large organizations.", tagline: "Security, control, and support" },
    ],
  },
  {
    title: "Programs",
    pages: [
      { slug: "open-source", title: "Open source", summary: "Multica for open-source maintainers.", tagline: "For open-source maintainers" },
      { slug: "partners", title: "Partners", summary: "Build, resell, or implement Multica with us.", tagline: "Build, resell, or implement" },
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

export const productHref = (slug: string) => `/product/${slug}`;
export const solutionHref = (slug: string) => `/solutions/${slug}`;

const toLinks = (pages: SitePage[], href: (slug: string) => string): SiteLink[] =>
  pages.map((page) => ({ label: page.title, href: href(page.slug), description: page.tagline }));

export function findPage(groups: PageGroup[], slug: string): SitePage | undefined {
  return groups.flatMap((group) => group.pages).find((page) => page.slug === slug);
}

export const TOP_LINKS: SiteLink[] = [
  { label: PLACEHOLDER_PAGES.customers.title, href: "/customers" },
  { label: PLACEHOLDER_PAGES.pricing.title, href: "/pricing" },
];

export function buildMenus(docsHref: string): SiteMenu[] {
  return [
    {
      id: "product",
      label: "Product",
      groups: [
        ...PRODUCT_GROUPS.map((group) => ({
          title: group.title,
          links: toLinks(group.pages, productHref),
        })),
        {
          title: "Works with",
          links: [
            { label: "Claude Code", href: `${docsHref}/providers`, external: true },
            { label: "Codex", href: `${docsHref}/providers`, external: true },
            { label: "Cursor", href: `${docsHref}/providers`, external: true },
            { label: "GitHub Copilot", href: `${docsHref}/providers`, external: true },
            { label: "All 27 agents", href: `${docsHref}/providers`, external: true },
          ],
        },
      ],
    },
    {
      id: "solutions",
      label: "Solutions",
      groups: SOLUTION_GROUPS.map((group) => ({
        title: group.title,
        links: toLinks(group.pages, solutionHref),
      })),
    },
    {
      id: "resources",
      label: "Resources",
      groups: [
        {
          title: "Learn",
          links: [
            { label: "Documentation", href: docsHref, external: true },
            { label: "Changelog", href: "/changelog" },
            { label: "Use cases", href: "/usecases" },
            { label: "Blog", href: "/blog" },
            { label: "Download", href: "/download" },
          ],
        },
        {
          title: "Community",
          links: [
            { label: "GitHub", href: githubUrl, external: true },
            { label: "Discord", href: discordUrl, external: true },
            { label: "X", href: twitterUrl, external: true },
          ],
        },
        {
          title: "Company",
          links: [
            { label: "About", href: "/about" },
            { label: "Careers", href: "/careers" },
            { label: "Press & brand", href: "/press" },
            { label: "Contact sales", href: "/contact-sales" },
          ],
        },
        {
          title: "Trust",
          links: [
            { label: "Security", href: "/security" },
            { label: "Licensing", href: "/licensing" },
            { label: "Privacy", href: "/privacy" },
          ],
        },
      ],
    },
  ];
}

export function buildFooterGroups(docsHref: string): SiteLinkGroup[] {
  const pick = (groups: PageGroup[], slugs: string[], href: (slug: string) => string) =>
    slugs.map((slug) => {
      const page = findPage(groups, slug);
      if (!page) throw new Error(`Unknown page in footer: ${slug}`);
      return { label: page.title, href: href(page.slug) };
    });
  return [
    {
      title: "Product",
      links: [
        ...pick(
          PRODUCT_GROUPS,
          ["cloud", "desktop", "cli", "agents", "squads", "skills", "autopilots", "runtimes", "self-hosting"],
          productHref,
        ),
        { label: PLACEHOLDER_PAGES.pricing.title, href: "/pricing" },
      ],
    },
    {
      title: "Solutions",
      links: pick(
        SOLUTION_GROUPS,
        ["software-delivery", "code-review", "bug-triage", "data-analysis", "startups", "enterprise"],
        solutionHref,
      ),
    },
    {
      title: "Resources",
      links: [
        { label: "Documentation", href: docsHref, external: true },
        { label: "Changelog", href: "/changelog" },
        { label: "Use cases", href: "/usecases" },
        { label: "Blog", href: "/blog" },
        { label: "Download", href: "/download" },
      ],
    },
    {
      title: "Company",
      links: [
        { label: "About", href: "/about" },
        { label: "Customers", href: "/customers" },
        { label: "Careers", href: "/careers" },
        { label: "Press & brand", href: "/press" },
        { label: "Contact sales", href: "/contact-sales" },
      ],
    },
    {
      title: "Trust",
      links: [
        { label: "Security", href: "/security" },
        { label: "Licensing", href: "/licensing" },
        { label: "Privacy", href: "/privacy" },
      ],
    },
  ];
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
