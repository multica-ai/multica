// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import { localV2NavigationPath, localV2SnapshotSchema, type LocalV2Navigation } from "@multica/core/local-conversation/v2";
import { ConversationModel } from "../models/conversation-model";
import { ConversationNavigation } from "./conversation-navigation";
import { ConversationService } from "./conversation-service";
import { createFakeTransport } from "../testing/fake-transport";

const at = "2026-11-05T00:00:00Z";
const turn = (index: number) => ({ id: `t${index}`, turn_index: index, request_id: `r${index}`, status: "completed", started_at: at, messages: [{ id: `m${index}`, role: "user", content: `Prompt ${index}`, created_at: at, attachment_ids: [], steered: false, failed: false }], events: [] });
const snapshot = () => localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 100, conversation: { conversation_id: "c", summary_seq: 1, provider: "claude", title: "c", status: "idle", pinned: false, archived: false, updated_at: at, turn_count: 100 }, turns: Array.from({ length: 10 }, (_, i) => turn(i + 90)), has_older_turns: true });
const page = (start: number, end: number): LocalV2Navigation => ({ store_generation: "g", snapshot_seq: 100, node_count: 100, range: { start_index: start, end_index: end, has_before: start > 0, has_after: end < 99 }, turns: Array.from({ length: end - start + 1 }, (_, i) => turn(start + i)), nodes: Array.from({ length: end - start + 1 }, (_, i) => ({ node_index: start + i, turn_index: start + i, message_id: `m${start + i}`, turn_id: `t${start + i}`, summary: `Prompt ${start + i}`, has_agent_response: false })) });

function fixture(request: (query: string) => Promise<unknown>) {
  const model = ConversationModel.fromSnapshot(snapshot());
  const changed = vi.fn();
  const navigation = new ConversationNavigation({ model: () => model, request: (query) => {
    const params = new URLSearchParams(localV2NavigationPath("c", query).split("?")[1]);
    params.delete("store_generation");
    return request(params.toString());
  }, changed });
  return { model, changed, navigation };
}

describe("ConversationNavigation", () => {
  it("keeps existing history usable on a peer that has not advertised navigation", async () => {
    const scope = { userId: "u", workspaceId: "w" };
    const request = vi.fn(async () => snapshot());
    const transport = createFakeTransport(scope, request);
    transport.capabilities = async () => ["device-link"];
    const service = new ConversationService(transport, scope, { deviceId: "old-peer", snapshots: null, onGeneration: () => {}, onUsed: () => {} });
    await service.ensure("c");
    await service.navigation("c").ensure();
    expect(service.navigation("c").count).toBeNull();
    expect(service.navigation("c").visibleTurnIds()).toHaveLength(10);
    expect(request).toHaveBeenCalledTimes(1);
    service.dispose();
  });
  it("renders a directory count without reading bodies and deduplicates hover prefetch", async () => {
    const request = vi.fn(async (query: string) => query.includes("include_body=false") ? { ...page(80, 99), directory_range: page(80, 99).range, turns: [], range: null } : page(6, 15));
    const { model, navigation, changed } = fixture(request);
    await navigation.ensure();
    expect(navigation.count).toBe(100);
    await Promise.all([navigation.preview(10), navigation.preview(10)]);
    expect(request).toHaveBeenCalledTimes(2);
    expect(navigation.window).toBeNull();
    expect(model.turns.has("t10")).toBe(false);
    expect(changed).not.toHaveBeenCalled();
    await navigation.locate(10);
    expect(request).toHaveBeenCalledTimes(2);
    expect(navigation.visibleTurnIds()).toEqual(Array.from({ length: 10 }, (_, i) => `t${i + 6}`));
    expect(model.historyStartIndex).toBe(90);
    expect(model.seq).toBe(100);
  });

  it("expands both boundaries without filling or hiding the gaps to the live tail", async () => {
    const { model, navigation } = fixture(async (query) => query.startsWith("before") ? page(0, 5) : query.startsWith("after") ? page(16, 25) : page(6, 15));
    await navigation.preview(10);
    await navigation.locate(10);
    await navigation.more("before");
    await navigation.more("after");
    expect(navigation.window).toEqual({ start_index: 0, end_index: 25, has_before: false, has_after: true });
    expect(navigation.visibleTurnIds()).toHaveLength(26);
    expect(model.historyStartIndex).toBe(90);
    expect(model.hasOlderTurns).toBe(true);
    navigation.latest();
    expect(navigation.visibleTurnIds()).toEqual(Array.from({ length: 10 }, (_, i) => `t${i + 90}`));
  });

  it("pages the hover directory independently in both directions without fetching bodies", async () => {
    const request = vi.fn(async (query: string) => {
      const params = new URLSearchParams(query);
      expect(params.get("include_body")).toBe("false");
      if (params.has("node_index")) {
        expect(params.get("before")).toBe("9");
        expect(params.get("after")).toBe("10");
      } else expect(params.get("limit")).toBe("20");
      const start = params.has("node_index") ? 41 : params.has("after_index") ? 61 : params.get("before_index") === "41" ? 21 : 80;
      const result = page(start, start + 19);
      return { ...result, directory_range: result.range, range: null, turns: [] };
    });
    const { navigation, model, changed } = fixture(request);
    await navigation.ensure();
    expect(navigation.count).toBe(100);
    expect(navigation.directory).toMatchObject({ start_index: 80, end_index: 99 });
    await navigation.previewDirectory(50);
    await navigation.moreDirectory("before");
    await navigation.moreDirectory("after");
    expect(navigation.directory).toEqual({ start_index: 21, end_index: 80, has_before: true, has_after: true });
    expect(navigation.window).toBeNull();
    expect(model.turns.size).toBe(10);
    expect(model.historyStartIndex).toBe(90);
    expect(changed).not.toHaveBeenCalled();
  });

  it("lets only the latest click select a window and rejects a response from a replaced store", async () => {
    const answers = new Map<string, (value: unknown) => void>();
    const { model, navigation } = fixture((query) => new Promise((resolve) => answers.set(query, resolve)));
    navigation.sync();
    const first = navigation.locate(10);
    const second = navigation.locate(50);
    answers.get("node_index=50")!(page(46, 55));
    await second;
    answers.get("node_index=10")!(page(6, 15));
    expect(await first).toBeNull();
    expect(navigation.window?.start_index).toBe(46);
    const pending = navigation.preview(20);
    model.replaceSnapshot({ ...snapshot(), store_generation: "new" });
    navigation.sync();
    answers.get("node_index=20")!(page(16, 25));
    await expect(pending).rejects.toThrow("Conversation changed");
    expect(navigation.count).toBeNull();
  });

  it("rejects malformed and disconnected body windows", async () => {
    const { navigation } = fixture(async () => ({ ...page(6, 15), turns: [turn(6), turn(15)] }));
    await expect(navigation.preview(10)).rejects.toThrow("Invalid conversation navigation");
    expect(navigation.window).toBeNull();
  });

  it("preserves a completion received over P2P before an older body response arrives", async () => {
    const response = page(6, 15);
    response.turns[4]!.status = "running";
    const { model, navigation } = fixture(async () => response);
    model.applyEvents([{ seq: 101, turn_id: "t10", turn_seq: 2, item_id: null, type: "turn.completed", final: true, created_at: at, payload: { status: "completed" } }]);
    await navigation.locate(10);
    expect(model.turns.get("t10")?.status).toBe("completed");
    expect(model.seq).toBe(101);
  });

  it("does not retain a hover body that contains an unfinished turn", async () => {
    const response = page(6, 15);
    response.turns[4]!.status = "running";
    const request = vi.fn(async () => response);
    const { navigation } = fixture(request);
    await navigation.preview(10);
    await navigation.preview(10);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("keeps normal history paging contiguous after a distant navigation window was cached", async () => {
    const requested: string[] = [];
    const scope = { userId: "u", workspaceId: "w" };
    const transport = createFakeTransport(scope, async (_method, path) => {
      requested.push(path);
      if (path.includes("/snapshot")) return snapshot();
      if (path.includes("/navigation")) return page(6, 15);
      if (path.includes("/history")) return { store_generation: "g", has_older_turns: true, turns: Array.from({ length: 10 }, (_, i) => turn(i + 80)) };
      throw new Error(path);
    });
    const service = new ConversationService(transport, scope, { deviceId: "peer", snapshots: null, onGeneration: () => {}, onUsed: () => {} });
    await service.ensure("c");
    const navigation = service.navigation("c");
    await navigation.locate(10);
    await service.loadOlder("c");
    expect(requested.at(-1)).toBe("/sessions/c/history?before_index=90&turn_limit=10");
    expect(service.conversation("c")?.historyStartIndex).toBe(80);
    navigation.latest();
    expect(navigation.visibleTurnIds()).toHaveLength(20);
    service.dispose();
  });

  it("preserves settled selected history across a same-store refresh", async () => {
    const { model, navigation } = fixture(async () => page(6, 15));
    await navigation.locate(10);
    model.replaceSnapshot({ ...snapshot(), snapshot_seq: 101 });
    navigation.sync();
    expect(navigation.visibleTurnIds()).toEqual(Array.from({ length: 10 }, (_, i) => `t${i + 6}`));
    expect(model.historyStartIndex).toBe(90);
  });

  it("deduplicates an in-flight page and queues a reverse page without losing the user's request", async () => {
    let finish!: (value: unknown) => void;
    const request = vi.fn(async (query: string) => query.startsWith("before") ? new Promise((resolve) => { finish = resolve; }) : query.startsWith("after") ? page(16, 25) : page(6, 15));
    const { navigation } = fixture(request);
    await navigation.locate(10);
    const older = navigation.more("before");
    const same = navigation.more("before");
    const newer = navigation.more("after");
    const duplicate = navigation.more("after");
    finish(page(0, 5));
    await Promise.all([older, same, newer, duplicate]);
    expect(request).toHaveBeenCalledTimes(3);
    expect(navigation.window).toEqual({ start_index: 0, end_index: 25, has_before: false, has_after: true });
  });

  it("isolates a new locate from an older page still travelling over P2P", async () => {
    let finish!: (value: unknown) => void;
    const { navigation } = fixture(async (query) => query.startsWith("before") ? new Promise((resolve) => { finish = resolve; }) : query.startsWith("after") ? page(56, 65) : query.includes("50") ? page(46, 55) : page(6, 15));
    await navigation.locate(10);
    const old = navigation.more("before");
    await navigation.locate(50);
    await navigation.more("after");
    finish(page(0, 5));
    await old;
    expect(navigation.window).toEqual({ start_index: 46, end_index: 65, has_before: true, has_after: true });
    expect(navigation.paging).toBe(false);
  });
});
