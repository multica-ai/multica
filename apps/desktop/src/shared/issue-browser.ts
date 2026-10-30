// Contract between the desktop main process, preload and renderer for the
// per-issue browser. Pure types + constants so every layer can import it.

export const ISSUE_BROWSER_STATE_CHANNEL = "issue-browser:state";

export type IssueBrowserBounds = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type IssueBrowserState = {
  issueId: string;
  url: string;
  title: string;
  loading: boolean;
  canGoBack: boolean;
  canGoForward: boolean;
};

export type IssueBrowserDrawTool = "pen" | "rect" | "arrow" | "eraser";

export type IssueBrowserDrawState = {
  tool: IssueBrowserDrawTool | null;
  color: string;
  canUndo: boolean;
};

export type IssueBrowserPickedElement = {
  selector: string;
  tag: string;
  text: string;
  html: string;
  pageUrl: string;
  pageTitle: string;
  rect: IssueBrowserBounds;
};

export type IssueBrowserCapture = {
  /** PNG, base64 without the data: prefix. */
  pngBase64: string;
  width: number;
  height: number;
  pageUrl: string;
  pageTitle: string;
};

export type IssueBrowserResult<T = undefined> =
  | { ok: true; value: T }
  | {
      ok: false;
      reason: "invalid_request" | "no_browser" | "cancelled" | "error";
      error?: string;
    };
