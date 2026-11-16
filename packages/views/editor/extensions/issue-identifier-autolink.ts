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
 * A ProseMirror plugin captures the SPECIFIC candidate range(s) introduced by a
 * user transaction — the token before the caret when typing, or the tokens
 * inside the pasted slice — into plugin state, mapping each range forward on
 * every subsequent transaction so it stays current across the async gap. A
 * plugin `view` resolves the captured identifiers and replaces ONLY those
 * mapped ranges (after re-verifying the range still holds exactly that
 * identifier, with intact boundaries and no code/link mark). It never rescans
 * the document by identifier, so a pre-existing or out-of-paste occurrence of
 * the same identifier that the user did not touch is left untouched. Misses and
 * errors are cached so a token is resolved at most once per editing session.
 *
 * Only genuine user edits seed candidates (never programmatic setContent), so
 * merely opening existing content does not rewrite it. The resolver is injected
 * (a ref) from the editor setup layer, which owns React Query + workspace
 * context; this extension never touches React hooks.
 */
import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import type { EditorState, Transaction } from "@tiptap/pm/state";
import type { EditorView } from "@tiptap/pm/view";
import type { Mark, Node as PMNode, NodeType } from "@tiptap/pm/model";
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
const BOUNDARY_RE = /[A-Za-z0-9_-]/;

// Set on our own replacement transactions with the list of pending keys they
// consume, so `apply` drops them and never treats the replacement as user input.
const META_REMOVE = "issueIdentifierAutolinkRemove";

interface Candidate {
  identifier: string;
  from: number;
  to: number;
}

interface PendingCandidate extends Candidate {
  /** Unique per-plugin key correlating capture → async resolve → replace. */
  key: number;
}

interface AutolinkPluginState {
  pending: PendingCandidate[];
  seq: number;
}

const pluginKey = new PluginKey<AutolinkPluginState>("issueIdentifierAutolink");

/** Text nodes that must never be autolinked (code and explicit-link contexts). */
function isSkippedTextNode(
  marks: readonly Mark[],
  parent: PMNode | null,
): boolean {
  if (marks.some((m) => m.type.name === "code" || m.type.name === "link")) {
    return true;
  }
  if (parent && parent.type.name === "codeBlock") return true;
  return false;
}

/**
 * Complete, standalone identifier tokens whose range intersects
 * [rangeFrom, rangeTo). "Complete" means a trailing boundary char exists within
 * the same text node — a token still being typed at the very end is skipped.
 */
function collectCandidates(
  state: EditorState,
  rangeFrom = 0,
  rangeTo = Number.POSITIVE_INFINITY,
): Candidate[] {
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
      if (localEnd >= text.length) continue; // no trailing boundary in-node
      const from = pos + m.index;
      const to = pos + localEnd;
      if (to <= rangeFrom || from >= rangeTo) continue; // outside window
      out.push({ identifier, from, to });
    }
  });
  return out;
}

/** Absolute [from,to) range spanned by a transaction's changes, or null. */
function changedRange(tr: Transaction): { from: number; to: number } | null {
  let from = Number.POSITIVE_INFINITY;
  let to = -1;
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
 *   - paste: every complete identifier INSIDE the pasted range only
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
    return collectCandidates(state, range.from, range.to);
  }

  const caret = state.selection.from;
  const target = collectCandidates(state).find((c) => c.to === caret - 1);
  return target ? [target] : [];
}

/**
 * Verify a mapped candidate range still holds exactly `identifier` as plain
 * text with intact boundaries — the async safety net before replacing.
 */
function rangeStillPlainIdentifier(
  state: EditorState,
  from: number,
  to: number,
  identifier: string,
): boolean {
  const size = state.doc.content.size;
  if (from < 0 || to > size || from >= to) return false;
  if (state.doc.textBetween(from, to) !== identifier) return false;

  let ok = true;
  state.doc.nodesBetween(from, to, (node, _pos, parent) => {
    if (node.isText && isSkippedTextNode(node.marks, parent)) ok = false;
  });
  if (!ok) return false;

  const before = from > 0 ? state.doc.textBetween(from - 1, from) : "";
  const after = to < size ? state.doc.textBetween(to, to + 1) : "";
  if (before && BOUNDARY_RE.test(before)) return false;
  if (after && BOUNDARY_RE.test(after)) return false;
  return true;
}

export function createIssueIdentifierAutolinkExtension(
  options: IssueIdentifierAutolinkOptions,
): Extension {
  return Extension.create({
    name: "issueIdentifierAutolink",

    addProseMirrorPlugins() {
      const maybeMentionType = this.editor.schema.nodes.mention;
      if (!maybeMentionType) return [];
      // Alias to a definitely-defined const so the narrowing survives into the
      // nested plugin `view`/`applyReady` closures.
      const mentionType: NodeType = maybeMentionType;
      const resolveRef = options.resolveRef;

      return [
        new Plugin<AutolinkPluginState>({
          key: pluginKey,
          state: {
            init: () => ({ pending: [], seq: 0 }),
            apply(tr, value, _oldState, newState): AutolinkPluginState {
              let pending = value.pending;
              let seq = value.seq;

              // 1. Keep captured ranges current across every doc change, and
              //    drop ranges the edit invalidated (collapsed to empty).
              if (tr.docChanged && pending.length > 0) {
                pending = pending
                  .map((c) => ({
                    ...c,
                    from: tr.mapping.map(c.from, 1),
                    to: tr.mapping.map(c.to, -1),
                  }))
                  .filter((c) => c.from < c.to);
              }

              // 2. Drop candidates consumed by our own replacement transaction.
              const remove = tr.getMeta(META_REMOVE) as number[] | undefined;
              if (remove && remove.length > 0) {
                const rm = new Set(remove);
                pending = pending.filter((c) => !rm.has(c.key));
              }

              // 3. Capture new candidates from a genuine user edit only (never
              //    programmatic setContent or our own replacement).
              const isUserEdit =
                tr.docChanged && !tr.getMeta("preventUpdate") && !remove;
              if (isUserEdit) {
                const fresh = candidatesFromUserTransaction(tr, newState);
                if (fresh.length > 0) {
                  pending = pending.concat(
                    fresh.map((c) => ({ key: seq++, ...c })),
                  );
                }
              }

              if (pending === value.pending && seq === value.seq) return value;
              return { pending, seq };
            },
          },
          view(view) {
            // identifier → resolved ref (null = miss); resolved at most once.
            const resultCache = new Map<string, ResolvedIssueRef | null>();
            const inFlight = new Set<string>();
            let destroyed = false;
            let scheduled = false;

            // Apply all ready (resolved) pending candidates on a microtask, so
            // we never dispatch synchronously from inside `update`.
            function scheduleApply(): void {
              if (scheduled || destroyed) return;
              scheduled = true;
              void Promise.resolve().then(() => {
                scheduled = false;
                if (!destroyed) applyReady(view);
              });
            }

            function applyReady(v: EditorView): void {
              const st = pluginKey.getState(v.state);
              if (!st || st.pending.length === 0) return;

              const removeKeys: number[] = [];
              const replacements: {
                from: number;
                to: number;
                ref: ResolvedIssueRef;
              }[] = [];

              for (const c of st.pending) {
                if (!resultCache.has(c.identifier)) continue;
                const ref = resultCache.get(c.identifier);
                removeKeys.push(c.key); // processed either way
                if (
                  ref &&
                  rangeStillPlainIdentifier(v.state, c.from, c.to, c.identifier)
                ) {
                  replacements.push({ from: c.from, to: c.to, ref });
                }
              }
              if (removeKeys.length === 0) return;

              const { tr } = v.state;
              // Right-to-left so earlier ranges stay valid as later ones change.
              replacements.sort((a, b) => b.from - a.from);
              for (const r of replacements) {
                tr.replaceWith(
                  r.from,
                  r.to,
                  mentionType.create({
                    id: r.ref.id,
                    label: r.ref.identifier,
                    type: "issue",
                  }),
                );
              }
              tr.setMeta(META_REMOVE, removeKeys);
              v.dispatch(tr);
            }

            return {
              update() {
                if (destroyed) return;
                const resolve = resolveRef.current;
                if (!resolve) return;
                const st = pluginKey.getState(view.state);
                if (!st || st.pending.length === 0) return;

                let anyReady = false;
                for (const c of st.pending) {
                  if (resultCache.has(c.identifier)) {
                    anyReady = true;
                    continue;
                  }
                  if (inFlight.has(c.identifier)) continue;
                  inFlight.add(c.identifier);
                  Promise.resolve(resolve(c.identifier))
                    .then((ref) => {
                      resultCache.set(c.identifier, ref ?? null);
                      inFlight.delete(c.identifier);
                      scheduleApply();
                    })
                    .catch(() => {
                      // Treat resolve failures as a miss so we don't spin.
                      resultCache.set(c.identifier, null);
                      inFlight.delete(c.identifier);
                      scheduleApply();
                    });
                }
                if (anyReady) scheduleApply();
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
