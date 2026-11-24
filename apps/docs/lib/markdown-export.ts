import {
  renderPlaceholder,
  type PlaceholderData,
} from "fumadocs-core/mdx-plugins/remark-llms.runtime";
import { i18n } from "./i18n";

// Loaded by `source.config.ts` at MDX compile time, so this module only uses
// relative imports.

/**
 * MDX components exported as placeholders and rendered to plain Markdown by
 * `MARKDOWN_RENDERERS`, so the Markdown carries the same content as the page.
 */
export const MARKDOWN_PLACEHOLDERS = [
  "Callout",
  "Steps",
  "Step",
  "Tabs",
  "Tab",
  "Accordions",
  "Accordion",
  "VideoEmbed",
  "CommunityLinks",
] as const;

const placeholderNames: ReadonlySet<string> = new Set(MARKDOWN_PLACEHOLDERS);

type MarkdownNode = { type: string; name?: string | null };

/**
 * Which authored elements reach the Markdown. Interactive `*Demo` components
 * illustrate the prose around them and have no text of their own; any other
 * JSX keeps only its children so no markup leaks into the Markdown.
 */
export function markdownElementVisibility(node: MarkdownNode): boolean | "children-only" {
  if (node.type === "mdxFlowExpression" || node.type === "mdxTextExpression") return false;
  if (node.type !== "mdxJsxFlowElement" && node.type !== "mdxJsxTextElement") return true;
  if (node.name && placeholderNames.has(node.name)) return true;
  if (node.name?.endsWith("Demo")) return false;
  return "children-only";
}

/** Plain text of a JSX attribute; expression values such as `title={<><code>x</code> y</>}` become Markdown. */
function attributeText(value: unknown): string {
  const raw =
    typeof value === "string"
      ? value
      : value && typeof value === "object" && "value" in value && typeof value.value === "string"
        ? value.value
        : "";
  return raw
    .replace(/<code>(.*?)<\/code>/g, "`$1`")
    .replace(/<\/?(?:[A-Za-z][^>]*)?>/g, "")
    .trim();
}

function quote(text: string): string {
  return text
    .split("\n")
    .map((line) => (line ? `> ${line}` : ">"))
    .join("\n");
}

const CALLOUT_LABELS: Record<string, string> = {
  info: "Note",
  warn: "Warning",
  warning: "Warning",
  error: "Error",
  success: "Tip",
};

const VIDEO_WATCH_URLS: Record<string, (id: string) => string> = {
  bilibili: (id) => `https://www.bilibili.com/video/${id}/`,
  youtube: (id) => `https://www.youtube.com/watch?v=${id}`,
};

const block = (text: string) => `\n${text.trim()}\n`;

const MARKDOWN_RENDERERS: Record<
  (typeof MARKDOWN_PLACEHOLDERS)[number],
  (data: PlaceholderData) => string
> = {
  Callout: ({ attributes, children }) => {
    const label = CALLOUT_LABELS[attributeText(attributes.type)] ?? "Note";
    return block(quote(`**${label}:** ${children.trim()}`));
  },
  Steps: ({ children }) => block(children),
  Step: ({ children }) => block(children),
  Tabs: ({ children }) => block(children),
  Tab: ({ attributes, children }) => block(`**${attributeText(attributes.value)}**\n\n${children.trim()}`),
  Accordions: ({ children }) => block(children),
  Accordion: ({ attributes, children }) => block(`### ${attributeText(attributes.title)}\n\n${children.trim()}`),
  VideoEmbed: ({ attributes }) => {
    const watchUrl = VIDEO_WATCH_URLS[attributeText(attributes.provider)];
    const id = attributeText(attributes.id);
    if (!watchUrl || !id) return "";
    return block(`[Video: ${attributeText(attributes.title) || id}](${watchUrl(id)})`);
  },
  CommunityLinks: ({ attributes }) =>
    block(
      [
        `- [Discord](https://discord.gg/W8gYBn226t): ${attributeText(attributes.discordDescription)}`,
        `- [GitHub](https://github.com/multica-ai/multica): ${attributeText(attributes.githubDescription)}`,
        `- [X](https://x.com/MulticaAI): ${attributeText(attributes.xDescription)}`,
      ].join("\n"),
    ),
};

const FENCE = /^(\s*)(`{3,}|~{3,})/;
const MARKDOWN_LINK = /(!?\[[^\]]*\]\()(\/[^)\s]*)/g;

/**
 * Rewrites root-relative links to absolute docs URLs and collapses runs of
 * blank lines. Page links stay in the page's language, as `LocaleLink` does on
 * the HTML page; images do not. Code blocks are left as written.
 */
export function absolutizeMarkdownLinks(markdown: string, lang: string, docsUrl: string): string {
  let fence: string | null = null;
  const out: string[] = [];
  for (const line of markdown.split("\n")) {
    const marker = FENCE.exec(line)?.[2];
    if (marker) {
      if (fence === null) fence = marker;
      else if (marker.startsWith(fence)) fence = null;
      out.push(line);
      continue;
    }
    if (fence !== null) {
      out.push(line);
      continue;
    }
    if (!line.trim() && !out.at(-1)?.trim()) continue;
    out.push(
      line.replace(MARKDOWN_LINK, (_, open: string, href: string) => {
        const isImage = open.startsWith("!") || href.startsWith("/images/");
        return `${open}${docsUrl}${isImage ? href : localizedPath(href, lang)}`;
      }),
    );
  }
  return out.join("\n").trim();
}

function localizedPath(href: string, lang: string): string {
  if (lang === i18n.defaultLanguage) return href === "/" ? "" : href;
  const first = href.split(/[/?#]/)[1] ?? "";
  if ((i18n.languages as readonly string[]).includes(first)) return href;
  return href === "/" ? `/${lang}` : `/${lang}${href}`;
}

/** Turns a page's processed Markdown into the clean Markdown served to agents. */
export async function renderPageMarkdown(processed: string, lang: string, docsUrl: string): Promise<string> {
  return absolutizeMarkdownLinks(await renderPlaceholder(processed, MARKDOWN_RENDERERS), lang, docsUrl);
}
