"use client";

/**
 * Placeholder for the cross-workspace Kanban view (MUL-6).
 *
 * The route exists in this PR so the rail's "All workspaces" tile resolves
 * to a real page instead of a 404. The actual board ships in MUL-6, after
 * the `GET /api/issues/cross-workspace` endpoint lands in MUL-4.
 */
export default function GlobalPage() {
  return (
    <div className="flex flex-1 items-center justify-center px-6 text-center">
      <div className="max-w-md">
        <h1 className="text-lg font-semibold">All workspaces</h1>
        <p className="mt-2 text-sm text-muted-foreground">
          The cross-workspace view is on its way. Pick a workspace from the
          left rail to keep working in the meantime.
        </p>
      </div>
    </div>
  );
}
