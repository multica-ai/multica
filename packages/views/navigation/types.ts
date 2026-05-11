export interface NavigationAdapter {
  push(path: string): void;
  replace(path: string): void;
  back(): void;
  pathname: string;
  searchParams: URLSearchParams;
  /** Desktop only: open a path in a new background tab. Optional title overrides the default. */
  openInNewTab?: (path: string, title?: string) => void;
  /** Return a shareable URL for a path. Web: origin + path. Desktop: public web URL of the connected environment. */
  getShareableUrl: (path: string) => string;
}
