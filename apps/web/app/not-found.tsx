"use client";

import Link from "next/link";
import { buttonVariants } from "@multica/ui/components/ui/button";
import { useT } from "@multica/views/i18n";

export default function NotFound() {
  const { t } = useT("common");
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-6 px-6 py-24 text-center">
      <p className="text-body font-medium text-muted-foreground">404</p>
      <h1 className="text-display-sm font-semibold tracking-tight">
        {t(($) => $.not_found.title)}
      </h1>
      <p className="max-w-md text-body text-muted-foreground">
        {t(($) => $.not_found.description)}
      </p>
      <Link href="/" className={buttonVariants({ className: "mt-2" })}>
        {t(($) => $.not_found.back_to_multica)}
      </Link>
    </main>
  );
}
