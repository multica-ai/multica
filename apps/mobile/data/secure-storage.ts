/**
 * Thin wrapper around expo-secure-store for the persisted auth session.
 *
 * The credential and the last-known-good user snapshot live in ONE record
 * under ONE key, written with a single setItemAsync. Two independent keys
 * (the previous "multica_token" + "multica_user" pair) could not be updated
 * atomically: a process kill between the two writes left token B next to
 * user A, and the next offline cold start would enter the app showing A's
 * identity while every request carried B's token. One key, one write, no
 * intermediate state.
 *
 * Web/desktop still key on "multica_token" (packages/core/auth/store.ts);
 * mobile deliberately diverges because only mobile persists a snapshot to
 * survive an offline cold start. Legacy "multica_token" installs migrate on
 * first read — see migrateLegacySession.
 */
import * as SecureStore from "expo-secure-store";
import { z } from "zod";
import type { User } from "@multica/core/types";
import { UserSchema } from "./schemas";

const SESSION_KEY = "multica_session";
const LEGACY_TOKEN_KEY = "multica_token";
const LEGACY_USER_KEY = "multica_user";

export interface Session {
  token: string;
  /** null = credential present, account details not available locally. */
  user: User | null;
}

/**
 * The envelope is validated separately from the snapshot on purpose. A
 * snapshot that fails validation must NOT invalidate the credential next to
 * it — dropping the token there would recreate the very bug this file exists
 * to prevent (a local problem logging the user out). Envelope damage is
 * different: if we cannot trust the record's shape we cannot trust the token
 * we would read out of it either, so that case clears everything.
 */
const SessionEnvelopeSchema = z.object({
  v: z.literal(1),
  token: z.string().min(1),
  user: z.unknown().nullable().default(null),
});

/**
 * `JSON.parse(raw) as User` only ever fails on a syntax error, so any
 * syntactically valid JSON — including a snapshot written by an older app
 * version whose User shape has since drifted — used to be accepted as a
 * verified account. This is a persistence boundary feeding an auth entry
 * point, so it parses (root CLAUDE.md, "Parse, don't cast").
 */
function parseUser(value: unknown): User | null {
  if (value == null) return null;
  const parsed = UserSchema.safeParse(value);
  // UserSchema's `id: z.string()` accepts "", which schemas.ts uses as the
  // "drifted / unauthenticated" sentinel. Never restore that as a session.
  if (!parsed.success || !parsed.data.id) return null;
  return parsed.data;
}

function serialize(session: Session): string {
  return JSON.stringify({ v: 1, token: session.token, user: session.user });
}

export async function getSession(): Promise<Session | null> {
  const raw = await SecureStore.getItemAsync(SESSION_KEY);
  if (raw === null) return migrateLegacySession();

  let decoded: unknown;
  try {
    decoded = JSON.parse(raw);
  } catch {
    await SecureStore.deleteItemAsync(SESSION_KEY);
    return null;
  }
  const envelope = SessionEnvelopeSchema.safeParse(decoded);
  if (!envelope.success) {
    await SecureStore.deleteItemAsync(SESSION_KEY);
    return null;
  }
  return { token: envelope.data.token, user: parseUser(envelope.data.user) };
}

export async function saveSession(
  token: string,
  user: User | null,
): Promise<void> {
  await SecureStore.setItemAsync(SESSION_KEY, serialize({ token, user }));
}

/**
 * Replace only the snapshot, keeping the credential it is bound to. No-ops
 * when there is no session — a snapshot without a credential is unusable and
 * must never be left on disk for a later token to adopt.
 */
export async function saveSessionUser(user: User): Promise<void> {
  const session = await getSession();
  if (!session) return;
  await saveSession(session.token, user);
}

/**
 * Legacy keys are deleted BEFORE the current one. The reverse order has a
 * resurrection window: killed after SESSION_KEY is gone but before the legacy
 * token is, the next cold start finds no session, migrates the stale legacy
 * token, and signs a logged-out user back in.
 */
export async function clearSession(): Promise<void> {
  await SecureStore.deleteItemAsync(LEGACY_USER_KEY);
  await SecureStore.deleteItemAsync(LEGACY_TOKEN_KEY);
  await SecureStore.deleteItemAsync(SESSION_KEY);
}

/** Kept for realtime-provider, which only needs the credential. */
export async function getToken(): Promise<string | null> {
  return (await getSession())?.token ?? null;
}

/**
 * Installs that predate the single-record format hold a bare
 * "multica_token". Root CLAUDE.md forbids legacy adapters for internal,
 * non-boundary code — on-device storage written by an already-distributed
 * build is the boundary case, in the same sense API response parsing is:
 * without this read every existing install would be signed out by the
 * upgrade itself, which is the exact failure this PR exists to remove.
 *
 * Idempotent — SESSION_KEY is written before the legacy keys are removed, so
 * an interrupted migration simply runs again on the next read.
 */
async function migrateLegacySession(): Promise<Session | null> {
  const token = await SecureStore.getItemAsync(LEGACY_TOKEN_KEY);
  const rawUser = await SecureStore.getItemAsync(LEGACY_USER_KEY);

  if (token === null) {
    // Orphaned snapshot: no credential can ever be proven to own it.
    if (rawUser !== null) await SecureStore.deleteItemAsync(LEGACY_USER_KEY);
    return null;
  }

  let user: User | null = null;
  if (rawUser !== null) {
    try {
      user = parseUser(JSON.parse(rawUser));
    } catch {
      user = null;
    }
  }

  const session: Session = { token, user };
  await SecureStore.setItemAsync(SESSION_KEY, serialize(session));
  await SecureStore.deleteItemAsync(LEGACY_USER_KEY);
  await SecureStore.deleteItemAsync(LEGACY_TOKEN_KEY);
  return session;
}
