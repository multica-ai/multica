/** Convert a Jira description/comment body to plain text. Jira Cloud bodies are
 *  ADF node trees; older payloads may already be strings. We extract readable
 *  text rather than full Markdown fidelity — enough for a synced description.
 *  Unknown node types are walked for their text children, never dropped. */
export function adfToText(body: unknown): string {
  if (body == null) return "";
  if (typeof body === "string") return body;
  if (typeof body !== "object") return "";
  return renderBlocks(asNode(body).content).trim();
}

interface AdfNode {
  type?: string;
  text?: string;
  content?: unknown[];
}

function asNode(value: unknown): AdfNode {
  return (value ?? {}) as AdfNode;
}

function renderBlocks(content: unknown[] | undefined): string {
  if (!Array.isArray(content)) return "";
  const blocks: string[] = [];
  for (const raw of content) {
    const node = asNode(raw);
    switch (node.type) {
      case "paragraph":
      case "heading":
        blocks.push(renderInline(node.content));
        break;
      case "bulletList":
      case "orderedList":
        blocks.push(renderList(node.content));
        break;
      case "text":
        blocks.push(node.text ?? "");
        break;
      default:
        // Unknown block: keep its text by recursing into children.
        blocks.push(renderBlocks(node.content));
    }
  }
  return blocks.filter((b) => b.length > 0).join("\n\n");
}

function renderList(items: unknown[] | undefined): string {
  if (!Array.isArray(items)) return "";
  return items
    .map((raw) => `- ${renderBlocks(asNode(raw).content).replace(/\n+/g, " ").trim()}`)
    .join("\n");
}

function renderInline(content: unknown[] | undefined): string {
  if (!Array.isArray(content)) return "";
  return content.map((raw) => asNode(raw).text ?? "").join("");
}
