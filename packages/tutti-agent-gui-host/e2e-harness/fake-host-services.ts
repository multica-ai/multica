import type { AgentGuiHostServices } from "../src/protocol";
export function fakeHostServices(
  request: (
    method: any,
    params: any,
    options?: { signal?: AbortSignal }
  ) => Promise<any>,
  subscribe: AgentGuiHostServices["subscribe"] = () => () => {}
): AgentGuiHostServices {
  return {
    queue: {
      enqueue: (params, options) =>
        options
          ? request("queue.enqueue", params, options)
          : request("queue.enqueue", params),
      statuses: (params, options) =>
        options
          ? request("queue.statuses", params, options)
          : request("queue.statuses", params),
      update: (params, options) =>
        options
          ? request("queue.update", params, options)
          : request("queue.update", params)
    },
    interaction: {
      respond: (params, options) =>
        options
          ? request("interaction.respond", params, options)
          : request("interaction.respond", params)
    },
    workspace: {
      load: (params, options) =>
        options
          ? request("workspace.load", params, options)
          : request("workspace.load", params)
    },
    session: {
      load: (params, options) =>
        options
          ? request("session.load", params, options)
          : request("session.load", params),
      create: (params, options) =>
        options
          ? request("session.create", params, options)
          : request("session.create", params),
      send: (params, options) =>
        options
          ? request("session.send", params, options)
          : request("session.send", params),
      rename: (params, options) =>
        options
          ? request("session.rename", params, options)
          : request("session.rename", params),
      fork: (params, options) =>
        options
          ? request("session.fork", params, options)
          : request("session.fork", params),
      pin: (params, options) =>
        options
          ? request("session.pin", params, options)
          : request("session.pin", params),
      delete: (params, options) =>
        options
          ? request("session.delete", params, options)
          : request("session.delete", params),
      cancel: (params, options) =>
        options
          ? request("session.cancel", params, options)
          : request("session.cancel", params),
      compact: (params, options) =>
        options
          ? request("session.compact", params, options)
          : request("session.compact", params)
    },
    models: {
      list: (params, options) =>
        options
          ? request("models.list", params, options)
          : request("models.list", params)
    },
    usage: {
      read: (params, options) =>
        options
          ? request("usage.read", params, options)
          : request("usage.read", params)
    },
    context: {
      tasks: (params, options) =>
        options
          ? request("context.tasks", params, options)
          : request("context.tasks", params)
    },
    attachment: {
      metadata: (params, options) => options ? request("attachment.metadata", params, options) : request("attachment.metadata", params),
      upload: (params, options) =>
        options
          ? request("attachment.upload", params, options)
          : request("attachment.upload", params),
      cancel: (params, options) =>
        options
          ? request("attachment.cancel", params, options)
          : request("attachment.cancel", params),
      retry: (params, options) =>
        options
          ? request("attachment.retry", params, options)
          : request("attachment.retry", params),
      forget: (params, options) =>
        options
          ? request("attachment.forget", params, options)
          : request("attachment.forget", params),
      url: (params, options) =>
        options
          ? request("attachment.url", params, options)
          : request("attachment.url", params),
      read: (params, options) =>
        options
          ? request("attachment.read", params, options)
          : request("attachment.read", params)
    },
    image: {
      url: (params, options) =>
        options
          ? request("image.url", params, options)
          : request("image.url", params),
      read: (params, options) =>
        options
          ? request("image.read", params, options)
          : request("image.read", params)
    },
    storage: {
      read: (params, options) =>
        options
          ? request("storage.read", params, options)
          : request("storage.read", params),
      write: (params, options) =>
        options
          ? request("storage.write", params, options)
          : request("storage.write", params)
    },
    hostApi: {
      openTask: (params, options) =>
        options
          ? request("host.openTask", params, options)
          : request("host.openTask", params),
      openInternalResource: (params, options) =>
        options
          ? request("host.openInternalResource", params, options)
          : request("host.openInternalResource", params),
      openSide: (params, options) =>
        options
          ? request("host.openSide", params, options)
          : request("host.openSide", params),
      openResource: (params, options) =>
        options
          ? request("host.openResource", params, options)
          : request("host.openResource", params),
      showFileContextMenu: (params, options) =>
        options
          ? request("host.showFileContextMenu", params, options)
          : request("host.showFileContextMenu", params),
      locateSource: (params, options) =>
        options
          ? request("host.locateSource", params, options)
          : request("host.locateSource", params),
      selectSession: (params, options) =>
        options
          ? request("host.selectSession", params, options)
          : request("host.selectSession", params),
      showMessage: (params, options) =>
        options
          ? request("host.showMessage", params, options)
          : request("host.showMessage", params),
      promptApplied: (params, options) =>
        options
          ? request("host.promptApplied", params, options)
          : request("host.promptApplied", params)
    },
    subscribe
  };
}
