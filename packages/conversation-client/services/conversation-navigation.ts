import { makeAutoObservable, observable, runInAction } from "mobx";
import { parseWithFallback } from "@multica/core/api/schema";
import { localV2NavigationPath, localV2NavigationSchema, type LocalV2Navigation, type LocalV2NavigationQuery } from "@multica/core/local-conversation/v2";
import type { ConversationModel } from "../models/conversation-model";

type Node = LocalV2Navigation["nodes"][number];
type Range = NonNullable<LocalV2Navigation["range"]>;
const DIRECTORY_PAGE_TURNS = 20;

/** Directory reads never change the transcript. Only an explicit locate selects a body window. */
export class ConversationNavigation {
  count: number | null = null;
  readonly nodes = observable.map<number, Node>();
  directory: Range | null = null;
  directoryPaging = false;
  window: Range | null = null;
  locating = false;
  paging = false;
  error: string | null = null;
  private generation = "";
  private directorySeq = -1;
  private countedTurns = -1;
  private countRead?: Promise<void>;
  private readonly pages = new Map<number, LocalV2Navigation>();
  private readonly reads = new Map<string, Promise<LocalV2Navigation>>();
  private intent = 0;
  private pageRead?: Promise<void>;
  private pageDirection?: "before" | "after";
  private directoryIntent = 0;
  private directoryRead?: Promise<void>;
  private directoryDirection?: "before" | "after";

  constructor(private readonly options: {
    supported?(): Promise<boolean>;
    model(): ConversationModel | undefined;
    request(query: LocalV2NavigationQuery): Promise<unknown>;
    changed(): void;
  }) {
    makeAutoObservable<this, "options" | "pages" | "reads" | "countRead" | "pageRead" | "directoryRead">(this, {
      options: false, pages: false, reads: false, countRead: false, pageRead: false, directoryRead: false,
    }, { autoBind: true });
  }

  sync(): void {
    const model = this.options.model();
    if (!model) return;
    if (this.generation !== model.storeGeneration) {
      this.generation = model.storeGeneration;
      this.intent++;
      this.directoryIntent++;
      this.directory = null; this.directoryPaging = false; this.directoryRead = undefined;
      this.locating = false; this.paging = false; this.pageRead = undefined;
      this.count = null; this.window = null; this.nodes.clear(); this.pages.clear();
      this.reads.clear(); this.directorySeq = -1; this.countedTurns = -1; this.countRead = undefined;
    }
    if (this.count !== null && this.countedTurns !== model.summary.turn_count) void this.ensure().catch(() => {});
  }

  async ensure(): Promise<void> {
    if (this.options.supported && !await this.options.supported()) return;
    this.syncGeneration();
    const model = this.options.model();
    if (!model || (this.count !== null && this.countedTurns === model.summary.turn_count)) return;
    if (this.countRead) return this.countRead;
    const turns = model.summary.turn_count;
    runInAction(() => { this.countedTurns = turns; });
    const intent = this.directoryIntent;
    const pending = this.read(turns > 0 ? { beforeIndex: turns, limit: DIRECTORY_PAGE_TURNS, includeBody: false } : {}).then((page) => { runInAction(() => {
      this.countedTurns = turns;
      if (intent === this.directoryIntent && page.directory_range && (!this.directory || !this.directory.has_after)) {
        this.directory = this.directory ? {
          ...page.directory_range, start_index: this.directory.start_index, has_before: this.directory.has_before,
        } : page.directory_range;
      }
    }); });
    this.countRead = pending;
    try { await pending; }
    finally {
      if (this.countRead === pending) {
        this.countRead = undefined;
        if (this.count !== null && this.options.model() === model && model.summary.turn_count !== turns) void this.ensure().catch(() => {});
      }
    }
  }

  private syncGeneration(): void {
    if (this.options.model()?.storeGeneration !== this.generation) this.sync();
  }

  private read(query: LocalV2NavigationQuery): Promise<LocalV2Navigation> {
    const key = localV2NavigationPath("", query);
    const existing = this.reads.get(key);
    if (existing) return existing;
    const model = this.options.model();
    const generation = this.generation;
    const request = { ...query, generation };
    const pending = this.options.request(request).then((raw) => {
      const page = parseWithFallback<LocalV2Navigation | null>(raw, localV2NavigationSchema, null, { endpoint: "local-v2.navigation" });
      if (!page) throw new Error("Invalid conversation navigation response");
      if (this.options.model() !== model || page.store_generation !== generation || this.generation !== generation) throw new Error("Conversation changed. Try again.");
      runInAction(() => {
        if (page.snapshot_seq >= this.directorySeq) {
          this.count = page.node_count;
          this.directorySeq = page.snapshot_seq;
        }
        for (const node of page.nodes) if (page.snapshot_seq >= this.directorySeq || !this.nodes.has(node.node_index)) this.nodes.set(node.node_index, node);
        this.error = null;
      });
      return page;
    }).catch((error: unknown) => {
      runInAction(() => { if (this.generation === generation) this.error = error instanceof Error ? error.message : String(error); });
      throw error;
    }).finally(() => { if (this.reads.get(key) === pending) this.reads.delete(key); });
    this.reads.set(key, pending);
    return pending;
  }

  async previewDirectory(index: number): Promise<void> {
    this.syncGeneration();
    const node = this.nodes.get(index);
    if (node && this.directory && node.turn_index >= this.directory.start_index && node.turn_index <= this.directory.end_index) return;
    const intent = ++this.directoryIntent;
    this.directoryRead = undefined;
    this.directoryPaging = true;
    try {
      const page = await this.read({ nodeIndex: index, before: 9, after: 10, includeBody: false });
      runInAction(() => { if (intent === this.directoryIntent) this.directory = page.directory_range ?? null; });
    } finally { runInAction(() => { if (intent === this.directoryIntent) this.directoryPaging = false; }); }
  }

  async moreDirectory(direction: "before" | "after"): Promise<void> {
    if (this.directoryRead) {
      const intent = this.directoryIntent;
      const currentDirection = this.directoryDirection;
      await this.directoryRead;
      if (intent === this.directoryIntent && currentDirection !== direction) await this.moreDirectory(direction);
      return;
    }
    const range = this.directory;
    if (!range || this.directoryPaging || (direction === "before" ? !range.has_before : !range.has_after)) return;
    const intent = this.directoryIntent;
    this.directoryPaging = true;
    this.directoryDirection = direction;
    const pending = (async () => { try {
      const page = await this.read(direction === "before" ? { beforeIndex: range.start_index, limit: DIRECTORY_PAGE_TURNS, includeBody: false } : { afterIndex: range.end_index, limit: DIRECTORY_PAGE_TURNS, includeBody: false });
      runInAction(() => {
        const next = page.directory_range;
        if (intent !== this.directoryIntent || !next) return;
        this.directory = {
          start_index: Math.min(range.start_index, next.start_index), end_index: Math.max(range.end_index, next.end_index),
          has_before: direction === "before" ? next.has_before : range.has_before,
          has_after: direction === "after" ? next.has_after : range.has_after,
        };
      });
    } finally { runInAction(() => { if (intent === this.directoryIntent) { this.directoryPaging = false; this.directoryRead = undefined; } }); }
    })();
    this.directoryRead = pending;
    await pending;
  }

  async preview(index: number): Promise<LocalV2Navigation> {
    this.syncGeneration();
    const cached = this.pages.get(index);
    if (cached) return cached;
    const page = await this.read({ nodeIndex: index });
    if (page.turns.every((turn) => ["completed", "failed", "cancelled"].includes(turn.status) && turn.events.every((event) => event.final))) this.pages.set(index, page);
    if (this.pages.size > 32) this.pages.delete(this.pages.keys().next().value!);
    return page;
  }

  async locate(index: number): Promise<Node | null> {
    this.syncGeneration();
    const intent = ++this.intent;
    this.pageRead = undefined; this.paging = false;
    this.locating = true;
    try {
      const page = await this.preview(index);
      if (intent !== this.intent || !page.range) return null;
      runInAction(() => {
        this.options.model()?.mergeNavigation(page.turns);
        this.window = page.range;
        this.options.changed();
      });
      return page.nodes.find((node) => node.node_index === index) ?? null;
    } finally { runInAction(() => { if (intent === this.intent) this.locating = false; }); }
  }

  async more(direction: "before" | "after"): Promise<void> {
    if (!this.window || this.locating) return;
    if (this.pageRead) {
      const intent = this.intent;
      const currentDirection = this.pageDirection;
      await this.pageRead;
      if (direction !== currentDirection && intent === this.intent) await this.more(direction);
      return;
    }
    const window = this.window;
    if (direction === "before" ? !window.has_before : !window.has_after) return;
    const intent = this.intent;
    this.paging = true;
    this.pageDirection = direction;
    const pending = (async () => { try {
      const page = await this.read(direction === "before" ? { beforeIndex: window.start_index, limit: 10 } : { afterIndex: window.end_index, limit: 10 });
      if (intent !== this.intent || !page.range) return;
      runInAction(() => {
        this.options.model()?.mergeNavigation(page.turns);
        this.window = {
          start_index: Math.min(window.start_index, page.range!.start_index),
          end_index: Math.max(window.end_index, page.range!.end_index),
          has_before: direction === "before" ? page.range!.has_before : window.has_before,
          has_after: direction === "after" ? page.range!.has_after : window.has_after,
        };
        this.options.changed();
      });
    } finally { runInAction(() => { if (intent === this.intent) { this.paging = false; this.pageRead = undefined; } }); }
    })();
    this.pageRead = pending;
    await pending;
  }

  latest(): void {
    this.intent++; this.window = null; this.locating = false; this.paging = false; this.pageRead = undefined;
    this.options.changed();
  }

  async refreshDirectory(): Promise<void> {
    this.countedTurns = -1;
    this.pages.clear();
    await this.ensure();
  }

  visibleTurnIds(): readonly string[] {
    const model = this.options.model();
    if (!model) return [];
    const range = this.window;
    const start = range?.start_index ?? model.historyStartIndex;
    const end = range?.has_after ? range.end_index : Infinity;
    return [...model.turns.values()].filter((turn) => turn.index >= start && turn.index <= end).map((turn) => turn.id);
  }
}
