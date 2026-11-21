import { createHmac, timingSafeEqual } from "node:crypto";

export interface Actor {
  type: "member" | "agent";
  id: string;
  name: string;
}

/** What the Go API vouches for when it hands an editor a ticket. */
export interface Ticket {
  document: string;
  workspaceId: string;
  actor: Actor;
  readOnly: boolean;
  expiresAt: number;
}

interface TicketPayload {
  d: string;
  w: string;
  t: "member" | "agent";
  i: string;
  n: string;
  ro: boolean;
  exp: number;
}

const sign = (secret: string, payload: string) =>
  createHmac("sha256", secret).update(payload).digest("base64url");

/** Format: `<base64url JSON payload>.<base64url HMAC-SHA256>`, minted by
 *  server/internal/collab/ticket.go. */
export function verifyTicket(token: string, secret: string, now = Date.now()): Ticket {
  const [payload, signature, extra] = token.split(".");
  if (!payload || !signature || extra !== undefined) throw new Error("malformed ticket");
  const expected = Buffer.from(sign(secret, payload));
  const actual = Buffer.from(signature);
  if (expected.length !== actual.length || !timingSafeEqual(expected, actual)) {
    throw new Error("bad ticket signature");
  }
  const data = JSON.parse(Buffer.from(payload, "base64url").toString("utf8")) as TicketPayload;
  if (typeof data.exp !== "number" || data.exp * 1000 < now) throw new Error("ticket expired");
  if (data.t !== "member" && data.t !== "agent") throw new Error("bad ticket actor");
  return {
    document: data.d,
    workspaceId: data.w,
    actor: { type: data.t, id: data.i, name: data.n },
    readOnly: data.ro === true,
    expiresAt: data.exp,
  };
}

/** Test helper; production tickets come from the Go API. */
export function signTicket(ticket: Ticket, secret: string): string {
  const payload = Buffer.from(
    JSON.stringify({
      d: ticket.document,
      w: ticket.workspaceId,
      t: ticket.actor.type,
      i: ticket.actor.id,
      n: ticket.actor.name,
      ro: ticket.readOnly,
      exp: ticket.expiresAt,
    } satisfies TicketPayload),
  ).toString("base64url");
  return `${payload}.${sign(secret, payload)}`;
}
