/**
 * Development hook into every request `ApiClient` sends through its common
 * path. Nothing registers one in production; the dev toolbar's recorder is the
 * only observer.
 */
export interface ApiRequestObserver {
  onStart(request: {
    requestId: string;
    method: string;
    path: string;
    body: string | null;
  }): void;
  /** `status` is null when the request never got a response. */
  onEnd(result: { requestId: string; status: number | null; error: string | null }): void;
}

let observer: ApiRequestObserver | null = null;

export function setApiRequestObserver(next: ApiRequestObserver | null): void {
  observer = next;
}

export function getApiRequestObserver(): ApiRequestObserver | null {
  return observer;
}
