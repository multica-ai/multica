/**
 * Tracks the tabbed main windows and the workspace each one shows. A
 * workspace belongs to at most one window (the VS Code folder model), so
 * routing by workspace — notification clicks, cross-workspace links — has a
 * single answer. Creation order is the restore order; focus order decides
 * which window receives app-level requests that name no workspace.
 */
export class MainWindowRegistry<W> {
  private readonly windows: W[] = [];
  private readonly workspaces = new Map<W, string | null>();
  private focusOrder: W[] = [];

  get size(): number {
    return this.windows.length;
  }

  add(window: W, workspaceSlug: string | null): void {
    if (this.workspaces.has(window)) return;
    this.windows.push(window);
    this.workspaces.set(window, workspaceSlug);
    this.focusOrder = [window, ...this.focusOrder];
  }

  remove(window: W): void {
    if (!this.workspaces.delete(window)) return;
    this.windows.splice(this.windows.indexOf(window), 1);
    this.focusOrder = this.focusOrder.filter((item) => item !== window);
  }

  has(window: W): boolean {
    return this.workspaces.has(window);
  }

  focus(window: W): void {
    if (!this.workspaces.has(window)) return;
    this.focusOrder = [window, ...this.focusOrder.filter((item) => item !== window)];
  }

  /** Returns whether the window's workspace actually changed. */
  setWorkspace(window: W, workspaceSlug: string | null): boolean {
    if (!this.workspaces.has(window)) return false;
    if (this.workspaces.get(window) === workspaceSlug) return false;
    this.workspaces.set(window, workspaceSlug);
    return true;
  }

  workspaceOf(window: W): string | null {
    return this.workspaces.get(window) ?? null;
  }

  /** The most recently focused window showing `workspaceSlug`. */
  findByWorkspace(workspaceSlug: string, except?: W): W | null {
    return (
      this.focusOrder.find(
        (window) =>
          window !== except && this.workspaces.get(window) === workspaceSlug,
      ) ?? null
    );
  }

  mostRecent(except?: W): W | null {
    return this.focusOrder.find((window) => window !== except) ?? null;
  }

  /** Windows in creation order. */
  all(): W[] {
    return [...this.windows];
  }

  /** Workspaces shown by every window other than `window`, deduplicated. */
  otherWorkspaces(window: W): string[] {
    const slugs = new Set<string>();
    for (const other of this.windows) {
      const slug = this.workspaces.get(other);
      if (other !== window && slug) slugs.add(slug);
    }
    return [...slugs];
  }
}
