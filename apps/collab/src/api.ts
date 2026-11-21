import type { Actor } from "./ticket";

/** What a live document holds: a doc, or an issue's description. */
export type DocumentType = "doc" | "issue";

/** A stored document as the Go API holds it. Issues have no title. */
export interface StoredDocument {
  workspace_id: string;
  title: string;
  content: string;
  revision: number;
  /** Last persisted Yjs state, base64, and the revision it was saved with. */
  ystate: string | null;
  ystate_revision: number | null;
}

export interface StoreRequest {
  base_revision: number;
  content: string;
  ystate: string;
  /** Everyone whose changes this write carries; the first is the author. */
  contributors: Pick<Actor, "type" | "id">[];
  title?: string;
  message?: string;
  action?: "edit" | "restore";
  action_ref?: number;
  task_id?: string;
  /** Save only the Yjs state for the current revision; the doc is untouched. */
  state_only?: boolean;
  /** Issues: mentions ("type:id") this session has already stored, which
   *  must not notify again. */
  seen_mentions?: string[];
  /** Issues: a save of what people typed rather than one explicit write. */
  live?: boolean;
}

export type StoreResult =
  | { status: "stored"; revision: number; title: string; mentions: string[] }
  | { status: "conflict"; revision: number; content: string }
  | { status: "gone" };

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

/** Client for the Go API's internal collaboration endpoints
 *  (server/internal/handler/doc_collab.go, issue_description_collab.go). */
export class CollabApi {
  constructor(
    private readonly baseUrl: string,
    private readonly secret: string,
  ) {}

  private async request(path: string, init: RequestInit = {}): Promise<Response> {
    return fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        "X-Collab-Secret": this.secret,
        ...init.headers,
      },
      signal: AbortSignal.timeout(15_000),
    });
  }

  private path(type: DocumentType, id: string) {
    return `/internal/collab/${type === "issue" ? "issues" : "docs"}/${encodeURIComponent(id)}`;
  }

  async load(type: DocumentType, id: string): Promise<StoredDocument | null> {
    const res = await this.request(this.path(type, id));
    if (res.status === 404) return null;
    if (!res.ok) throw new ApiError(`load ${type} ${id}: ${res.status} ${await res.text()}`, res.status);
    return (await res.json()) as StoredDocument;
  }

  async store(type: DocumentType, id: string, body: StoreRequest): Promise<StoreResult> {
    const res = await this.request(`${this.path(type, id)}/store`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    if (res.status === 404) return { status: "gone" };
    if (res.status === 409) {
      const data = (await res.json()) as { revision: number; content: string };
      return { status: "conflict", revision: data.revision, content: data.content };
    }
    if (!res.ok) throw new ApiError(`store ${type} ${id}: ${res.status} ${await res.text()}`, res.status);
    const data = (await res.json()) as { revision: number; title?: string; mentions?: string[] };
    return { status: "stored", revision: data.revision, title: data.title ?? "", mentions: data.mentions ?? [] };
  }
}
