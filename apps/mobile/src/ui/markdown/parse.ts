// A deliberately small Markdown reader for issue descriptions, comments and
// agent replies: the block and inline forms those actually use. Anything it
// does not recognise stays visible as plain text.

export type Inline =
  | { kind: "text"; text: string }
  | { kind: "strong"; children: Inline[] }
  | { kind: "em"; children: Inline[] }
  | { kind: "strike"; children: Inline[] }
  | { kind: "code"; text: string }
  | { kind: "link"; url: string; children: Inline[] }
  | { kind: "image"; url: string; alt: string }
  | { kind: "mention"; type: string; id: string; label: string };

export interface ListItem {
  content: Inline[];
  /** A task list item's box; null for a plain item. */
  checked: boolean | null;
}

export type Align = "left" | "center" | "right";

export type Block =
  | { kind: "heading"; level: number; content: Inline[] }
  | { kind: "paragraph"; content: Inline[] }
  | { kind: "list"; ordered: boolean; start: number; items: ListItem[] }
  | { kind: "code"; lang: string; text: string }
  | { kind: "quote"; blocks: Block[] }
  | { kind: "table"; align: Align[]; head: Inline[][]; rows: Inline[][][] }
  | { kind: "rule" };

const MENTION = /^mention:\/\/(\w+)\/([^)\s]+)$/;

export function parseInline(source: string): Inline[] {
  const out: Inline[] = [];
  let text = "";
  const flush = () => {
    if (text) out.push({ kind: "text", text });
    text = "";
  };
  let i = 0;
  while (i < source.length) {
    const rest = source.slice(i);
    const ch = source[i]!;

    if (ch === "\\" && i + 1 < source.length && /[\\`*_[\]()#!~>-]/.test(source[i + 1]!)) {
      text += source[i + 1];
      i += 2;
      continue;
    }
    if (ch === "`") {
      const fence = /^`+/.exec(rest)![0];
      const end = source.indexOf(fence, i + fence.length);
      if (end > 0) {
        flush();
        out.push({ kind: "code", text: source.slice(i + fence.length, end).trim() });
        i = end + fence.length;
        continue;
      }
    }
    const link = /^!?\[((?:[^\][]|\[[^\]]*\])*)\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)/.exec(rest);
    if (link) {
      flush();
      const [whole, label = "", url = ""] = link;
      const mention = MENTION.exec(url);
      if (mention) {
        out.push({ kind: "mention", type: mention[1]!, id: mention[2]!, label: label.replace(/^@/, "") });
      } else if (whole.startsWith("!")) {
        out.push({ kind: "image", url, alt: label });
      } else {
        out.push({ kind: "link", url, children: parseInline(label) });
      }
      i += whole.length;
      continue;
    }
    const autolink = /^<(https?:\/\/[^>\s]+)>/.exec(rest) ?? /^(https?:\/\/[^\s<>()]+[^\s<>().,;:!?'"])/.exec(rest);
    if (autolink && (i === 0 || /[\s(]/.test(source[i - 1]!))) {
      flush();
      out.push({ kind: "link", url: autolink[1]!, children: [{ kind: "text", text: autolink[1]! }] });
      i += autolink[0].length;
      continue;
    }
    const strong = /^(\*\*|__)(?=\S)([\s\S]*?\S)\1/.exec(rest);
    if (strong) {
      flush();
      out.push({ kind: "strong", children: parseInline(strong[2]!) });
      i += strong[0].length;
      continue;
    }
    const em = /^(\*|_)(?=\S)([\s\S]*?\S)\1(?!\1)/.exec(rest);
    if (em && (ch === "*" || i === 0 || /\W/.test(source[i - 1]!))) {
      flush();
      out.push({ kind: "em", children: parseInline(em[2]!) });
      i += em[0].length;
      continue;
    }
    const strike = /^~~(?=\S)([\s\S]*?\S)~~/.exec(rest);
    if (strike) {
      flush();
      out.push({ kind: "strike", children: parseInline(strike[1]!) });
      i += strike[0].length;
      continue;
    }
    text += ch;
    i += 1;
  }
  flush();
  return out;
}

const LIST_ITEM = /^(\s*)([-*+]|\d{1,9}[.)])\s+(.*)$/;
const TASK = /^\[([ xX])\]\s+/;
const TABLE_RULE = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/;

function tableCells(line: string): string[] {
  const cells: string[] = [];
  let cell = "";
  let code = false;
  let body = line.trim().replace(/^\|/, "");
  if (body.endsWith("|") && !body.endsWith("\\|")) body = body.slice(0, -1);
  for (let i = 0; i < body.length; i += 1) {
    const ch = body[i]!;
    if (ch === "\\" && body[i + 1] === "|") {
      cell += "|";
      i += 1;
    } else if (ch === "`") {
      code = !code;
      cell += ch;
    } else if (ch === "|" && !code) {
      cells.push(cell.trim());
      cell = "";
    } else {
      cell += ch;
    }
  }
  cells.push(cell.trim());
  return cells;
}

function startsTable(lines: readonly string[], i: number): boolean {
  const line = lines[i]!;
  const next = lines[i + 1];
  return line.includes("|") && next !== undefined && next.includes("-") && TABLE_RULE.test(next) && tableCells(next).length === tableCells(line).length;
}

function listItem(text: string): ListItem {
  const task = TASK.exec(text);
  return task ? { content: parseInline(text.slice(task[0].length)), checked: task[1] !== " " } : { content: parseInline(text), checked: null };
}

export function parseMarkdown(source: string): Block[] {
  const lines = source.replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let i = 0;

  while (i < lines.length) {
    const line = lines[i]!;
    if (!line.trim()) {
      i += 1;
      continue;
    }

    const fence = /^\s*(`{3,}|~{3,})\s*([\w+-]*)/.exec(line);
    if (fence) {
      const body: string[] = [];
      i += 1;
      while (i < lines.length && !lines[i]!.trim().startsWith(fence[1]!)) body.push(lines[i++]!);
      i += 1;
      blocks.push({ kind: "code", lang: fence[2] ?? "", text: body.join("\n") });
      continue;
    }

    const heading = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/.exec(line);
    if (heading) {
      blocks.push({ kind: "heading", level: heading[1]!.length, content: parseInline(heading[2]!) });
      i += 1;
      continue;
    }

    if (/^\s{0,3}([-*_])(\s*\1){2,}\s*$/.test(line)) {
      blocks.push({ kind: "rule" });
      i += 1;
      continue;
    }

    if (/^\s{0,3}>/.test(line)) {
      const body: string[] = [];
      while (i < lines.length && /^\s{0,3}>/.test(lines[i]!)) body.push(lines[i++]!.replace(/^\s{0,3}>\s?/, ""));
      blocks.push({ kind: "quote", blocks: parseMarkdown(body.join("\n")) });
      continue;
    }

    const item = LIST_ITEM.exec(line);
    if (item) {
      const ordered = /\d/.test(item[2]!);
      const items: string[] = [];
      while (i < lines.length) {
        const current = lines[i]!;
        const next = LIST_ITEM.exec(current);
        const sibling = next !== null && next[1]!.length <= item[1]!.length + 1;
        if (sibling && /\d/.test(next[2]!) === ordered) {
          items.push(next[3]!);
        } else if (!sibling && current.trim() && /^\s+/.test(current)) {
          items[items.length - 1] += `\n${current.trim()}`;
        } else {
          break;
        }
        i += 1;
      }
      blocks.push({ kind: "list", ordered, start: ordered ? Number.parseInt(item[2]!, 10) : 1, items: items.map(listItem) });
      continue;
    }

    if (startsTable(lines, i)) {
      const head = tableCells(line);
      const align = tableCells(lines[i + 1]!).map((cell): Align => (cell.endsWith(":") ? (cell.startsWith(":") ? "center" : "right") : "left"));
      const rows: Inline[][][] = [];
      i += 2;
      while (i < lines.length && lines[i]!.trim() && lines[i]!.includes("|")) {
        const cells = tableCells(lines[i++]!);
        rows.push(head.map((_, column) => parseInline(cells[column] ?? "")));
      }
      blocks.push({ kind: "table", align, head: head.map((cell) => parseInline(cell)), rows });
      continue;
    }

    const body: string[] = [];
    while (
      i < lines.length &&
      lines[i]!.trim() &&
      !/^\s*(`{3,}|~{3,}|#{1,6}\s|>)/.test(lines[i]!) &&
      !LIST_ITEM.test(lines[i]!) &&
      !(body.length > 0 && startsTable(lines, i))
    )
      body.push(lines[i++]!.trim());
    blocks.push({ kind: "paragraph", content: parseInline(body.join("\n")) });
  }
  return blocks;
}

/** Plain text of some inline content, for previews and accessibility labels. */
export function inlineText(content: readonly Inline[]): string {
  return content
    .map((node) => {
      switch (node.kind) {
        case "text":
        case "code":
          return node.text;
        case "image":
          return node.alt;
        case "mention":
          return node.type === "issue" ? node.label : `@${node.label}`;
        default:
          return inlineText(node.children);
      }
    })
    .join("");
}

function blockText(block: Block): string {
  switch (block.kind) {
    case "heading":
    case "paragraph":
      return inlineText(block.content);
    case "list":
      return block.items.map((item) => inlineText(item.content)).join(" ");
    case "code":
      return block.text;
    case "quote":
      return block.blocks.map(blockText).join(" ");
    case "table":
      return [block.head, ...block.rows].map((row) => row.map(inlineText).join(" ")).join(" ");
    default:
      return "";
  }
}

/** One-line plain preview of a Markdown body. */
export function markdownPreview(source: string): string {
  return parseMarkdown(source).map(blockText).join(" ").replace(/\s+/g, " ").trim();
}
