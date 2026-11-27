import "./dom";
import { createServer, type Server as HttpServer } from "node:http";
import type { AddressInfo } from "node:net";
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import { COLLAB_FIELD, createDocumentCodec } from "@multica/views/editor/schema/codec";
import { COMPACT_STATE_MIN_BYTES } from "./documents";
import { createCollabServer } from "./server";
import { signTicket, type Actor } from "./ticket";

const SECRET = "test-secret";
const DOC_ID = "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b";
const NAME = `doc:${DOC_ID}`;
const ada: Actor = { type: "member", id: "11111111-1111-1111-1111-111111111111", name: "Ada" };
const bob: Actor = { type: "member", id: "22222222-2222-2222-2222-222222222222", name: "Bob" };
const agent: Actor = { type: "agent", id: "33333333-3333-3333-3333-333333333333", name: "Lambda" };

/** In-memory stand-in for the Go API's internal endpoints. */
interface FakeDoc {
  content: string;
  revision: number;
  ystate: string | null;
  ystateRevision: number | null;
  stores: { contributors: string[]; content: string; message?: string; live?: boolean; seen?: string[] }[];
}
let fake: FakeDoc;
const ISSUE_ID = "0193a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5c";
const ISSUE_NAME = `issue:${ISSUE_ID}`;
let fakeIssue: FakeDoc;
const mentionsIn = (content: string) =>
  [...content.matchAll(/mention:\/\/(member|agent)\/([0-9a-f-]{36})/g)].map((m) => `${m[1]}:${m[2]}`);
/** When set, stores wait for it before answering. */
let storeGate: { requested: number; release: Promise<void> } | null = null;
let apiServer: HttpServer;
let apiUrl = "";

const codec = createDocumentCodec();
const silent = { info() {}, warn() {}, error() {} };
let collab: ReturnType<typeof createCollabServer>;
let collabUrl = "";
const providers: HocuspocusProvider[] = [];

beforeAll(async () => {
  apiServer = createServer(async (req, res) => {
    if (req.headers["x-collab-secret"] !== SECRET) {
      res.writeHead(401).end();
      return;
    }
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    const send = (status: number, body: unknown) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(JSON.stringify(body));
    };
    const route = /^\/internal\/collab\/(docs|issues)\/([^/]+)(\/store)?$/.exec(req.url ?? "");
    const issue = route?.[1] === "issues";
    const target = route && route[2] === (issue ? ISSUE_ID : DOC_ID) ? (issue ? fakeIssue : fake) : null;
    if (!route || !target) return send(404, {});
    // Issues have no title; their stores answer with the mentions stored.
    const stored = () =>
      issue ? { revision: target.revision, mentions: mentionsIn(target.content) } : { revision: target.revision, title: "T" };
    if (req.method === "GET" && !route[3]) {
      return send(200, {
        workspace_id: "ws",
        title: issue ? "" : "T",
        content: target.content,
        revision: target.revision,
        ystate: target.ystate,
        ystate_revision: target.ystateRevision,
      });
    }
    if (req.method === "POST" && route[3]) {
      const body = JSON.parse(Buffer.concat(chunks).toString());
      if (storeGate && !body.state_only) {
        storeGate.requested += 1;
        await storeGate.release;
      }
      if (body.base_revision !== target.revision) {
        return send(409, { revision: target.revision, content: target.content });
      }
      if (body.state_only) {
        target.ystate = body.ystate;
        target.ystateRevision = target.revision;
        return send(200, stored());
      }
      target.revision += 1;
      target.content = body.content;
      target.ystate = body.ystate;
      target.ystateRevision = target.revision;
      target.stores.push({
        contributors: body.contributors.map((c: Actor) => c.id),
        content: body.content,
        message: body.message,
        live: body.live,
        seen: body.seen_mentions,
      });
      return send(200, stored());
    }
    send(404, {});
  });
  await new Promise<void>((resolve) => apiServer.listen(0, resolve));
  apiUrl = `http://127.0.0.1:${(apiServer.address() as AddressInfo).port}`;

  collab = createCollabServer(
    { port: 0, secret: SECRET, apiUrl, cdnDomain: "", storeDebounceMs: 100, storeMaxDebounceMs: 400 },
    codec,
    silent,
  );
  await collab.server.listen(0);
  collabUrl = `ws://127.0.0.1:${collab.server.address.port}`;
});

afterAll(async () => {
  await collab.server.destroy();
  await new Promise((resolve) => apiServer.close(resolve));
});

afterEach(async () => {
  for (const provider of providers.splice(0)) provider.destroy();
  // Let the documents unload so every test starts from the fake API.
  await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0, 3000);
});

function resetDoc(content: string) {
  fake = { content, revision: 1, ystate: null, ystateRevision: null, stores: [] };
  storeGate = null;
}

function resetIssue(content: string) {
  fakeIssue = { content, revision: 7, ystate: null, ystateRevision: null, stores: [] };
  storeGate = null;
}

function ticket(actor: Actor, name = NAME) {
  return signTicket(
    { document: name, workspaceId: "ws", actor, readOnly: false, expiresAt: Math.floor(Date.now() / 1000) + 60 },
    SECRET,
  );
}

async function connect(actor: Actor, opts: { document?: Y.Doc; epoch?: string; name?: string } = {}) {
  const document = opts.document ?? new Y.Doc();
  const name = opts.name ?? NAME;
  const provider = new HocuspocusProvider({
    url: collabUrl,
    name,
    document,
    token: () => JSON.stringify({ ticket: ticket(actor, name), epoch: opts.epoch }),
  });
  providers.push(provider);
  await waitFor(() => provider.isSynced, 3000);
  return { provider, document };
}

async function waitFor(check: () => boolean, timeout = 3000) {
  const start = Date.now();
  while (!check()) {
    if (Date.now() - start > timeout) throw new Error("timed out");
    await new Promise((resolve) => setTimeout(resolve, 20));
  }
}

const markdown = (doc: Y.Doc) => codec.ydocToMarkdown(doc);
/** Types into the text of the n-th top-level block. */
function typeInto(doc: Y.Doc, block: number, offset: number, text: string) {
  const element = doc.getXmlFragment(COLLAB_FIELD).get(block) as Y.XmlElement;
  (element.get(0) as Y.XmlText).insert(offset, text);
}

async function internal(path: "write" | "edit" | "flush", body: unknown, name = NAME) {
  const res = await fetch(`http://127.0.0.1:${collab.server.address.port}/internal/documents/${name}/${path}`, {
    method: "POST",
    headers: { "X-Collab-Secret": SECRET, "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  return { status: res.status, body: (await res.json()) as Record<string, any> };
}

describe("collaboration server", () => {
  it("syncs two editors typing in the same paragraph and credits both", async () => {
    resetDoc("Hello world\n\nSecond");
    const a = await connect(ada);
    const b = await connect(bob);
    typeInto(a.document, 0, 5, ", Ada");
    typeInto(b.document, 0, 11, "!");
    await waitFor(() => markdown(a.document) === markdown(b.document) && markdown(a.document).includes("!"));
    expect(markdown(a.document)).toBe("Hello, Ada world!\n\nSecond");
    await waitFor(() => fake.stores.length > 0, 2000);
    expect(fake.content).toBe("Hello, Ada world!\n\nSecond");
    expect(fake.stores.at(-1)!.contributors.sort()).toEqual([ada.id, bob.id].sort());
  });

  it("applies an agent's find-and-replace edit live, as its own version", async () => {
    resetDoc("Intro\n\nStatus: draft\n\nOutro");
    const a = await connect(ada);
    typeInto(a.document, 0, 5, " (Ada)");
    const result = await internal("edit", {
      actor: agent,
      edits: [{ old_text: "Status: draft", new_text: "Status: approved" }],
      message: "approve",
    });
    expect(result.status).toBe(200);
    await waitFor(() => markdown(a.document).includes("approved"));
    expect(markdown(a.document)).toBe("Intro (Ada)\n\nStatus: approved\n\nOutro");
    // Ada's pending typing is stored first, under her name; the agent's edit
    // is a version of its own.
    const [human, agentStore] = fake.stores.slice(-2);
    expect(human!.contributors).toEqual([ada.id]);
    expect(human!.content).toBe("Intro (Ada)\n\nStatus: draft\n\nOutro");
    expect(agentStore!.contributors).toEqual([agent.id]);
    expect(agentStore!.message).toBe("approve");
    // Editors see the agent in their presence list while it writes.
    const users = () => [...a.provider.awareness!.getStates().values()].map((s) => s.user?.name);
    await waitFor(() => users().includes(agent.name));
  });

  it("credits whoever typed while a save was in flight in the next save", async () => {
    resetDoc("Alpha\n\nBeta");
    let release!: () => void;
    storeGate = { requested: 0, release: new Promise<void>((resolve) => (release = resolve)) };
    const gate = storeGate;
    const a = await connect(ada);
    const b = await connect(bob);
    typeInto(a.document, 0, 5, " by Ada");
    await waitFor(() => gate.requested === 1, 2000);
    typeInto(b.document, 1, 4, " by Bob");
    await waitFor(() => markdown(a.document).includes("by Bob"));
    release();
    await waitFor(() => fake.stores.some((store) => store.content.includes("by Bob")), 3000);
    const bobsSave = fake.stores.find((store) => store.content.includes("by Bob"))!;
    expect(fake.stores[0]!.contributors).toEqual([ada.id]);
    expect(bobsSave.contributors).toContain(bob.id);
  });

  it("refuses an edit that would make the doc too large, leaving the live copy alone", async () => {
    resetDoc("Short");
    const a = await connect(ada);
    const result = await internal("edit", {
      actor: agent,
      edits: [{ old_text: "Short", new_text: "x".repeat((2 << 20) + 1) }],
    });
    expect(result.status).toBe(413);
    expect(result.body.status).toBe("too_large");
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(markdown(a.document)).toBe("Short");
    expect(fake.content).toBe("Short");
    expect(fake.stores).toEqual([]);
  });

  it("rejects an edit whose quoted text someone else already changed", async () => {
    resetDoc("Status: draft");
    const a = await connect(ada);
    typeInto(a.document, 0, 13, " v2");
    await waitFor(() => markdown(a.document) === "Status: draft v2");
    const result = await internal("edit", {
      actor: agent,
      edits: [{ old_text: "Status: draft\n", new_text: "x" }],
    });
    expect(result.status).toBe(409);
    expect(result.body.error).toBe("edit_failed");
    expect(result.body.failure.reason).toBe("not_found");
    expect(result.body.content).toBe("Status: draft v2");
  });

  it("merges a whole-document write from a stale base around live edits", async () => {
    resetDoc("One\n\nTwo\n\nThree");
    const a = await connect(ada);
    typeInto(a.document, 2, 5, " (live)");
    await waitFor(() => markdown(a.document).includes("live"));
    const result = await internal("write", {
      actor: agent,
      base_revision: 1,
      base_content: "One\n\nTwo\n\nThree",
      content: "One (agent)\n\nTwo\n\nThree",
    });
    expect(result.status).toBe(200);
    expect(result.body.merged).toBe(true);
    await waitFor(() => markdown(a.document).includes("agent"));
    expect(markdown(a.document)).toBe("One (agent)\n\nTwo\n\nThree (live)");
    expect(fake.content).toBe("One (agent)\n\nTwo\n\nThree (live)");
  });

  it("merges a stale write to formatted text nobody else touched", async () => {
    const original = "**Name check**: see [Kaon AI](https://kaon.ai)\n\nTwo\n\nThree";
    resetDoc(original);
    const a = await connect(ada);
    typeInto(a.document, 2, 5, " (live)");
    await waitFor(() => markdown(a.document).includes("live"));
    const result = await internal("write", {
      actor: agent,
      base_revision: 1,
      base_content: original,
      content: original.replace("**Name check**", "**Name check (done)**"),
    });
    expect(result.status).toBe(200);
    expect(result.body.merged).toBe(true);
    expect(fake.content).toBe("**Name check (done)**: see [Kaon AI](https://kaon.ai)\n\nTwo\n\nThree (live)");
  });

  it("refuses a whole-document write that collides with a live edit", async () => {
    resetDoc("One\n\nTwo");
    const a = await connect(ada);
    typeInto(a.document, 1, 3, " (Ada)");
    await waitFor(() => markdown(a.document).includes("Ada"));
    const result = await internal("write", {
      actor: agent,
      base_revision: 1,
      base_content: "One\n\nTwo",
      content: "One\n\nTwo (agent)",
    });
    expect(result.status).toBe(409);
    expect(result.body.error).toBe("merge_conflict");
    expect(result.body.conflicts).toEqual([{ current: "Two (Ada)", incoming: "Two (agent)" }]);
    expect(markdown(a.document)).toBe("One\n\nTwo (Ada)");
  });

  it("refuses a restore from a revision someone has since saved over, keeping their text", async () => {
    resetDoc("Old body");
    const a = await connect(ada);
    typeInto(a.document, 0, 8, " (Ada)");
    await waitFor(() => markdown(a.document).includes("Ada"));
    // The caller read revision 1; Ada's pending edit is saved as revision 2
    // before the restore is checked.
    const stale = await internal("write", {
      actor: agent,
      base_revision: 1,
      content: "Restored body",
      action: "restore",
      action_ref: 1,
    });
    expect(stale.status).toBe(409);
    expect(stale.body.error).toBe("revision_conflict");
    expect(stale.body.revision).toBe(2);
    expect(stale.body.content).toBe("Old body (Ada)");
    expect(fake.content).toBe("Old body (Ada)");
    expect(markdown(a.document)).toBe("Old body (Ada)");

    const fresh = await internal("write", {
      actor: agent,
      base_revision: 2,
      content: "Restored body",
      action: "restore",
      action_ref: 1,
    });
    expect(fresh.status).toBe(200);
    await waitFor(() => markdown(a.document) === "Restored body");
    expect(fake.content).toBe("Restored body");
  });

  describe("keeps what Markdown cannot express", () => {
    const image = { type: "image", attrs: { src: "https://cdn.example.com/a.png", alt: "a", width: 800, height: 600 } };
    const file = { type: "fileCard", attrs: { href: "https://cdn.example.com/f.pdf", filename: "f.pdf", fileSize: 12345 } };
    const uploading = { type: "image", attrs: { src: "blob:http://local/1", uploading: true, uploadId: "u1" } };
    const paragraph = (text: string) => ({ type: "paragraph", content: [{ type: "text", text }] });

    /** Ada's editor holds media whose size and upload state live only in the
     *  Yjs document; the server's copy has caught up with it. */
    async function editorWithMedia() {
      resetDoc("First.\n\nLast.");
      const a = await connect(ada);
      codec.applyJSON(a.document, {
        type: "doc",
        content: [paragraph("First."), image, file, uploading, paragraph("Last.")],
      });
      const live = () => codec.ydocToJSON(collab.server.hocuspocus.documents.get(NAME)!);
      await waitFor(() => JSON.stringify(live()).includes('"uploadId":"u1"'));
      return a;
    }
    const blocks = (doc: Y.Doc) =>
      codec.ydocToJSON(doc).content!.filter((block) => block.type !== "paragraph").map((block) => block.attrs);

    it("in blocks an agent's edit did not touch", async () => {
      const a = await editorWithMedia();
      const before = markdown(a.document);
      const result = await internal("edit", {
        actor: agent,
        edits: [{ old_text: "First.", new_text: "First, edited." }],
      });
      expect(result.status).toBe(200);
      await waitFor(() => markdown(a.document).includes("edited"));
      expect(blocks(a.document)).toMatchObject([image.attrs, file.attrs, uploading.attrs]);
      expect(markdown(a.document)).toBe(before.replace("First.", "First, edited."));
    });

    it("in blocks a whole-document write did not touch", async () => {
      const a = await editorWithMedia();
      const result = await internal("write", {
        actor: agent,
        base_revision: 1,
        base_content: markdown(a.document),
        content: markdown(a.document).replace("Last.", "Last, rewritten."),
      });
      expect(result.status).toBe(200);
      await waitFor(() => markdown(a.document).includes("rewritten"));
      expect(blocks(a.document)).toMatchObject([image.attrs, file.attrs, uploading.attrs]);
    });
  });

  it("serializes concurrent agent edits so neither is lost", async () => {
    resetDoc("Alpha\n\nBeta\n\nGamma");
    const [first, second] = await Promise.all([
      internal("edit", { actor: agent, edits: [{ old_text: "Alpha", new_text: "Alpha 1" }] }),
      internal("edit", { actor: { ...agent, id: "44444444-4444-4444-4444-444444444444", name: "Kappa" }, edits: [{ old_text: "Gamma", new_text: "Gamma 2" }] }),
    ]);
    expect(first.status).toBe(200);
    expect(second.status).toBe(200);
    expect(fake.content).toBe("Alpha 1\n\nBeta\n\nGamma 2");
    expect(fake.stores.map((s) => s.contributors)).toEqual([
      [agent.id],
      ["44444444-4444-4444-4444-444444444444"],
    ]);
  });

  it("reuses the saved state on reload instead of rebuilding", async () => {
    resetDoc("Saved text");
    const a = await connect(ada);
    typeInto(a.document, 0, 10, " more");
    await waitFor(() => fake.content === "Saved text more", 2000);
    const epoch = a.document.getMap("meta").get("epoch");
    a.provider.destroy();
    await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0);

    // Same Y.Doc reconnects: no duplicated blocks, same epoch.
    const again = await connect(ada, { document: a.document, epoch: epoch as string });
    expect(markdown(again.document)).toBe("Saved text more");
    expect(again.document.getMap("meta").get("epoch")).toBe(epoch);
  });

  it("catches up on a write made while it was down and keeps edits made offline", async () => {
    resetDoc("First\n\nSecond");
    const a = await connect(ada);
    const epoch = a.document.getMap("meta").get("epoch") as string;
    a.provider.destroy();
    await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0);

    // While the service is unavailable an agent's write lands directly in the
    // stored doc, and Ada keeps typing in her open editor.
    fake.content = "First (agent)\n\nSecond";
    fake.revision += 1;
    typeInto(a.document, 1, 6, " (Ada offline)");

    const back = await connect(ada, { document: a.document, epoch });
    const watcher = await connect(bob);
    const expected = "First (agent)\n\nSecond (Ada offline)";
    await waitFor(() => markdown(back.document) === expected && markdown(watcher.document) === expected);
    expect(back.document.getMap("meta").get("epoch")).toBe(epoch);
    await waitFor(() => fake.content === expected, 2000);

    // The caught-up state was saved, so a later load does not replay it.
    back.provider.destroy();
    watcher.provider.destroy();
    await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0);
    const again = await connect(ada, { document: a.document, epoch });
    expect(markdown(again.document)).toBe(expected);
  });

  describe("folds in a write stored while the API could not reach it", () => {
    /** Ada has the doc open and the service holds it; an agent's write lands
     *  in the stored doc directly, before Ada's typing is saved. */
    async function directWriteDuringLiveEdit(stored: string) {
      resetDoc("One\n\nTwo");
      const a = await connect(ada);
      fake.content = stored;
      fake.revision += 1;
      typeInto(a.document, 1, 3, " (Ada)");
      await waitFor(() => fake.stores.length > 0, 2000);
      return a;
    }

    it("merging it when it changed other blocks", async () => {
      const a = await directWriteDuringLiveEdit("One (agent)\n\nTwo");
      const expected = "One (agent)\n\nTwo (Ada)";
      expect(fake.content).toBe(expected);
      expect(fake.stores.at(-1)!.message).toBeUndefined();
      await waitFor(() => markdown(a.document) === expected);
    });

    it("keeping both copies of a block both sides changed, in a labeled version", async () => {
      const a = await directWriteDuringLiveEdit("One\n\nTwo (agent)");
      const expected = "One\n\nTwo (Ada)\n\nTwo (agent)";
      expect(fake.content).toBe(expected);
      const store = fake.stores.at(-1)!;
      expect(store.contributors).toEqual([ada.id]);
      expect(store.message).toMatch(/^Kept both sides of 1 conflicting edit:/);
      await waitFor(() => markdown(a.document) === expected);
    });
  });

  it("tells a client to start over when no saved state is left to merge with", async () => {
    resetDoc("Original");
    const a = await connect(ada);
    const epoch = a.document.getMap("meta").get("epoch") as string;
    a.provider.destroy();
    await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0);

    // The saved state is gone (restored from a backup without it), so the
    // service rebuilds from Markdown under a new epoch.
    fake.content = "Rewritten elsewhere";
    fake.revision += 1;
    fake.ystate = null;
    fake.ystateRevision = null;

    const reasons: string[] = [];
    const stale = new HocuspocusProvider({
      url: collabUrl,
      name: NAME,
      document: a.document,
      token: () => JSON.stringify({ ticket: ticket(ada), epoch }),
      // A per-document close on a shared socket arrives with the server's
      // reason; the close code stays 1000.
      onClose: ({ event }) => reasons.push(event.reason),
    });
    providers.push(stale);
    await waitFor(() => reasons.includes("document-reset"));
    stale.destroy();
    expect(fake.content).toBe("Rewritten elsewhere");

    const fresh = await connect(ada);
    expect(markdown(fresh.document)).toBe("Rewritten elsewhere");
  });

  it("flushes pending edits so a reader sees the live text", async () => {
    resetDoc("Before");
    const a = await connect(ada);
    typeInto(a.document, 0, 6, " after");
    await waitFor(() => collab.store !== undefined);
    const result = await internal("flush", {});
    expect(result.body.loaded).toBe(true);
    expect(fake.content).toBe("Before after");
  });

  it("does not serialize or store a document nobody changed since its last save", async () => {
    resetDoc("Quiet");
    const a = await connect(ada);
    typeInto(a.document, 0, 5, " doc");
    await internal("flush", {});
    const stores = fake.stores.length;
    const serialize = vi.spyOn(codec, "ydocToMarkdown");
    try {
      await internal("flush", {});
      await internal("flush", {});
      expect(serialize).not.toHaveBeenCalled();
      expect(fake.stores.length).toBe(stores);
    } finally {
      serialize.mockRestore();
    }
    typeInto(a.document, 0, 9, "!");
    await internal("flush", {});
    expect(fake.content).toBe("Quiet doc!");
  });

  it("rebuilds an oversized saved state from Markdown on load", async () => {
    resetDoc("Small doc");
    const bloated = codec.createYDoc("Small doc");
    bloated.getMap("meta").set("epoch", "old-epoch");
    bloated.getMap("history").set("weight", "x".repeat(COMPACT_STATE_MIN_BYTES + 1));
    fake.ystate = Buffer.from(Y.encodeStateAsUpdate(bloated)).toString("base64");
    fake.ystateRevision = fake.revision;

    const a = await connect(ada);
    expect(markdown(a.document)).toBe("Small doc");
    expect(a.document.getMap("meta").get("epoch")).not.toBe("old-epoch");
    expect(Buffer.from(fake.ystate!, "base64").byteLength).toBeLessThan(10_000);
  });
});

describe("issue descriptions", () => {
  it("saves what editors type to the issue as live, passing on the mentions already stored", async () => {
    resetIssue(`Hi [@Bob](mention://member/${bob.id})\n\nSecond`);
    const a = await connect(ada, { name: ISSUE_NAME });
    const b = await connect(bob, { name: ISSUE_NAME });
    typeInto(a.document, 1, 6, " line");
    await waitFor(() => fakeIssue.stores.length === 1, 2000);
    expect(fakeIssue.stores[0]).toMatchObject({ contributors: [ada.id], live: true, seen: [] });
    expect(fakeIssue.content).toContain("Second line");

    typeInto(b.document, 1, 11, "!");
    await waitFor(() => fakeIssue.stores.length === 2, 2000);
    expect(fakeIssue.stores[1]).toMatchObject({ contributors: [bob.id], live: true, seen: [`member:${bob.id}`] });
  });

  it("saves an agent's edit to an issue as an explicit write of its own", async () => {
    resetIssue("Status: draft");
    const a = await connect(ada, { name: ISSUE_NAME });
    const result = await internal(
      "edit",
      { actor: agent, edits: [{ old_text: "draft", new_text: "approved" }] },
      ISSUE_NAME,
    );
    expect(result.status).toBe(200);
    expect(result.body.revision).toBe(8);
    await waitFor(() => markdown(a.document) === "Status: approved");
    expect(fakeIssue.stores).toEqual([
      { contributors: [agent.id], content: "Status: approved", message: undefined, live: false, seen: [] },
    ]);
  });

  it("does not repeat a description written directly when an editor reconnects after the sync", async () => {
    resetIssue("First\n\nSecond");
    const a = await connect(ada, { name: ISSUE_NAME });
    const epoch = a.document.getMap("meta").get("epoch") as string;

    // The API writes a description that comes with other fields itself, then
    // hands it to the open session.
    const base = fakeIssue.content;
    fakeIssue.content = "First (agent)\n\nSecond";
    fakeIssue.revision += 1;
    await internal("flush", {}, ISSUE_NAME);
    await internal("write", { actor: agent, base_content: base, content: fakeIssue.content }, ISSUE_NAME);
    await waitFor(() => markdown(a.document) === "First (agent)\n\nSecond");
    expect(fakeIssue.stores).toEqual([]);

    a.provider.destroy();
    await waitFor(() => collab.server.hocuspocus.getDocumentsCount() === 0);
    const again = await connect(ada, { document: a.document, epoch, name: ISSUE_NAME });
    expect(markdown(again.document)).toBe("First (agent)\n\nSecond");
    expect(again.document.getMap("meta").get("epoch")).toBe(epoch);
    expect(fakeIssue.content).toBe("First (agent)\n\nSecond");
  });

  it("reports an issue that no longer exists as gone", async () => {
    resetIssue("Anything");
    const result = await internal(
      "write",
      { actor: agent, content: "x" },
      "issue:0193a1b2-c3d4-7e5f-8a9b-000000000000",
    );
    expect(result.status).toBe(404);
  });
});

describe("access after joining", () => {
  let access: ReturnType<typeof createCollabServer>;
  let accessUrl = "";

  beforeAll(async () => {
    access = createCollabServer(
      { port: 0, secret: SECRET, apiUrl, cdnDomain: "", storeDebounceMs: 100, storeMaxDebounceMs: 400 },
      codec,
      silent,
      { refreshLeadMs: 1500, revalidateGraceMs: 300, sweepMs: 50 },
    );
    await access.server.listen(0);
    accessUrl = `ws://127.0.0.1:${access.server.address.port}`;
  });

  afterAll(async () => {
    await access.server.destroy();
  });

  /** Joins with a ticket expiring in `ttl` seconds; later tickets come from
   *  `next`, which may refuse by throwing. */
  async function join(actor: Actor, ttl: number, next: () => string) {
    let calls = 0;
    const reasons: string[] = [];
    const provider = new HocuspocusProvider({
      url: accessUrl,
      name: NAME,
      document: new Y.Doc(),
      token: () => {
        calls += 1;
        if (calls > 1) return next();
        return signTicket(
          { document: NAME, workspaceId: "ws", actor, readOnly: false, expiresAt: Math.floor(Date.now() / 1000) + ttl },
          SECRET,
        );
      },
      onClose: ({ event }) => reasons.push(event.reason),
    });
    providers.push(provider);
    await waitFor(() => provider.isSynced);
    return { provider, reasons, calls: () => calls };
  }

  const connections = () => access.server.hocuspocus.documents.get(NAME)?.getConnectionsCount() ?? 0;

  it("renews a connection's ticket before it expires", async () => {
    resetDoc("Hello");
    const a = await join(ada, 2, () => ticket(ada));
    await waitFor(() => a.calls() >= 2);
    // Past the first ticket's expiry, the connection is still in.
    await new Promise((resolve) => setTimeout(resolve, 2200));
    expect(a.reasons).toEqual([]);
    expect(connections()).toBe(1);
  });

  it("closes a connection that shows no fresh ticket by the time its ticket expires", async () => {
    resetDoc("Hello");
    const a = await join(ada, 2, () => {
      throw new Error("access revoked");
    });
    await waitFor(() => a.reasons.includes("reauthenticate"), 4000);
    expect(connections()).toBe(0);
  });

  it("refuses a fresh ticket issued to someone else", async () => {
    resetDoc("Hello");
    const a = await join(ada, 2, () => ticket(bob));
    await waitFor(() => a.reasons.includes("reauthenticate"));
    expect(connections()).toBe(0);
  });

  it("revalidates only the connections a revocation names", async () => {
    resetDoc("Hello");
    let allowed = true;
    const a = await join(ada, 60, () => {
      if (!allowed) throw new Error("access revoked");
      return ticket(ada);
    });
    const b = await join(bob, 60, () => ticket(bob));
    const revalidate = (body: unknown) =>
      fetch(`http://127.0.0.1:${access.server.address.port}/internal/revalidate`, {
        method: "POST",
        headers: { "X-Collab-Secret": SECRET, "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

    // Still allowed in: a fresh ticket keeps the connection.
    const kept = await revalidate({ workspace_id: "ws", actor: { type: "member", id: ada.id } });
    expect(await kept.json()).toEqual({ asked: 1 });
    await waitFor(() => a.calls() >= 2);
    await new Promise((resolve) => setTimeout(resolve, 500));
    expect(a.reasons).toEqual([]);

    // Revoked: no ticket comes, and the connection is closed after the grace
    // period. Others in the doc are untouched.
    allowed = false;
    await revalidate({ workspace_id: "ws", actor: { type: "member", id: ada.id } });
    await waitFor(() => a.reasons.includes("reauthenticate"));
    expect(b.reasons).toEqual([]);
    expect(b.calls()).toBe(1);
    expect(connections()).toBe(1);

    expect((await revalidate({ workspace_id: "other" })).status).toBe(200);
    expect((await revalidate({})).status).toBe(400);
  });
});
