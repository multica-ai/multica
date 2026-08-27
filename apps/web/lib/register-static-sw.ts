const SW_SCRIPT_PATH = "/sw.js";

type RegistrationLike = {
  unregister: () => Promise<unknown>;
  active?: { scriptURL: string } | null;
  waiting?: { scriptURL: string } | null;
  installing?: { scriptURL: string } | null;
};

export type ServiceWorkerContainerLike = {
  register: (
    scriptURL: string,
    options?: { scope?: string; updateViaCache?: "all" | "imports" | "none" },
  ) => Promise<unknown>;
  getRegistrations: () => Promise<readonly RegistrationLike[]>;
};

function scriptURL(registration: RegistrationLike): string {
  return (
    registration.active?.scriptURL ??
    registration.waiting?.scriptURL ??
    registration.installing?.scriptURL ??
    ""
  );
}

function isOurWorker(url: string): boolean {
  try {
    return new URL(url).pathname === SW_SCRIPT_PATH;
  } catch {
    return url.endsWith(SW_SCRIPT_PATH);
  }
}

/**
 * Production: register `/sw.js` after `load` so the worker does not contend
 * with first-paint. `updateViaCache: "none"` matches the route's
 * `Cache-Control: no-cache` so a k8s SHA deploy is not pinned by HTTP cache
 * on the worker script. Dev / test: unregister that script if a previous
 * `next start` left it installed on localhost.
 */
export function syncStaticAssetServiceWorker(input: {
  production: boolean;
  serviceWorker?: ServiceWorkerContainerLike;
  documentReadyState?: DocumentReadyState;
  onLoad?: (handler: () => void) => () => void;
}): () => void {
  const container = input.serviceWorker;
  if (!container) return () => {};

  if (!input.production) {
    void container.getRegistrations().then((registrations) => {
      for (const registration of registrations) {
        if (isOurWorker(scriptURL(registration))) {
          void registration.unregister();
        }
      }
    });
    return () => {};
  }

  const register = () => {
    void container.register(SW_SCRIPT_PATH, {
      scope: "/",
      updateViaCache: "none",
    });
  };

  if (input.documentReadyState === "complete") {
    register();
    return () => {};
  }

  if (!input.onLoad) {
    register();
    return () => {};
  }

  return input.onLoad(register);
}
