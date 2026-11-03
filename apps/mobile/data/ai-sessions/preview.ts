import { marked, type Token, type Tokens } from "marked";
import { readReferenceMessage } from "./references";

function plainToken(token: Token): string {
  if (token.type === "space" || token.type === "hr" || token.type === "br") return " ";
  if (token.type === "html") return token.raw.replace(/<[^>]*>/g, "");
  if (token.type === "list") return (token as Tokens.List).items.map((item) => item.tokens.map(plainToken).join("")).join(" ") + " ";
  if (token.type === "table") {
    const table = token as Tokens.Table;
    return [...table.header, ...table.rows.flat()].map((cell) => cell.tokens.map(plainToken).join("")).join(" ") + " ";
  }
  const content = "tokens" in token && Array.isArray(token.tokens)
    ? token.tokens.map(plainToken).join("")
    : "text" in token && typeof token.text === "string" ? token.text : "";
  return content + (["paragraph", "heading", "code", "blockquote"].includes(token.type) ? " " : "");
}

/** Compact AI previews keep labels and code text without Markdown decoration. */
export function aiSessionPreview(content: string): string {
  const { text, references } = readReferenceMessage(content);
  return marked.lexer(text || references.map((entry) => entry.name).join(" · ")).map(plainToken).join("").replace(/\s+/g, " ").trim();
}
