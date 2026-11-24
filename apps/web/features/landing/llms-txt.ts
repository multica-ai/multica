import { parseInlineLinks } from "./components/inline-links";
import { githubUrl } from "./components/shared";
import { createEnDict } from "./i18n/en";
import type { LandingDict, MetaPage } from "./i18n/types";
import { isLiveRoute, siteRoutes } from "./site/sitemap";
import { SITE_ORIGIN } from "./structured-data";

const DOCS_URL = `${SITE_ORIGIN}/docs`;

/** Marketing routes that have their own title and description. */
const ROUTE_META: Readonly<Record<string, MetaPage>> = {
  "/about": "about",
  "/changelog": "changelog",
  "/contact-sales": "contactSales",
  "/download": "download",
  "/licensing": "licensing",
  "/privacy": "privacy",
};

/** Dictionary copy as Markdown, with root-relative links made absolute. */
function markdownText(text: string): string {
  return parseInlineLinks(text)
    .map((segment) => {
      if (segment.type === "text") return segment.text;
      const href = segment.href.startsWith("/") ? `${SITE_ORIGIN}${segment.href}` : segment.href;
      return `[${segment.label}](${href})`;
    })
    .join("");
}

/**
 * `/llms.txt` for the marketing site (llmstxt.org): what Multica is, how to
 * start, and where the Markdown docs live. Every product fact comes from the
 * English dictionary the pages render, so the two cannot drift apart.
 */
export function buildLlmsTxt(
  dict: LandingDict = createEnDict(),
  isLive: (href: string) => boolean = isLiveRoute,
): string {
  const { meta, home, faq } = dict;
  const capabilities = Object.values(home.features.items).map((item) => `- **${item.title}**: ${item.body}`);
  const pages = siteRoutes()
    .filter((route) => isLive(route) && ROUTE_META[route])
    .map((route) => {
      const page = meta[ROUTE_META[route]!];
      return `- [${page.title}](${SITE_ORIGIN}${route}): ${page.description}`;
    });
  const questions = faq.items.flatMap((item) => [`### ${item.question}`, "", markdownText(item.answer), ""]);

  return [
    "# Multica",
    "",
    `> ${meta.home.description}`,
    "",
    home.hero.lead,
    "",
    "## Core capabilities",
    "",
    ...capabilities,
    "",
    "## Get started",
    "",
    `- **Multica Cloud**: Hosted by Multica. Sign in at ${SITE_ORIGIN} and follow the [Quickstart](${DOCS_URL}/cloud-quickstart.md).`,
    `- **Desktop app**: ${meta.download.description} [Download](${SITE_ORIGIN}/download) · [Desktop docs](${DOCS_URL}/desktop-app.md)`,
    `- **Self-host**: Run Multica on your own infrastructure with Docker Compose or Kubernetes. [Self-host quickstart](${DOCS_URL}/self-host-quickstart.md)`,
    `- **For AI agents installing Multica**: [Self-hosting setup for AI agents](${DOCS_URL}/self-hosting-ai.md) lists the exact commands to run.`,
    "",
    "## Documentation",
    "",
    `- [Docs index](${DOCS_URL}/llms.txt): Every documentation page, as Markdown.`,
    `- [Full documentation](${DOCS_URL}/llms-full.txt): All English docs in one Markdown file.`,
    `- [Supported AI coding tools](${DOCS_URL}/providers.md): The coding tools Multica runs and what each supports.`,
    `- [Source code](${githubUrl}): The public repository.`,
    "",
    "## FAQ",
    "",
    ...questions,
    "## Pages",
    "",
    ...pages,
    "",
  ].join("\n");
}
