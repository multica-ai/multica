/**
 * IssueIdentifierAutolink — Linear-style live autolinking of bare issue
 * identifiers (e.g. `MUL-123`) in the editable editor.
 *
 * When the user finishes a bare identifier by typing a boundary character
 * (space / punctuation) after it, or pastes text containing identifiers, the
 * completed token is resolved against the workspace and — on an exact match —
 * replaced with a real `issue` mention node. On save the mention serialises to
 * the canonical `[MUL-123](mention://issue/<uuid>)`.
 *
 * Resolution is async, so this is NOT a synchronous Tiptap InputRule/PasteRule.
 * A ProseMirror plugin captures completed candidates from user transactions
 * (never from programmatic setContent — so merely opening existing content does
 * not rewrite it) into plugin state; a plugin `view` drains them, resolves via
 * the injected resolver, and — after re-scanning the CURRENT doc so stale
 * positions can't mis-replace — swaps the text for a mention node. Misses and
 * errors are cached so a token is resolved at most once per editing session.
 *
 * The resolver is injected (a ref) from the editor setup layer, which owns
 * React Query + workspace context; this extension never touches React hooks.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type { NodeType } from "@tiptap/pm/model";
import type { RefObject } from "react";

export interface ResolvedIssueRef {
  /** Issue UUID. */
  id: string;
  /** Canonical identifier as returned by the server, e.g. "MUL-123". */
  identifier: string;
}

export type IssueIdentifierResolver = (
  identifier: string,
) => Promise<ResolvedIssueRef | null>;

export interface IssueIdentifierAutolinkOptions {
  /**
   * Ref to the resolver. A ref (not a bare function) so the editor is created
   * once while the resolver always reads the latest workspace context.
   */
  resolveRef: RefObject<IssueIdentifierResolver | undefined>;
}

// Boundary-delimited, case-sensitive identifier — same shape as the readonly
// detector in @multica/ui/markdown.
const IDENTIFIER_RE = /(?<![A-Za-z0-9_-])([A-Z][A-Z0-9]*-\d+)(?![A-Za-z0-9_-])/g;

// Marks our own replacement transactions so `apply` doesn't treat them as user
// input and re-scan them.
const META_APPLIED = "issueIdentifierAutolinkApplied";

interface Candidate {
  identifier: string;
  from: number;
  to: number;
}

interface AutolinkPluginState {
  pending: Candidate[];
}

const pluginKey = new PluginKey<AutolinkPluginState>("issueIdentifierAutolink");

/** True when the text node at `parent` must be skipped (code contexts). */
function isSkippedTextNode(
  marks: readonly { type: { name: string } }[],
  parent: { type: { name: string } } | null,
): boolean {
  if (marks.some((m) => m.type.name === "code")) return true;
  if (parent && parent.type.name === "codeBlock") return true;
  return false;
}

/**
 * Collect complete, standalone identifier tokens across the doc. "Complete"
 * means a trailing boundary character exists within the same text node, i.e.
 * the user finished the token — a token still being typed at the very end of a
 * text run is skipped.
 */
function collectCandidates(state: EditorState): Candidate[] {
  const out: Candidate[] = [];
  state.doc.descendants((node, pos, parent) => {
    if (!node.isText || !node.text) return;
    if (isSkippedTextNode(node.marks, parent)) return;
    const text = node.text;
    IDENTIFIER_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = IDENTIFIER_RE.exec(text)) !== null) {
      const identifier = m[1];
      if (!identifier) continue;
      const localEnd = m.index + identifier.length;
      // Require a trailing boundary char inside this text node.
      if (localEnd >= text.length) continue;
      out.push({ identifier, from: pos + m.index, to: pos + localEnd });
    }
  });
  return out;
}

/** Absolute [from,to) range spanned by a transaction's changes, or null. */
function changedRange(tr: Transaction): { from: number; to: number } | null {
  let from = Infinity;
  let to = -Infinity;
  tr.mapping.maps.forEach((map) => {
    map.forEach((_oldStart, _oldEnd, newStart, newEnd) => {
      from = Math.min(from, newStart);
      to = Math.max(to, newEnd);
    });
  });
  if (to < 0) return null;
  return { from, to };
}

/**
 * Candidates introduced by a single user transaction:
 *   - paste: every complete identifier inside the pasted range
 *   - typing: the token immediately before the cursor, i.e. the "previous
 *     token" completed by the boundary char that was just typed
 */
function candidatesFromUserTransaction(
  tr: Transaction,
  state: EditorState,
): Candidate[] {
  const isPaste =
    tr.getMeta("paste") === true || tr.getMeta("uiEvent") === "paste";

  if (isPaste) {
    const range = changedRange(tr);
    if (!range) return [];
    return collectCandidates(state).filter(
      (c) => c.to > range.from && c.from < range.to,
    );
  }

  // Typing: the identifier whose end sits exactly before the caret is the token
  // the just-typed boundary character completed.
  const caret = state.selection.from;
  const target = collectCandidates(state).find((c) => c.to === caret - 1);
  return target ? [target] : [];
}

/**
 * Replace every current complete occurrence of `identifier` with a mention
 * node for `ref`. Re-scans the live doc (positions may have shifted since the
 * candidate was captured) and edits right-to-left so earlier ranges stay valid.
 */
function replaceIdentifierOccurrences(
  view: EditorView,
  mentionType: NodeType,
  identifier: string,
  ref: ResolvedIssueRef,
): void {
  const { state } = view;
  const ranges: { from: number; to: number }[] = [];
  state.doc.descendants((node, pos, parent) => {
    if (!node.isText || !node.text) return;
    if (isSkippedTextNode(node.marks, parent)) return;
    const text = node.text;
    IDENTIFIER_RE.lastIndex = 0;
    let m: RegExpExecArray | null;
    while ((m = IDENTIFIER_RE.exec(text)) !== null) {
      if (m[1] !== identifier) continue;
      const localEnd = m.index + identifier.length;
      if (localEnd >= text.length) continue;
      ranges.push({ from: pos + m.index, to: pos + localEnd });
    }
  });
  if (ranges.length === 0) return;

  ranges.sort((a, b) => b.from - a.from);
  const { tr } = state;
  for (const r of ranges) {
    tr.replaceWith(
      r.from,
      r.to,
      mentionType.create({ id: ref.id, label: ref.identifier, type: "issue" }),
    );
  }
  tr.setMeta(META_APPLIED, true);
  view.dispatch(tr);
}

export function createIssueIdentifierAutolinkExtension(
  options: IssueIdentifierAutolinkOptions,
): Extension {
  return Extension.create({
    name: "issueIdentifierAutolink",

    addProseMirrorPlugins() {
      const mentionType = this.editor.schema.nodes.mention;
      if (!mentionType) return [];
      const resolveRef = options.resolveRef;

      return [
        new Plugin<AutolinkPluginState>({
          key: pluginKey,
          state: {
            init: () => ({ pending: [] }),
            apply(tr, _value, _oldState, newState): AutolinkPluginState {
              // Only real user edits seed candidates: skip non-doc changes,
              // programmatic setContent (`preventUpdate`), and our own
              // replacement transactions. This is why merely opening existing
              // content never rewrites it.
              if (
                !tr.docChanged ||
                tr.getMeta("preventUpdate") ||
                tr.getMeta(META_APPLIED)
              ) {
                return { pending: [] };
              }
              return { pending: candidatesFromUserTransaction(tr, newState) };
            },
          },
          view(view) {
            const inFlight = new Set<string>();
            const missed = new Set<string>();
            let destroyed = false;

            return {
              update() {
                if (destroyed) return;
                const resolve = resolveRef.current;
                if (!resolve) return;
                const pluginState = pluginKey.getState(view.state);
                if (!pluginState || pluginState.pending.length === 0) return;

                for (const candidate of pluginState.pending) {
                  const id = candidate.identifier;
                  if (inFlight.has(id) || missed.has(id)) continue;
                  inFlight.add(id);
                  Promise.resolve(resolve(id))
                    .then((ref) => {
                      inFlight.delete(id);
                      if (destroyed) return;
                      if (!ref) {
                        missed.add(id);
                        return;
                      }
                      replaceIdentifierOccurrences(view, mentionType, id, ref);
                    })
                    .catch(() => {
                      // Treat resolve failures as a miss so we don't spin.
                      inFlight.delete(id);
                      missed.add(id);
                    });
                }
              },
              destroy() {
                destroyed = true;
              },
            };
          },
        }),
      ];
    },
  });
}
