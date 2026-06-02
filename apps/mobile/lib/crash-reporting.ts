import Constants from "expo-constants";
import type * as React from "react";
import * as Sentry from "@sentry/react-native";

type CrashEnvironment = "development" | "staging" | "production";

const appEnv = (Constants.expoConfig?.extra?.APP_ENV ??
  "development") as CrashEnvironment;
const sentryDsn = process.env.EXPO_PUBLIC_SENTRY_DSN;
const enabled = process.env.EXPO_PUBLIC_SENTRY_ENABLED === "true" && !!sentryDsn;
const release =
  Constants.expoConfig?.version != null
    ? `multica-mobile@${Constants.expoConfig.version}`
    : "multica-mobile";

const workspaceSlugPattern = /^\/([^/?#]+)/;

function redactWorkspaceRoute(pathname: string): string {
  if (!pathname.startsWith("/") || pathname === "/") return pathname;
  return pathname.replace(workspaceSlugPattern, "/[workspace]");
}

function hashSlug(slug: string): string {
  let hash = 2166136261;
  for (let i = 0; i < slug.length; i += 1) {
    hash ^= slug.charCodeAt(i);
    hash = Math.imul(hash, 16777619);
  }
  return (hash >>> 0).toString(16).padStart(8, "0");
}

export function initCrashReporting() {
  if (!enabled || !sentryDsn) return;

  Sentry.init({
    dsn: sentryDsn,
    enabled,
    environment: appEnv,
    release,
    dist: Constants.expoConfig?.ios?.buildNumber ?? undefined,
    tracesSampleRate: appEnv === "production" ? 0.05 : 0.1,
    beforeSend(event) {
      if (event.request?.url) {
        event.request.url = redactWorkspaceRoute(event.request.url);
      }
      return event;
    },
  });

  Sentry.setTag("app.env", appEnv);
  Sentry.setTag("app.platform", Constants.platform?.ios ? "ios" : "unknown");
}

export function setCrashRouteContext(pathname: string) {
  if (!enabled) return;
  Sentry.setTag("route", redactWorkspaceRoute(pathname));
}

export function setCrashWorkspaceContext(slug: string | null) {
  if (!enabled) return;
  if (!slug) {
    Sentry.setTag("workspace.present", "false");
    Sentry.setContext("workspace", null);
    return;
  }
  Sentry.setTag("workspace.present", "true");
  Sentry.setContext("workspace", {
    slug_hash: hashSlug(slug),
  });
}

export function captureBoundaryError(error: Error, componentStack?: string) {
  if (!enabled) return;
  Sentry.captureException(error, {
    contexts: componentStack
      ? {
          react: {
            component_stack: componentStack,
          },
        }
      : undefined,
  });
}

export function captureNativeError(error: unknown, context?: Record<string, unknown>) {
  if (!enabled) return;
  Sentry.captureException(error, {
    contexts: context ? { native: context } : undefined,
  });
}

export function withCrashReporting<P extends object>(
  component: React.ComponentType<P>,
): React.ComponentType<P> {
  return enabled
    ? (Sentry.wrap(component as React.ComponentType<Record<string, unknown>>) as React.ComponentType<P>)
    : component;
}

export const crashReporting = {
  enabled,
  environment: appEnv,
  release,
};
