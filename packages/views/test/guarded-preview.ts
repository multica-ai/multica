import { PREVIEW_POLICY_HEAD } from "../editor/utils/html-egress-guard";

const DOCTYPE_RE = /^\s*<!doctype[^>]*>/i;

/**
 * What a preview frame shows: the author's document inside a guarded srcdoc
 * (html-egress-guard), without the policy in front of it. `null` when the
 * srcdoc is not a guarded one, or the document lost its policy.
 */
export function guardedContent(frame: Element | null | undefined): string | null {
  const srcdoc = frame?.getAttribute("srcdoc");
  if (!srcdoc) return null;
  const inner = new DOMParser()
    .parseFromString(srcdoc, "text/html")
    .getElementById("f")
    ?.getAttribute("srcdoc");
  if (inner == null) return null;
  const doctype = DOCTYPE_RE.exec(inner)?.[0] ?? "";
  const rest = inner.slice(doctype.length);
  return rest.startsWith(PREVIEW_POLICY_HEAD) ? doctype + rest.slice(PREVIEW_POLICY_HEAD.length) : null;
}
