"use client";

import { BookOpenText } from "lucide-react";
import { PageHeader } from "../layout/page-header";

export function WikiPage() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="px-5">
        <div className="flex items-center gap-2">
          <BookOpenText className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Wiki</h1>
        </div>
      </PageHeader>
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
          <BookOpenText className="h-6 w-6 text-muted-foreground" />
        </div>
        <h2 className="text-base font-semibold">Wiki coming soon</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          A shared knowledge base for your workspace. Stay tuned.
        </p>
      </div>
    </div>
  );
}
