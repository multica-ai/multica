import type { LocalV2Scope, LocalV2Transport } from "@multica/core/local-conversation/v2";
import { ConversationService, type ConversationSnapshotStore } from "./conversation-service";
import type { CompletionReadStorage } from "./completion-read-service";

/** Conversations whose content is kept across all services. */
const MAX_RETAINED_CONVERSATIONS = 100;

export interface ConversationServiceManagerOptions {
  completions?(scope: LocalV2Scope, deviceId: string): CompletionReadStorage;
  /** Where another device's conversations are kept between reads. */
  snapshots(): ConversationSnapshotStore;
  limit?: number;
}

/**
 * Every ConversationService: one per workspace on each device. Created on first
 * use and independent of any view; only signing out or leaving a workspace ends
 * one. Content is retained up to one limit across all of them.
 */
export class ConversationServiceManager {
  /** Each device's services; a transport is one device. */
  private devices = new Map<LocalV2Transport, Map<string, ConversationService>>();
  private retained = new Map<string, { service: ConversationService; id: string }>();
  private generations = new WeakMap<LocalV2Transport, string>();

  constructor(private options: ConversationServiceManagerOptions) {}

  get(transport: LocalV2Transport, scope: LocalV2Scope): ConversationService {
    let services = this.devices.get(transport);
    if (!services) {
      const own = new Map<string, ConversationService>();
      services = own;
      this.devices.set(transport, own);
      const find = (value: LocalV2Scope) => own.get(JSON.stringify([value.userId, value.workspaceId]));
      transport.subscribe((value, message) => find(value)?.receive(message));
      transport.onConnection((value, connected) => find(value)?.connection(connected));
    }
    const key = JSON.stringify([scope.userId, scope.workspaceId]);
    let service = services.get(key);
    if (!service) {
      const deviceId = (transport as { deviceId?: string }).deviceId ?? "local";
      service = new ConversationService(transport, scope, {
        completionStorage: this.options.completions?.(scope, deviceId),
        deviceId,
        // Only another device's conversations cost anything to read.
        snapshots: deviceId === "local" ? null : this.options.snapshots(),
        onGeneration: (generation) => this.generationChanged(transport, generation),
        onUsed: (owner, id) => this.used(owner, id),
      });
      services.set(key, service);
    }
    return service;
  }

  /** One daemon, one database: every workspace on the device is out of date together. */
  private generationChanged(transport: LocalV2Transport, generation: string) {
    const previous = this.generations.get(transport);
    if (previous === generation) return;
    this.generations.set(transport, generation);
    if (previous === undefined) return;
    for (const service of this.devices.get(transport)?.values() ?? []) service.invalidate();
  }

  private limit() { return this.options.limit ?? MAX_RETAINED_CONVERSATIONS; }

  private retainKey(service: ConversationService, id: string) { return JSON.stringify([service.scope.userId, service.scope.workspaceId, id, this.indexOf(service)]); }
  private ids = new WeakMap<ConversationService, number>();
  private nextId = 1;
  private indexOf(service: ConversationService) {
    let value = this.ids.get(service);
    if (!value) { value = this.nextId++; this.ids.set(service, value); }
    return value;
  }

  private used(service: ConversationService, id: string) {
    const key = this.retainKey(service, id);
    this.retained.delete(key);
    this.retained.set(key, { service, id });
    if (this.retained.size <= this.limit()) return;
    for (const [candidate, { service: owner, id: conversation }] of this.retained) {
      if (this.retained.size <= this.limit()) break;
      if (owner.inUse(conversation)) continue;
      owner.evict(conversation);
      this.retained.delete(candidate);
    }
  }

  private disposeWhere(match: (scope: LocalV2Scope) => boolean) {
    for (const services of this.devices.values()) {
      for (const [key, service] of services) {
        if (!match(service.scope)) continue;
        service.dispose();
        services.delete(key);
        for (const [retainKey, value] of this.retained) if (value.service === service) this.retained.delete(retainKey);
      }
    }
  }

  disposeWorkspace(userId: string, workspaceId: string): void { this.disposeWhere((scope) => scope.userId === userId && scope.workspaceId === workspaceId); }
  /** Signing out or a session ending: the next user must not inherit anything. */
  disposeAll(): void { this.disposeWhere(() => true); }
}
