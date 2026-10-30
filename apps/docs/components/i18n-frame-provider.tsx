"use client";

import { FrameworkProvider } from "fumadocs-core/framework";
import NextImage from "next/image";
import NextLink from "next/link";
import { useParams, usePathname, useRouter } from "next/navigation";
import type { ComponentProps, ReactNode } from "react";
import { i18n } from "@/lib/i18n";

// Static generation sees internal /en/... routes; the page tree and browser
// use locale-less URLs for English. Normalize before Fumadocs resolves the tree.
function useDocsPathname(): string {
  const pathname = usePathname() ?? "";
  const prefix = `/${i18n.defaultLanguage}`;
  if (pathname === prefix) return "/";
  if (pathname.startsWith(`${prefix}/`)) return pathname.slice(prefix.length);
  return pathname;
}

// Fumadocs' framework contract wants plain function components; adapt the
// next.js ones to it.
function DocsLink({
  href,
  prefetch,
  ...rest
}: ComponentProps<"a"> & { prefetch?: boolean }) {
  return <NextLink href={href ?? ""} prefetch={prefetch} {...rest} />;
}

type DocsImageProps = Omit<ComponentProps<"img">, "src"> & {
  src?: ComponentProps<typeof NextImage>["src"];
  priority?: boolean;
};

function DocsImage({ src, alt = "", priority, ...rest }: DocsImageProps) {
  if (!src) {
    // eslint-disable-next-line @next/next/no-img-element
    return <img alt={alt} {...rest} />;
  }
  // Preserve MDX static imports so Next.js can read their URL and dimensions.
  return (
    <NextImage
      {...(rest as Omit<
        ComponentProps<typeof NextImage>,
        "src" | "alt" | "priority"
      >)}
      src={src}
      alt={alt}
      priority={priority}
    />
  );
}

export function DocsFrameworkProvider({ children }: { children: ReactNode }) {
  return (
    <FrameworkProvider
      Link={DocsLink}
      Image={DocsImage}
      useRouter={useRouter}
      usePathname={useDocsPathname}
      useParams={useParams}
    >
      {children}
    </FrameworkProvider>
  );
}
