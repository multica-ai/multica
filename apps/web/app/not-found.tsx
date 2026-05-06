"use client";

import Link from "next/link";
import { buttonVariants } from "@multica/ui/components/ui/button";

export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 px-6 py-24 text-center">
      <p className="text-sm font-medium text-muted-foreground">404</p>
      <h1 className="text-2xl font-semibold tracking-tight">Page not found</h1>
      <p className="max-w-md text-sm text-muted-foreground">
        The page you are looking for doesn&rsquo;t exist or has been moved.
      </p>
      <Link href="/" className={buttonVariants({ className: "mt-2" })}>
        Back to Multica
      </Link>
    </main>
  );
}
