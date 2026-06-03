"use client";

// FIR-2580: runtime favicon swap. The static favicon/manifest entries in
// apps/web/app/layout.tsx are build-time metadata and cannot vary per
// workspace, so this client component points the live <link rel="icon">
// tags at the active workspace's logo and restores them on workspace
// switch / unmount. Gated behind the cerebro_workspace_logo flag; when the
// flag is off or no logo is set, the default favicon is left untouched.

import { useEffect } from "react";
import { useCurrentWorkspace } from "@multica/core/paths";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

const ICON_SELECTOR =
  'link[rel~="icon"], link[rel="shortcut icon"], link[rel="apple-touch-icon"]';

export function CerebroWorkspaceFavicon() {
  const enabled = useFeatureFlag("cerebro_workspace_logo");
  const workspace = useCurrentWorkspace();
  const logoUrl = enabled ? resolvePublicFileUrl(workspace?.avatar_url) : null;

  useEffect(() => {
    if (typeof document === "undefined" || !logoUrl) return;
    const links = Array.from(
      document.querySelectorAll<HTMLLinkElement>(ICON_SELECTOR),
    );
    if (!links.length) return;
    const original = links.map((link) => ({
      link,
      href: link.getAttribute("href"),
      type: link.getAttribute("type"),
    }));
    links.forEach((link) => {
      link.setAttribute("href", logoUrl);
      // Drop the declared MIME type so the browser sniffs the logo's real
      // format instead of trusting the original svg/png declaration.
      link.removeAttribute("type");
    });
    return () => {
      original.forEach(({ link, href, type }) => {
        if (href !== null) link.setAttribute("href", href);
        if (type !== null) link.setAttribute("type", type);
      });
    };
  }, [logoUrl]);

  return null;
}
