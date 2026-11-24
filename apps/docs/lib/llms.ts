import { llms } from "fumadocs-core/source/llms";
import type { Node as PageTreeNode } from "fumadocs-core/page-tree";
import { source } from "@/lib/source";
import { i18n, type Lang } from "@/lib/i18n";
import { absoluteDocsUrl, DOCS_BASE_PATH, SITE_ORIGIN } from "@/lib/site";
import { renderPageMarkdown } from "@/lib/markdown-export";
import { localeLabels } from "@/lib/translations";

export const DOCS_URL = `${SITE_ORIGIN}${DOCS_BASE_PATH}`;

type DocsPage = NonNullable<ReturnType<typeof source.getPage>>;

/**
 * Pages listed first in `llms.txt` for agents that install or operate Multica.
 * `self-hosting-ai` is written for agents only, so it is left out of the
 * sidebar (meta.json); this list, `llms-full.txt` and its `.md` URL serve it.
 */
const AGENT_START_SLUGS = ["self-hosting-ai", "cloud-quickstart", "install-agent-runtime", "cli"];

function langPrefix(lang: string): string {
  return lang === i18n.defaultLanguage ? "" : `/${lang}`;
}

/** Absolute URL of a page's Markdown, e.g. `/docs/agents.md`; the home page is `/docs/index.md`. */
export function markdownUrl(page: Pick<DocsPage, "slugs">, lang: string): string {
  const path = page.slugs.length === 0 ? "/index" : `/${page.slugs.join("/")}`;
  return `${DOCS_URL}${langPrefix(lang)}${path}.md`;
}

function htmlToMarkdownUrl(relative: string, lang: string): string {
  const prefix = langPrefix(lang);
  const path = prefix && relative.startsWith(prefix) ? relative.slice(prefix.length) : relative;
  return `${DOCS_URL}${prefix}${path === "/" || path === "" ? "/index" : path}.md`;
}

export async function pageMarkdown(page: DocsPage, lang: string): Promise<string> {
  const body = await renderPageMarkdown(await page.data.getText("processed"), lang, DOCS_URL);
  const description = page.data.description ? `> ${page.data.description}\n\n` : "";
  return `# ${page.data.title}\n\n${description}${body}\n`;
}

/** Pages in sidebar order, then any page the sidebar does not list. */
export function orderedPages(lang: Lang): DocsPage[] {
  const pages: DocsPage[] = [];
  const seen = new Set<string>();
  const add = (page: DocsPage | undefined) => {
    if (!page || seen.has(page.url)) return;
    seen.add(page.url);
    pages.push(page);
  };
  const walk = (nodes: PageTreeNode[]) => {
    for (const node of nodes) {
      if (node.type === "page") add(source.getNodePage(node, lang));
      if (node.type === "folder") {
        if (node.index) add(source.getNodePage(node.index, lang));
        walk(node.children);
      }
    }
  };
  walk(source.getPageTree(lang).children);
  for (const page of source.getPages(lang)) add(page);
  return pages;
}

export async function docsFullMarkdown(lang: Lang): Promise<string> {
  const sections = await Promise.all(
    orderedPages(lang).map(async (page) => {
      const markdown = await pageMarkdown(page, lang);
      return markdown.replace(/^(# .+\n)/, `$1\nURL: ${absoluteDocsUrl(page.url)}\n`);
    }),
  );
  return `${sections.join("\n---\n\n")}`;
}

export async function docsIndexMarkdown(lang: Lang): Promise<string> {
  const tree = await llms(source).index(lang);
  const sections = tree
    .split("\n")
    .slice(1)
    .filter((line) => !line.startsWith("> "))
    .map((line) => line.replace(/^- \*\*(.+)\*\*$/, "## $1\n"))
    .join("\n")
    .replace(/\]\((\/[^)]*)\)/g, (_, href: string) => `](${htmlToMarkdownUrl(href, lang)})`)
    .replace(/\n{3,}/g, "\n\n")
    .trim();

  const agentPages = AGENT_START_SLUGS.flatMap((slug) => {
    const page = source.getPage([slug], lang);
    if (!page) return [];
    const description = page.data.description ? `: ${page.data.description}` : "";
    return [`- [${page.data.title}](${markdownUrl(page, lang)})${description}`];
  });

  const otherLanguages = i18n.languages
    .filter((other) => other !== lang)
    .map((other) => `[${localeLabels[other]}](${DOCS_URL}${langPrefix(other)}/llms.txt)`)
    .join(" · ");

  return [
    "# Multica Docs",
    "",
    "> Multica is a source-available workspace where people assign issues to AI coding agents the way they would to a teammate. Agents run on runtimes you control, with the coding tools you already use, and report progress back on the issue. Use Multica Cloud, the desktop app, or self-host it.",
    "",
    `Every page below is Markdown. On the site, append \`.md\` to a page URL or request it with \`Accept: text/markdown\`. The whole documentation is in [one file](${DOCS_URL}${langPrefix(lang)}/llms-full.txt). Other languages: ${otherLanguages}.`,
    "",
    "## Start here (for AI agents)",
    "",
    ...agentPages,
    "",
    sections,
    "",
  ].join("\n");
}

export function markdownResponse(body: string, init: { status?: number; canonical?: string } = {}): Response {
  const headers = new Headers({
    "Content-Type": "text/markdown; charset=utf-8",
    Vary: "Accept",
  });
  if (init.canonical) headers.set("Link", `<${init.canonical}>; rel="canonical"`);
  return new Response(body, { status: init.status ?? 200, headers });
}
