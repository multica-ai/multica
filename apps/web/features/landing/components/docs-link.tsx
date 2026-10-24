import type { ComponentProps } from "react";
import Link from "next/link";
import { isDocsHref } from "@/lib/docs-href";

// The docs site is a separate Next.js app proxied under /docs (see the
// beforeFiles rewrites in apps/web/next.config.ts). Navigating there leaves
// this app, so docs-site links render as native anchors: a next/link would
// prefetch the docs app's RSC payload into this app's router, which then
// requests the docs app's chunks from this app's /_next/static and 404s in
// the console. Same-app routes keep SPA navigation through next/link.
export function SmartLink({
  href,
  children,
  ...rest
}: Omit<ComponentProps<"a">, "href"> & { href: string }) {
  if (isDocsHref(href)) {
    return (
      <a href={href} {...rest}>
        {children}
      </a>
    );
  }
  return (
    <Link href={href} {...rest}>
      {children}
    </Link>
  );
}
