export interface LocalFileReadRequest {
  workspaceId: string;
  projectId: string;
  href: string;
}

export type LocalFileReadResult =
  | {
      ok: true;
      filename: string;
      text: string;
      contentType: "text/markdown" | "text/plain";
      sizeBytes: number;
    }
  | {
      ok: false;
      reason:
        | "unsupported"
        | "unavailable"
        | "not_found"
        | "outside_project"
        | "unsupported_type"
        | "too_large"
        | "failed";
    };

/** Classifies a document reference; native code must authorize and validate it. */
export function isLocalFileHref(href: string): boolean {
  const rawPath = href.split(/[?#]/, 1)[0] ?? "";
  let path = rawPath;
  try {
    path = decodeURIComponent(rawPath);
  } catch {
    // Still intercept malformed file references so they cannot become routes.
  }
  const windowsDrive = /^[a-z]:[/\\]/i.test(path);
  if (!windowsDrive && /^(?:[a-z][a-z\d+.-]*:|[/\\]{2})/i.test(path)) {
    return false;
  }
  return /(?:^|[/\\])[^/\\]+\.[a-z\d][a-z\d_-]*(?::\d+(?::\d+)?)?$/i.test(path);
}
