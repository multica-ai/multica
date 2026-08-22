"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { pluginSurfaceScriptOptions } from "@multica/core/plugins";
import type { PluginInstallation, PluginSurface } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { buildSurfaceDocument, readThemeTokens } from "./surface-document";
import { createSurfaceBridge } from "./surface-bridge";

const DEFAULT_HEIGHT = 220;

interface PluginSurfaceFrameProps {
  wsId: string;
  installation: PluginInstallation;
  surface: PluginSurface;
  issueId?: string;
  className?: string;
}

/**
 * Mounts one plugin surface.
 *
 * `sandbox="allow-scripts"` WITHOUT `allow-same-origin` is the whole isolation
 * boundary and must never be loosened: the pairing is called out in the HTML
 * spec as defeating the sandbox, and it is what would hand a third-party script
 * our cookies and storage. Same rule, same reason as
 * `packages/views/editor/code-block-iframe.tsx`.
 */
export function PluginSurfaceFrame({ wsId, installation, surface, issueId, className }: PluginSurfaceFrameProps) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const anchorRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState(DEFAULT_HEIGHT);

  // The code comes from us, not from the plugin author's server. It is keyed by
  // the installed version, which is immutable — so this is fetched once and an
  // upgrade is what changes the key.
  const { data: script, isPending, isError } = useQuery(
    pluginSurfaceScriptOptions(wsId, installation.id, surface.key, installation.package_version_id),
  );

  // Tokens are read off a mounted element, so the frame inherits whatever theme
  // the app is actually in rather than a hardcoded copy that drifts. It has to
  // happen after mount: on first render the ref is still null and the surface
  // would be built with no theme at all.
  const [theme, setTheme] = useState<Record<string, string>>({});
  useEffect(() => setTheme(readThemeTokens(anchorRef.current)), []);

  const surfaceDocument = useMemo(() => {
    // An empty body is what a malformed response parses to. Rendering it would
    // mount a frame that runs nothing and looks like a working, silent panel.
    if (!script?.code) return null;
    return buildSurfaceDocument({ code: script.code, grantedScopes: installation.granted_scopes, theme });
  }, [script?.code, installation.granted_scopes, theme]);

  // A failure belongs to one rendered document on one issue. Keeping a plain
  // boolean makes the banner survive an issue change or a version/code reload
  // even though the replacement iframe is running normally.
  const surfaceInstance = useMemo(() => ({ issueId, surfaceDocument }), [issueId, surfaceDocument]);
  const [failedSurfaceInstance, setFailedSurfaceInstance] = useState<typeof surfaceInstance | null>(null);
  const failed = failedSurfaceInstance === surfaceInstance;

  // One bridge per rendered document. srcDoc changing reloads the frame — and
  // therefore restarts the guest's handshake — so the old bridge is finished:
  // close() is terminal, and reusing a closed one across a document change is
  // exactly how the panel ends up permanently blank.
  const bridge = useMemo(
    () => createSurfaceBridge({ installationId: installation.id, issueId, onResize: setHeight }),
    [installation.id, issueId, surfaceDocument],
  );

  // Arm as soon as the frame element exists. The surface announces itself once
  // its listener is attached, so there is no load event to race.
  useEffect(() => {
    if (frameRef.current) bridge.connect(frameRef.current, readThemeTokens(anchorRef.current));
    return () => bridge.close();
  }, [bridge]);

  useEffect(() => {
    const onMessage = (event: MessageEvent) => {
      const type = (event.data as { type?: string } | null)?.type;
      if (type !== "multica:plugin-surface-error") return;
      // Same window-identity rule as the bridge: without it any frame on the
      // page could light up the failure banner on every other panel.
      if (!frameRef.current?.contentWindow || event.source !== frameRef.current.contentWindow) return;
      // A surface whose script throws on its first line posts the error rather
      // than rendering blank. Acknowledge it so the guest can stop repeating
      // the signal it started before this effect was guaranteed to be mounted.
      frameRef.current.contentWindow.postMessage({ type: "multica:plugin-surface-error-ack" }, "*");
      setFailedSurfaceInstance(surfaceInstance);
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [surfaceInstance]);

  if (!surfaceDocument) {
    return (
      <div ref={anchorRef} className={cn("rounded-lg border border-surface-border px-4 py-3 text-caption text-muted-foreground", className)}>
        {/* Three states share this box on purpose: still loading, the request
            failed, and the installed version carries no code for this surface.
            All three mean "nothing to render yet"; only the last is permanent,
            and none of them should show an empty frame the reader cannot act
            on. */}
        <PluginSurfaceNotice installation={installation} kind={isPending && !isError ? "loading" : "unavailable"} />
      </div>
    );
  }

  return (
    <div ref={anchorRef} className={cn("overflow-hidden rounded-lg border border-surface-border", className)}>
      {failed ? (
        <div className="px-4 py-3 text-caption text-muted-foreground">
          <PluginSurfaceNotice installation={installation} kind="failed" />
        </div>
      ) : null}
      <iframe
        // Keyed on the issue as well: a new bridge is created when issueId
        // changes, but an unchanged document would not reload, and the guest
        // stops announcing once answered — the fresh bridge would wait forever.
        key={`${installation.id}:${surface.key}:${issueId ?? ""}`}
        ref={frameRef}
        title={`${installation.name} — ${surface.name}`}
        srcDoc={surfaceDocument}
        sandbox="allow-scripts"
        className="w-full border-0 bg-transparent"
        style={{ height }}
      />
    </div>
  );
}

function PluginSurfaceNotice({
  installation,
  kind,
}: {
  installation: PluginInstallation;
  kind: "unavailable" | "failed" | "loading";
}) {
  const { t } = useT("issues");
  if (kind === "failed") return <>{t(($) => $.plugins.surface_failed, { name: installation.name })}</>;
  if (kind === "loading") return <>{t(($) => $.plugins.surface_loading, { name: installation.name })}</>;
  return <>{t(($) => $.plugins.surface_unavailable, { name: installation.name })}</>;
}
