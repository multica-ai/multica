import { Skeleton } from "@multica/ui/components/ui/skeleton";

// Rendered by Next.js as the Suspense fallback during route transitions
// inside the (dashboard) segment. Scoped to this segment only — auth /
// landing keep their own full-screen fallbacks.
export default function DashboardLoading() {
  return (
    <div className="flex h-svh w-full flex-col">
      <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
        <Skeleton className="h-5 w-5 rounded-md" />
        <Skeleton className="h-4 w-32" />
      </div>
      <div className="flex-1 space-y-2 p-4">
        {Array.from({ length: 8 }).map((_, i) => (
          <Skeleton key={i} className="h-9 w-full" />
        ))}
      </div>
    </div>
  );
}
