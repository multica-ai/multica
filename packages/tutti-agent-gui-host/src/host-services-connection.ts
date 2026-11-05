import type {
AgentGuiHostErrorDetails,
AgentGuiHostEvent,
AgentGuiHostServices,
AgentGuiOperation,
AgentGuiOperations
} from "./protocol";

export class AgentGuiHostError extends Error {
  code?: string;
  status?: number;

  constructor(error: AgentGuiHostErrorDetails) {
    super(error.message);
    this.name = "AgentGuiHostError";
    this.code = error.code;
    this.status = error.status;
  }
}

export type AgentGuiHostService = AgentGuiHostServices;

/** Methods a hidden or unfocused GUI may still call; every other one is a user action. */
function allowedInBackground(method: string, params: unknown): boolean {
  if (method === "workspace.load" || method === "session.load") return true;
  // A transfer outlives the focus that started it.
  if (
    method === "attachment.cancel" ||
    method === "attachment.retry" ||
    method === "attachment.forget" ||
    method === "attachment.url" ||
    method === "image.url"
  )
    return true;
  if (
    method === "attachment.read" ||
    method === "image.read" ||
    method === "context.tasks" ||
    method === "host.promptApplied" ||
    method === "host.showMessage" ||
    method.startsWith("storage.")
  )
    return true;
  // A prompt admitted to the queue must survive focus and tab changes.
  return (
    ["session.send", "queue.enqueue", "host.openSide"].includes(method) &&
    (params as { queued?: unknown } | null)?.queued === true
  );
}

/**
 * The GUI's client of its host in the same document. Calls remain asynchronous so host events cannot re-enter an operation.
 * Data stays in this document; no serialization or RPC dispatcher is involved.
 */
export function createHostServicesConnection(
  service: AgentGuiHostService,
  options: { interactive(): boolean; current(): boolean }
): {
  client: AgentGuiHostServices;
  dispose(): void;
} {
  let disposed = false;
  const pending = new Map<(reason: unknown) => void, () => void>();
  const listeners = new Set<(event: AgentGuiHostEvent) => void>();
  let latestInit: AgentGuiHostEvent | null = null;
  const unappliedPrompts = new Map<number, AgentGuiHostEvent>();
  const release = service.subscribe((event) => {
    if (disposed || !options.current()) return;
    const copy = event;
    if (copy.type === "init") latestInit = copy;
    if (copy.type === "appendPrompt") unappliedPrompts.set(copy.sequence, copy);
    queueMicrotask(() => {
      if (!disposed && options.current())
        for (const listener of [...listeners]) listener(copy);
    });
  });

  const invoke = <TMethod extends AgentGuiOperation>(
    method: TMethod,
    params: AgentGuiOperations[TMethod]["params"],
    run: () => Promise<AgentGuiOperations[TMethod]["result"]>,
    requestOptions?: { signal?: AbortSignal }
  ): Promise<AgentGuiOperations[TMethod]["result"]> => {
    if (disposed || !options.current())
      return Promise.reject(
        new DOMException("Host disconnected", "AbortError")
      );
    const signal = requestOptions?.signal;
    if (signal?.aborted)
      return Promise.reject(
        signal.reason ?? new DOMException("Aborted", "AbortError")
      );
    if (!options.interactive() && !allowedInBackground(method, params)) {
      return Promise.reject(
        new AgentGuiHostError({
          message: "This Agent GUI tab is inactive",
          code: "inactive_host",
          status: 409
        })
      );
    }
    return new Promise((resolve, reject) => {
      const settle = () => {
        clearTimeout(timer);
        pending.delete(reject);
        signal?.removeEventListener("abort", abort);
      };
      const abort = () => {
        settle();
        reject(signal?.reason ?? new DOMException("Aborted", "AbortError"));
      };
      const timer = setTimeout(
        () => {
          settle();
          reject(
            new AgentGuiHostError({
              code: "timeout",
              message: "The host request timed out"
            })
          );
          // Existing runtime discovery can spend 30s pending + 60s running;
          // its canonical poller allows 100s including network slack.
        },
        method === "models.list" ? 110_000 : 60_000
      );
      pending.set(reject, () => {
        settle();
        reject(new DOMException("Host disconnected", "AbortError"));
      });
      signal?.addEventListener("abort", abort, { once: true });
      void Promise.resolve()
        .then(() => {
          if (disposed || !options.current() || !pending.has(reject))
            throw new DOMException("Host disconnected", "AbortError");
          return run();
        })
        .then(
          (result) => {
            if (!pending.has(reject)) return;
            settle();
            if (disposed || !options.current()) {
              reject(new DOMException("Host disconnected", "AbortError"));
              return;
            }
            if (method === "host.promptApplied")
              unappliedPrompts.delete(
                (params as AgentGuiOperations["host.promptApplied"]["params"])
                  .sequence
              );
            resolve(result);
          },
          (error: unknown) => {
            if (!pending.has(reject)) return;
            settle();
            if (error instanceof DOMException && error.name === "AbortError") {
              reject(error);
              return;
            }
            const detail = error as {
              message?: string;
              code?: string;
              status?: number;
            };
            reject(
              new AgentGuiHostError({
                message: detail?.message || "GUI request failed",
                code: detail?.code,
                status: detail?.status
              })
            );
          }
        );
    });
  };
  const client: AgentGuiHostServices = {
    queue: {
      enqueue: (params, options) =>
        invoke(
          "queue.enqueue",
          params,
          () => service.queue.enqueue(params, options),
          options
        ),
      statuses: (params, options) =>
        invoke(
          "queue.statuses",
          params,
          () => service.queue.statuses(params, options),
          options
        ),
      update: (params, options) =>
        invoke(
          "queue.update",
          params,
          () => service.queue.update(params, options),
          options
        )
    },
    interaction: {
      respond: (params, options) =>
        invoke(
          "interaction.respond",
          params,
          () => service.interaction.respond(params, options),
          options
        )
    },
    workspace: {
      load: (params, options) =>
        invoke(
          "workspace.load",
          params,
          () => service.workspace.load(params, options),
          options
        )
    },
    session: {
      load: (params, options) =>
        invoke(
          "session.load",
          params,
          () => service.session.load(params, options),
          options
        ),
      create: (params, options) =>
        invoke(
          "session.create",
          params,
          () => service.session.create(params, options),
          options
        ),
      send: (params, options) =>
        invoke(
          "session.send",
          params,
          () => service.session.send(params, options),
          options
        ),
      rename: (params, options) =>
        invoke(
          "session.rename",
          params,
          () => service.session.rename(params, options),
          options
        ),
      fork: (params, options) =>
        invoke(
          "session.fork",
          params,
          () => service.session.fork(params, options),
          options
        ),
      pin: (params, options) =>
        invoke(
          "session.pin",
          params,
          () => service.session.pin(params, options),
          options
        ),
      delete: (params, options) =>
        invoke(
          "session.delete",
          params,
          () => service.session.delete(params, options),
          options
        ),
      cancel: (params, options) =>
        invoke(
          "session.cancel",
          params,
          () => service.session.cancel(params, options),
          options
        ),
      compact: (params, options) =>
        invoke(
          "session.compact",
          params,
          () => service.session.compact(params, options),
          options
        )
    },
    models: {
      list: (params, options) =>
        invoke(
          "models.list",
          params,
          () => service.models.list(params, options),
          options
        )
    },
    usage: {
      read: (params, options) =>
        invoke(
          "usage.read",
          params,
          () => service.usage.read(params, options),
          options
        )
    },
    context: {
      tasks: (params, options) =>
        invoke(
          "context.tasks",
          params,
          () => service.context.tasks(params, options),
          options
        )
    },
    attachment: {
      upload: (params, options) =>
        invoke(
          "attachment.upload",
          params,
          () => service.attachment.upload(params, options),
          options
        ),
      cancel: (params, options) =>
        invoke(
          "attachment.cancel",
          params,
          () => service.attachment.cancel(params, options),
          options
        ),
      retry: (params, options) =>
        invoke(
          "attachment.retry",
          params,
          () => service.attachment.retry(params, options),
          options
        ),
      forget: (params, options) =>
        invoke(
          "attachment.forget",
          params,
          () => service.attachment.forget(params, options),
          options
        ),
      url: (params, options) =>
        invoke(
          "attachment.url",
          params,
          () => service.attachment.url(params, options),
          options
        ),
      read: (params, options) =>
        invoke(
          "attachment.read",
          params,
          () => service.attachment.read(params, options),
          options
        )
    },
    image: {
      url: (params, options) =>
        invoke(
          "image.url",
          params,
          () => service.image.url(params, options),
          options
        ),
      read: (params, options) =>
        invoke(
          "image.read",
          params,
          () => service.image.read(params, options),
          options
        )
    },
    storage: {
      subscribe: service.storage.subscribe?.bind(service.storage),
      read: (params, options) =>
        invoke(
          "storage.read",
          params,
          () => service.storage.read(params, options),
          options
        ),
      write: (params, options) =>
        invoke(
          "storage.write",
          params,
          () => service.storage.write(params, options),
          options
        )
    },
    hostApi: {
      openTask: (params, options) =>
        invoke(
          "host.openTask",
          params,
          () => service.hostApi.openTask(params, options),
          options
        ),
      openInternalResource: (params, options) =>
        invoke(
          "host.openInternalResource",
          params,
          () => service.hostApi.openInternalResource(params, options),
          options
        ),
      openSide: (params, options) =>
        invoke(
          "host.openSide",
          params,
          () => service.hostApi.openSide(params, options),
          options
        ),
      openResource: (params, options) =>
        invoke(
          "host.openResource",
          params,
          () => service.hostApi.openResource(params, options),
          options
        ),
      showFileContextMenu: (params, options) =>
        invoke(
          "host.showFileContextMenu",
          params,
          () => service.hostApi.showFileContextMenu(params, options),
          options
        ),
      locateSource: (params, options) =>
        invoke(
          "host.locateSource",
          params,
          () => service.hostApi.locateSource(params, options),
          options
        ),
      selectSession: (params, options) =>
        invoke(
          "host.selectSession",
          params,
          () => service.hostApi.selectSession(params, options),
          options
        ),
      showMessage: (params, options) =>
        invoke(
          "host.showMessage",
          params,
          () => service.hostApi.showMessage(params, options),
          options
        ),
      promptApplied: (params, options) =>
        invoke(
          "host.promptApplied",
          params,
          () => service.hostApi.promptApplied(params, options),
          options
        )
    },

    subscribe(listener) {
      listeners.add(listener);
      queueMicrotask(() => {
        if (disposed || !options.current() || !listeners.has(listener)) return;
        if (latestInit) listener(latestInit);
        for (const prompt of unappliedPrompts.values()) listener(prompt);
      });
      return () => listeners.delete(listener);
    }
  };

  return {
    client,
    dispose() {
      if (disposed) return;
      disposed = true;
      release();
      for (const cancel of pending.values()) cancel();
      pending.clear();
      listeners.clear();
      unappliedPrompts.clear();
      latestInit = null;
    }
  };
}
