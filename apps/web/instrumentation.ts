const BACKEND_URL = process.env.REMOTE_API_URL || "http://localhost:8090";
const FRONTEND_PORT = process.env.FRONTEND_PORT || "3000";
const SELF_URL = `http://localhost:${FRONTEND_PORT}`;
const BASE_PATH = process.env.BASE_PATH || process.env.NEXT_PUBLIC_BASE_PATH || "";

async function sendLog(
  level: string,
  message: string,
  stack?: string,
  url?: string
): Promise<void> {
  try {
    await fetch(`${BACKEND_URL}${BASE_PATH}/api/client-log`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        level,
        message,
        stack,
        url,
        runtime: process.env.NEXT_RUNTIME ?? "nodejs",
      }),
      signal: AbortSignal.timeout(3000),
    });
  } catch (_e) {
  }
}

function formatError(err: unknown): { message: string; stack?: string } {
  if (err instanceof Error) {
    return { message: err.message, stack: err.stack };
  }
  return { message: String(err) };
}

async function warmup(): Promise<void> {
  const warmupBasePath = process.env.NEXT_PUBLIC_BASE_PATH || "";
  await new Promise((resolve) => setTimeout(resolve, 500));
  for (const path of [`${warmupBasePath}/`, `${warmupBasePath}/login`]) {
    try {
      await fetch(`${SELF_URL}${path}`, {
        signal: AbortSignal.timeout(10000),
      });
    } catch (_e) {
    }
  }
}

export async function register() {
  if (process.env.NEXT_RUNTIME === "nodejs") {
    process.on("unhandledRejection", (reason: unknown, promise: Promise<unknown>) => {
      const { message, stack } = formatError(reason);
      const url = String(promise);
      console.error("[unhandledRejection]", message, stack ?? "");
      void sendLog("unhandledRejection", message, stack, url);
    });

    process.on("uncaughtException", (err: Error) => {
      const { message, stack } = formatError(err);
      console.error("[uncaughtException]", message, stack ?? "");
      void sendLog("uncaughtException", message, stack);
    });

    process.on("exit", (code: number) => {
      if (code !== 0) {
        console.error(`[process:exit] code=${code}`);
      }
    });

    process.on("SIGTERM", () => {
      console.log("[process:SIGTERM] received");
    });

    process.on("SIGINT", () => {
      console.log("[process:SIGINT] received");
    });

    void warmup();
  }
}
