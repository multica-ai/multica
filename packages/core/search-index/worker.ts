// Entry point of the local search index worker (MUL-7754). Loaded as a
// SharedWorker where the platform supports one, otherwise as a dedicated
// Worker per tab; both speak the protocol in ./protocol.
import { SearchIndexHost, type PortLike } from "./host";
import type { TabMessage } from "./protocol";
import { IdbIndexStore, deleteSearchIndexDatabases, searchIndexDatabaseName } from "./store";

interface LockManagerLike {
  request<T>(name: string, callback: () => Promise<T>): Promise<T>;
}

interface MessagePortLike extends PortLike {
  onmessage: ((event: MessageEvent<TabMessage>) => void) | null;
  start?(): void;
}

const locks = (globalThis.navigator as { locks?: LockManagerLike } | undefined)?.locks;

const host = new SearchIndexHost({
  createStore: (target) => new IdbIndexStore(searchIndexDatabaseName(target.userId, target.workspaceId)),
  deleteDatabases: deleteSearchIndexDatabases,
  indexOptions: {
    withLock: locks ? (name, fn) => locks.request(name, fn) : (_name, fn) => fn(),
  },
});

function listen(port: MessagePortLike): void {
  const handle = host.connect(port);
  port.onmessage = (event) => handle(event.data);
  port.start?.();
}

const scope = globalThis as unknown as {
  onconnect?: ((event: MessageEvent) => void) | null;
};

if ("onconnect" in scope) {
  scope.onconnect = (event) => listen(event.ports[0] as unknown as MessagePortLike);
} else {
  listen(globalThis as unknown as MessagePortLike);
}
