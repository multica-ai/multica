import { RuntimesPage } from "@multica/views/runtimes";

const cloudRuntimeEnabled =
  process.env.NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME === "true";
const isDevMode = process.env.NODE_ENV === "development";

export default function RuntimesRoute() {
  return (
    <RuntimesPage
      cloudRuntimeEnabled={cloudRuntimeEnabled}
      isDevMode={isDevMode}
    />
  );
}
