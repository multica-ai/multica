"use client";

import { createDraftStore } from "../drafts/create-draft-store";
import type { StoredAgentDraft } from "../types";
import { EMPTY_AGENT_DRAFT } from "./draft";
import { storedAgentDraftsEqual, toStoredAgentDraft } from "./stored-draft";

/**
 * The manual creation form, kept across navigation.
 *
 * The AI route stores its configuration server-side, keyed by the conversation
 * it belongs to. The manual route has no conversation and nothing on the server
 * to hang a draft off, so it persists locally — which is enough for what it has
 * to survive: a tab switch, a route change, a reload (#6287).
 *
 * Drafts are keyed by what is being created, not stored one at a time. A blank
 * agent and a copy of agent X are different pieces of work, and a copy of X is
 * not a copy of Y; a single slot destroys one the moment the user opens
 * another, before they have typed anything. Same shape the chat composer uses
 * for its per-session drafts.
 */
export interface ManualDraftEntry {
  /** Runtime included here, unlike the AI route's draft: there is no carrier
   *  agent to be authoritative about it. */
  runtimeId: string;
  draft: StoredAgentDraft;
}

export interface ManualAgentDrafts {
  /** Keyed by {@link manualDraftOwner}. */
  byOwner: Record<string, ManualDraftEntry>;
}

export const EMPTY_MANUAL_DRAFT_ENTRY: ManualDraftEntry = {
  runtimeId: "",
  draft: toStoredAgentDraft(EMPTY_AGENT_DRAFT, null),
};

export const MANUAL_DRAFT_BLANK_OWNER = "blank";

export function manualDraftOwner(duplicateId: string | null): string {
  return duplicateId ? `duplicate:${duplicateId}` : MANUAL_DRAFT_BLANK_OWNER;
}

/**
 * Whether an entry is worth keeping: anything at all differs from a fresh form.
 *
 * Compared whole rather than field by field on purpose. An enumerated predicate
 * silently stops covering each field added to the draft after it was written —
 * and the failure is invisible, because the field saves fine as long as some
 * *other* field is also set. The first version of this listed six fields and
 * dropped a model-only or access-only edit on the floor.
 *
 * The runtime is the one thing that must not count, and it is already outside
 * this comparison: the form seeds it on every visit, so counting it would store
 * a draft for a form nobody touched and grow a dead slot for every agent ever
 * opened for duplication. It lives on the entry, not in the draft, for exactly
 * that reason.
 */
export function manualDraftEntryHasContent(entry: ManualDraftEntry): boolean {
  return !storedAgentDraftsEqual(entry.draft, EMPTY_MANUAL_DRAFT_ENTRY.draft);
}

/**
 * Writes one owner's entry without touching the others, and drops it entirely
 * when it holds nothing. Emptying the form therefore removes its slot rather
 * than parking a blank one, which is also what keeps the map from growing a
 * dead key per agent ever opened for duplication.
 */
export function setManualDraftEntry(
  drafts: ManualAgentDrafts,
  owner: string,
  entry: ManualDraftEntry | null,
): ManualAgentDrafts {
  const byOwner = { ...drafts.byOwner };
  if (entry && manualDraftEntryHasContent(entry)) {
    byOwner[owner] = entry;
  } else {
    delete byOwner[owner];
  }
  return { byOwner };
}

const EMPTY_MANUAL_AGENT_DRAFTS: ManualAgentDrafts = { byOwner: {} };

export const useManualAgentDraftStore = createDraftStore<ManualAgentDrafts>({
  storageKey: "multica:agents:manual-draft",
  emptyData: EMPTY_MANUAL_AGENT_DRAFTS,
  hasMeaningful: (drafts) =>
    Object.values(drafts.byOwner).some(manualDraftEntryHasContent),
});
