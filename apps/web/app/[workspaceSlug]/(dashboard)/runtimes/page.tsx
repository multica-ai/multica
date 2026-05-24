import { RuntimesPage } from "@multica/views/runtimes";

const cloudRuntimeEnabled =
  process.env.NEXT_PUBLIC_ENABLE_CLOUD_RUNTIME === "true";

export default function Page() {
  return <RuntimesPage cloudRuntimeEnabled={cloudRuntimeEnabled} />;
}
