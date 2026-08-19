"use client";

import {
  Pagination as PaginationRoot,
  PaginationContent,
  PaginationEllipsis,
  PaginationItem,
  PaginationLink,
  PaginationNext,
  PaginationPrevious,
} from "@multica/ui/components/ui/pagination";

interface PaginationProps {
  page: number;
  pageSize: number;
  total: number;
  onPageChange: (page: number) => void;
}

// Thin wrapper driven by ListWorkspacesResult's total/page/pageSize. Renders
// first/last + a window of pages around the current one, collapsing the rest
// behind an ellipsis — same convention as the shared primitive's own docs.
export function WorkspacePagination({ page, pageSize, total, onPageChange }: PaginationProps) {
  const pageCount = Math.max(1, Math.ceil(total / pageSize));
  if (pageCount <= 1) return null;

  const pages = new Set<number>([1, pageCount, page, page - 1, page + 1]);
  const sorted = [...pages].filter((p) => p >= 1 && p <= pageCount).sort((a, b) => a - b);

  const linkProps = (targetPage: number) => ({
    href: "#",
    onClick: (e: React.MouseEvent) => {
      e.preventDefault();
      onPageChange(targetPage);
    },
  });

  return (
    <PaginationRoot>
      <PaginationContent>
        <PaginationItem>
          <PaginationPrevious
            {...linkProps(Math.max(1, page - 1))}
            aria-disabled={page === 1}
            className={page === 1 ? "pointer-events-none opacity-50" : undefined}
          />
        </PaginationItem>
        {sorted.map((p, idx) => {
          const prev = sorted[idx - 1];
          const showEllipsis = prev !== undefined && p - prev > 1;
          return (
            <div key={p} className="flex items-center">
              {showEllipsis && (
                <PaginationItem>
                  <PaginationEllipsis />
                </PaginationItem>
              )}
              <PaginationItem>
                <PaginationLink {...linkProps(p)} isActive={p === page}>
                  {p}
                </PaginationLink>
              </PaginationItem>
            </div>
          );
        })}
        <PaginationItem>
          <PaginationNext
            {...linkProps(Math.min(pageCount, page + 1))}
            aria-disabled={page === pageCount}
            className={page === pageCount ? "pointer-events-none opacity-50" : undefined}
          />
        </PaginationItem>
      </PaginationContent>
    </PaginationRoot>
  );
}
