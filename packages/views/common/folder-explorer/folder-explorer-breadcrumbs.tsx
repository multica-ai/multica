"use client";

import { ChevronRight } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import type { FolderExplorerFolder } from "./model";

export function FolderExplorerBreadcrumbs({
  ariaLabel,
  rootLabel,
  path,
  onNavigate,
}: {
  ariaLabel: string;
  rootLabel: string;
  path: readonly FolderExplorerFolder[];
  onNavigate: (folderId: string | null) => void;
}) {
  const items: { id: string | null; label: string }[] = [
    { id: null, label: rootLabel },
    ...path.map((folder) => ({ id: folder.id, label: folder.name })),
  ];
  return (
    <nav aria-label={ariaLabel} className="min-w-0">
      <ol className="flex min-w-0 items-center overflow-x-auto">
        {items.map((item, index) => {
          const current = index === items.length - 1;
          return (
            <li key={item.id ?? "root"} className="flex min-w-0 items-center">
              {index > 0 && (
                <ChevronRight aria-hidden="true" className="mx-0.5 size-3 shrink-0 text-faint-foreground" />
              )}
              <button
                type="button"
                aria-current={current ? "page" : undefined}
                title={item.label}
                onClick={() => onNavigate(item.id)}
                className={cn(
                  "max-w-52 truncate rounded-xs px-1.5 py-1 text-caption transition-colors hover:bg-accent hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  current ? "font-medium text-foreground" : "text-muted-foreground",
                )}
              >
                {item.label}
              </button>
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
