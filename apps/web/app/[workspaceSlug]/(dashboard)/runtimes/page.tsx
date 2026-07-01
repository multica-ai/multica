import { RuntimesPage } from "@multica/views/runtimes";

const cloudRuntimeEnabled =
  process.env.NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME === "true";
const isDevMode = process.env.NODE_ENV === "development";
const WEB_RUNTIME_PROVIDERS = ["csc"] as const;

export default function RuntimesRoute() {
  return (
    <RuntimesPage
      visibleProviders={WEB_RUNTIME_PROVIDERS}
      cloudRuntimeEnabled={cloudRuntimeEnabled}
      isDevMode={isDevMode}
    />
  );
}
