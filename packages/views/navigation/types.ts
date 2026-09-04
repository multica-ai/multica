export interface NavigationAdapter {
  push(path: string): void;
  replace(path: string): void;
  back(): void;
  pathname: string;
  searchParams: URLSearchParams;
  /**
   * Current fragment, including its leading `#`, or `""` when the location has
   * none. Part of "where am I" alongside `pathname` / `searchParams`: shared
   * views that rebuild the current URL (copy link, feedback) must keep a deep
   * link such as `#comment-…` intact, and only the platform can report it —
   * the desktop renderer runs a MemoryRouter over a `file://` page, so its
   * `window.location.hash` is always empty. Compose the three with
   * `currentPath()` rather than concatenating them at each call site.
   */
  hash: string;
  /**
   * Desktop only: open a path in a new tab. Optional `title` overrides the
   * default tab label. `opts.activate` controls focus:
   *   - `false` / omitted → background tab (browser cmd+click semantics; what
   *     modifier-click on links and mentions should use).
   *   - `true` → foreground tab (explicit "Open in new tab" toolbar buttons,
   *     where the user is asking to move into the new context).
   * Cross-workspace paths always switch workspace, regardless of `activate`.
   */
  openInNewTab?: (
    path: string,
    title?: string,
    opts?: { activate?: boolean },
  ) => void;
  /** Return a shareable URL for a path. Web: origin + path. Desktop: public web URL of the connected environment. */
  getShareableUrl: (path: string) => string;
  /**
   * Optional: warm up route assets / RSC payload for a path. Web wires this
   * to `router.prefetch`; desktop leaves it undefined because react-router
   * already loads the whole SPA. Callers must invoke via `prefetch?.(href)`.
   */
  prefetch?: (path: string) => void;
  /**
   * Optional: is there an in-app page behind the current one, so that `back()`
   * lands somewhere inside Multica rather than stepping off the app? Only the
   * platform can answer — web reads the browser's session history, desktop the
   * active tab's virtual history. Adapters that cannot answer leave this
   * undefined and callers must treat that as `false`.
   *
   * Read it through `useBackOrReplace()` rather than calling it directly.
   */
  canGoBack?: () => boolean;
  /**
   * Optional: draw attention to the tab the current content is already showing
   * in, WITHOUT navigating. Used when a link resolves to the surface the user
   * is already on — e.g. a mention of the current issue from inside that
   * issue's own detail page: re-navigating there is a no-op that still tears
   * the page down and back up, so the click is swallowed and this fires a brief
   * highlight on the active tab instead. Only the desktop shell owns a tab
   * strip to flash; web leaves it undefined and callers must invoke it via
   * `flashActiveTab?.()`, treating absence as "nothing to flash".
   */
  flashActiveTab?: () => void;
  /**
   * Optional: step forward through history, the inverse of `back()`. Web wires
   * this to `router.forward`; the desktop shell to the active tab's virtual
   * history. It is optional because not every adapter owns a forward stack: the
   * dedicated issue window steps back with `navigate(-1)` and has no forward
   * counterpart to offer. That window does not mount `<GlobalShortcuts>`, so
   * today the history chords never reach it — the optionality keeps the adapter
   * contract honest rather than serving a live no-op. Callers must still invoke
   * it via `forward?.()`.
   */
  forward?: () => void;
}
