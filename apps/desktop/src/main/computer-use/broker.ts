import { z } from "zod";

const identity = z.string().min(1).max(200);
// Every request names the account it acts for, and it must be the account
// signed in to Desktop. Task runs (origin "task") are approved without a dialog.
const scopeSchema = z.object({ userId: identity, workspaceId: identity }).strict();
export const envelopeSchema = z.object({ op: z.enum(["status", "begin", "action", "release"]), conversationId: identity, turnId: identity, scope: scopeSchema, origin: z.literal("task").optional(), action: z.unknown().optional() }).strict();
const point = z.number().finite().nonnegative().max(100000);
const target = { app: identity, observationId: identity };
export const actionSchema = z.discriminatedUnion("op", [
  z.object({ op: z.literal("get_app"), app: identity }).strict(),
  z.object({ op: z.literal("observe"), app: identity }).strict(),
  z.object({ op: z.literal("windows"), app: identity }).strict(),
  z.object({ op: z.literal("select_window"), app: identity, windowId: identity }).strict(),
  z.object({ op: z.literal("click"), ...target, ref: identity.optional(), x: point.optional(), y: point.optional() }).strict(),
  z.object({ op: z.literal("set_value"), ...target, ref: identity, value: z.string().max(10000) }).strict(),
  z.object({ op: z.literal("type"), ...target, text: z.string().min(1).max(10000) }).strict(),
  z.object({ op: z.literal("press"), ...target, key: z.string().min(1).max(80) }).strict(),
  z.object({ op: z.literal("scroll"), ...target, x: point, y: point, dy: z.number().int().min(-3000).max(3000) }).strict(),
  z.object({ op: z.literal("drag"), ...target, x: point, y: point, toX: point, toY: point }).strict(),
]);
const appSchema = z.object({ id: identity, name: identity });
const statusSchema = z.object({ available: z.boolean(), accessibility: z.boolean(), screenRecording: z.boolean() });
export type ComputerEnvelope = z.infer<typeof envelopeSchema>;
export interface ComputerControl { key: string; conversationId: string; app?: string }
export interface BrokerOptions {
  currentUser(): string | null | undefined;
  generation(): number;
  native(action: Record<string, unknown>, signal: AbortSignal): Promise<unknown>;
  /** Increments whenever the native helper loses its per-app state. */
  nativeGeneration(): number;
  /** Drops the helper's selected window and observation for apps another turn may claim next. */
  forgetNative(apps: string[]): void;
  confirm(app: { id: string; name: string }, request: ComputerEnvelope, signal: AbortSignal): Promise<boolean>;
  permissions(signal: AbortSignal): Promise<boolean>;
  activity(controls: ComputerControl[]): void;
  stopNative(): void;
  stopped?(request: { conversationId: string; turnId: string }): void;
}

interface Lease {
  key: string;
  request: ComputerEnvelope;
  generation: number;
  controller: AbortController;
  /** Approved apps this turn holds, with the native generation that resolved them. */
  apps: Map<string, number>;
  appName?: string;
}

/**
 * Each conversation turn holds a lease, and each application belongs to at
 * most one lease at a time. Turns controlling different applications run
 * concurrently; native calls are still serialized by the helper.
 */
export class ComputerBroker {
  private revoked = new Set<string>();
  private leases = new Map<string, Lease>();
  constructor(private options: BrokerOptions) {}
  async handle(raw: unknown, signal: AbortSignal): Promise<unknown> {
    const request = envelopeSchema.parse(raw);
    const { op } = request;
    if (request.scope.userId !== this.options.currentUser()) throw new Error("account_changed: Computer Use requires the active account");
    signal.throwIfAborted();
    const key = leaseKey(request);
    if (op === "release") {
      this.end(this.leases.get(key));
      return { status: "released" };
    }
    if (op === "status") return this.options.native({ op: "status" }, signal);
    if (op === "begin") {
      if (this.revoked.has(key)) throw new Error("execution_ended: The user stopped Computer Use for this turn");
      let lease = this.leases.get(key);
      if (!lease) {
        lease = { key, request, generation: this.options.generation(), controller: new AbortController(), apps: new Map() };
        this.leases.set(key, lease);
        this.report();
      }
      const status = statusSchema.parse(await this.options.native({ op: "status" }, signal));
      if (!status.accessibility || !status.screenRecording) {
        if (await this.options.permissions(signal)) {
          this.check(lease, signal);
          await this.options.native({ op: "request_permissions" }, signal);
        }
        this.end(lease);
        throw new Error("permission_required: Grant Screen Recording and Accessibility to Multica in macOS Settings, then retry");
      }
      this.check(lease, signal);
      return { status: "ready", mode: "background" };
    }
    const lease = this.leases.get(key);
    if (!lease) throw new Error("execution_ended: No desktop control lease for this turn");
    const active = AbortSignal.any([signal, lease.controller.signal]);
    this.check(lease, active);
    const action = actionSchema.parse(request.action);
    if (action.op === "get_app") {
      const app = appSchema.parse(await this.options.native(action, active));
      if (!lease.apps.has(app.id)) {
        this.claimable(lease, app);
        if (request.origin !== "task") {
          if (!await this.options.confirm(app, request, active)) throw new Error("app_not_allowed: Application access was declined");
          this.check(lease, active);
          this.claimable(lease, app);
        }
      }
      lease.apps.set(app.id, this.options.nativeGeneration());
      lease.appName = app.name;
      this.report();
      await this.options.native({ op: "launch", app: app.id }, active);
      return app;
    }
    const resolved = lease.apps.get(action.app);
    if (resolved === undefined) throw new Error("app_not_allowed: Get and approve this application first");
    if (resolved !== this.options.nativeGeneration()) throw new Error("native_restarted: The Computer Use helper restarted; get the application and select its window again");
    try { return await this.options.native(action, active); }
    finally { if (active.aborted) this.end(lease); }
  }
  /** Ends the turn whose request the daemon cancelled. */
  cancel(raw: unknown) {
    const request = envelopeSchema.safeParse(raw);
    if (request.success) this.end(this.leases.get(leaseKey(request.data)));
  }
  /** User stop: ends one control, or all of them, and refuses to let those turns reacquire. */
  revoke(key?: string) {
    const leases = key === undefined ? [...this.leases.values()] : [this.leases.get(key)].filter((lease) => lease !== undefined);
    for (const lease of leases) {
      this.revoked.add(lease.key);
      this.end(lease);
      this.options.stopped?.(lease.request);
    }
    if (key === undefined) this.options.stopNative();
  }
  stop() {
    for (const lease of [...this.leases.values()]) this.end(lease);
    this.options.stopNative();
  }
  private claimable(lease: Lease, app: { id: string; name: string }) {
    for (const other of this.leases.values()) {
      if (other !== lease && other.apps.has(app.id)) throw new Error(`app_busy: Another conversation controls ${app.name}; retry after its turn ends`);
    }
  }
  private check(lease: Lease, signal: AbortSignal) {
    if (signal.aborted) throw new Error("execution_ended: Computer Use was cancelled");
    if (this.leases.get(lease.key) !== lease || lease.generation !== this.options.generation()) throw new Error("execution_ended: Computer Use authorization ended");
  }
  private end(lease: Lease | undefined) {
    if (!lease || this.leases.get(lease.key) !== lease) return;
    this.leases.delete(lease.key);
    lease.controller.abort();
    if (this.leases.size === 0) this.options.stopNative();
    else if (lease.apps.size) this.options.forgetNative([...lease.apps.keys()]);
    this.report();
  }
  private report() {
    this.options.activity([...this.leases.values()].map((lease) => ({ key: lease.key, conversationId: lease.request.conversationId, ...(lease.appName ? { app: lease.appName } : {}) })));
  }
}

const leaseKey = (request: ComputerEnvelope) => JSON.stringify([request.scope, request.conversationId, request.turnId]);
