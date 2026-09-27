"use client";

/**
 * HtmlPreviewBody — an HTML document in the sandboxed preview iframe, for the
 * attachment viewer's html kind (the caller has already fetched the text).
 *
 * Owns the two things every HTML preview needs:
 *
 *   - srcDoc + sandbox="allow-scripts" via CodeBlockIframe
 *   - withFragmentNavShim() so anchor links inside the iframe scroll instead
 *     of silently failing in the sandbox's opaque origin
 *
 * A ```html block in a message body is a dynamic block instead
 * (html-block-preview.tsx): it adds theme tokens and a size/error bridge.
 */

import { CodeBlockIframe } from "./code-block-iframe";
import { withFragmentNavShim } from "./utils/iframe-fragment-nav";

interface HtmlPreviewBodyProps {
  html: string;
  /** iframe title attribute (a11y). */
  title: string;
  /** Tailwind height/sizing classes for the iframe (e.g. "h-full"). */
  className?: string;
  /** Override iframe styling (border / radius). Tailwind-merge resolves
   *  conflicts so callers can pass "rounded-none border-0" for full-screen. */
  iframeClassName?: string;
}

export function HtmlPreviewBody({
  html,
  title,
  className,
  iframeClassName,
}: HtmlPreviewBodyProps) {
  return (
    <CodeBlockIframe
      html={withFragmentNavShim(html)}
      title={title}
      heightClassName={className}
      className={iframeClassName}
    />
  );
}
