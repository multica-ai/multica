import type { Actor } from "./ticket";

/** A stored document as the Go API holds it. */
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
}

export type StoreResult =
  | { status: "stored"; revision: number; title: string }
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
 *  (server/internal/handler/collab.go). */
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

  async loadDoc(id: string): Promise<StoredDocument | null> {
    const res = await this.request(`/internal/collab/docs/${encodeURIComponent(id)}`);
    if (res.status === 404) return null;
    if (!res.ok) throw new ApiError(`load doc ${id}: ${res.status} ${await res.text()}`, res.status);
    return (await res.json()) as StoredDocument;
  }

  async storeDoc(id: string, body: StoreRequest): Promise<StoreResult> {
    const res = await this.request(`/internal/collab/docs/${encodeURIComponent(id)}/store`, {
      method: "POST",
      body: JSON.stringify(body),
    });
    if (res.status === 404) return { status: "gone" };
    if (res.status === 409) {
      const data = (await res.json()) as { revision: number; content: string };
      return { status: "conflict", revision: data.revision, content: data.content };
    }
    if (!res.ok) throw new ApiError(`store doc ${id}: ${res.status} ${await res.text()}`, res.status);
    const data = (await res.json()) as { revision: number; title: string };
    return { status: "stored", revision: data.revision, title: data.title };
  }
}
