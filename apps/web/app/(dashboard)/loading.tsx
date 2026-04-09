import { Skeleton } from "@multica/ui/components/ui/skeleton";

export default function DashboardLoading() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {/* Header skeleton */}
      <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
        <Skeleton className="h-5 w-5 rounded" />
        <Skeleton className="h-4 w-32" />
      </div>
      {/* Toolbar skeleton */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <Skeleton className="h-5 w-24" />
        <Skeleton className="h-8 w-24" />
      </div>
      {/* Content skeleton */}
      <div className="flex-1 p-4 space-y-3">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="flex items-center gap-3">
            <Skeleton className="h-4 w-4 rounded" />
            <Skeleton className="h-4 flex-1 max-w-md" />
            <Skeleton className="h-4 w-16" />
          </div>
        ))}
      </div>
    </div>
  );
}
