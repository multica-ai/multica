interface MarkdownNode {
  type: string;
  value?: string;
  url?: string;
  identifier?: string;
  children?: MarkdownNode[];
}

function hasControlCharacters(value: string): boolean {
  return [...value].some((character) => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
}

/** Resolve only explicit local paths and file URLs; the desktop reader enforces access. */
export function localResourceTarget(value: string): { path: string; line?: number } | null {
  const href = value.trim();
  if (!href || hasControlCharacters(value)) return null;
  try {
    let path: string;
    let line: number | undefined;
    if (/^file:/i.test(href)) {
      const url = new URL(href);
      if ((url.hostname && url.hostname !== "localhost") || url.username || url.password || url.port || url.search) return null;
      path = decodeURIComponent(url.pathname);
      if (/^\/[a-z]:\//i.test(path)) path = path.slice(1);
      if (url.hash) {
        const location = /^#L([1-9]\d*)(?:C\d+)?(?:-L?\d+)?$/i.exec(url.hash);
        if (!location) return null;
        line = Number(location[1]);
      }
    } else {
      if (/^[a-z][\w+.-]*:/i.test(href) && !/^[a-z]:[\\/]/i.test(href)) return null;
      path = decodeURIComponent(href);
      const location = /(?::([1-9]\d*)(?::\d+)?|#L([1-9]\d*)(?:C\d+)?(?:-L?\d+)?)$/i.exec(path);
      if (location) { line = Number(location[1] ?? location[2]); path = path.slice(0, location.index); }
    }
    if (!path || path.startsWith("//") || hasControlCharacters(path) || /[?#]/u.test(path)) return null;
    if (path.includes("\\")) {
      if (!/^[a-z]:[\\/]/i.test(path)) return null;
      path = path.replaceAll("\\", "/");
    }
    const explicit = /^(?:\/|\.\.?\/|[a-z]:\/)/i.test(path);
    const relativeFile = !/\s/.test(path) && /^(?:[\p{L}\p{N}_@.-]+\/)*[\p{L}\p{N}_@().-]+\.[a-z0-9]{1,12}$/iu.test(path);
    if ((!explicit && !relativeFile) || (line !== undefined && !Number.isSafeInteger(line))) return null;
    return { path, ...(line !== undefined ? { line } : {}) };
  } catch { return null; }
}

export function previewResourceHref(value: string): string | null {
  if (/^https?:\/\//i.test(value)) {
    try { const url = new URL(value); return url.username || url.password ? null : url.href; }
    catch { return null; }
  }
  const target = localResourceTarget(value);
  return target && /^(?:\/|[a-z]:\/)/i.test(target.path)
    ? encodeURI(target.path) + (target.line === undefined ? "" : `:${target.line}`)
    : null;
}

/** Relative file lookup remains available to explicit file-opening workflows. */
export function isRelativeResourceHref(value: string): boolean {
  const target = localResourceTarget(value);
  if (target) return !/^(?:\/|[a-z]:\/)/i.test(target.path);
  const href = value.trim();
  return /^[a-z]:(?![\\/])/i.test(href) || !/^(?:\/|#|[a-z][\w+.-]*:)/i.test(href);
}

// Code fences, existing links and image destinations are not scanned as prose.
const candidates = /file:\/\/[^\s<>"'`，。；：！？、【】《》]+|(?:\/(?!\/)|\.\.?\/|[\p{L}\p{N}_@-][\p{L}\p{N}_@.-]*\/)[^\s<>"'`，。；：！？、【】《》]+/giu;
function proseLinks(value: string): MarkdownNode[] {
  const nodes: MarkdownNode[] = [];
  let end = 0;
  for (const match of value.matchAll(candidates)) {
    const start = match.index;
    if (start && /[\p{L}\p{N}_/:\\@.-]/u.test(value[start - 1]!)) continue;
    let label = match[0].replace(/[.,;:!?]+$/, "");
    while (/[)\]}）]$/.test(label)) {
      const close = label.at(-1)!;
      const open = ({ ")": "(", "]": "[", "}": "{", "）": "（" } as Record<string, string>)[close]!;
      if (label.split(close).length <= label.split(open).length) break;
      label = label.slice(0, -1);
    }
    // A lone "/word" in prose is a slash command far more often than a root-level path.
    if (/^\/[\p{L}\p{N}_-]+$/u.test(label)) continue;
    const href = previewResourceHref(label);
    if (!href) continue;
    if (start > end) nodes.push({ type: "text", value: value.slice(end, start) });
    nodes.push({ type: "link", url: href, children: [{ type: "text", value: label }] });
    end = start + label.length;
  }
  if (end < value.length) nodes.push({ type: "text", value: value.slice(end) });
  return nodes;
}

/** Linkify resource addresses before href sanitization, for settled and streaming replies. */
export function remarkMessageResourceLinks() {
  return (tree: MarkdownNode): void => {
    const references = new Set<string>();
    const definitions: MarkdownNode[] = [];
    const visit = (node: MarkdownNode): void => {
      if (node.type === "link") { node.url = previewResourceHref(node.url ?? "") ?? node.url; return; }
      if (node.type === "definition") { definitions.push(node); return; }
      if (node.type === "linkReference") { if (node.identifier) references.add(node.identifier); return; }
      if (["code", "html", "image", "imageReference"].includes(node.type) || !node.children) return;
      node.children = node.children.flatMap((child) => {
        if (child.type === "text" && child.value) return proseLinks(child.value);
        if (child.type === "inlineCode" && child.value && !/\s/.test(child.value)) {
          const href = previewResourceHref(child.value);
          if (href) return [{ type: "link", url: href, children: [child] }];
        }
        visit(child); return [child];
      });
    };
    visit(tree);
    for (const definition of definitions) if (references.has(definition.identifier ?? "")) definition.url = previewResourceHref(definition.url ?? "") ?? definition.url;
  };
}
