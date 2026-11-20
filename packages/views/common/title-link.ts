import { escapeMarkdownLabel } from "../editor/utils/escape-markdown-label";

/**
 * The two clipboard representations of a title link: Markdown for plain-text
 * targets, and an HTML anchor for rich-text targets. Both carry the same
 * visible label and destination.
 */
export interface TitleLinkPayload {
  text: string;
  html: string;
}

export function titleLinkPayload(label: string, url: string): TitleLinkPayload {
  return {
    text: `[${escapeMarkdownLabel(label).replace(/</g, "\\<")}](${url})`,
    html: `<a href="${escapeHtmlAttribute(url)}">${escapeHtmlText(label)}</a>`,
  };
}

function escapeHtmlText(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function escapeHtmlAttribute(value: string): string {
  return escapeHtmlText(value).replace(/'/g, "&#39;");
}
