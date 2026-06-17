const OPEN_DEFAULT_TAB_EVENT = "multica:inbox-open-default-tab"; // CEREBRO-PATCH(inbox-default-tab-nav): sidebar Inbox clicks request the Dynamic inbox default tab.
let pendingDefaultTabRequest = false;

export function requestInboxDefaultTab() {
  if (typeof window === "undefined") return;
  pendingDefaultTabRequest = true;
  window.dispatchEvent(new Event(OPEN_DEFAULT_TAB_EVENT));
}

export function listenForInboxDefaultTab(listener: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  const handleDefaultTabRequest = () => {
    pendingDefaultTabRequest = false;
    listener();
  };
  window.addEventListener(OPEN_DEFAULT_TAB_EVENT, handleDefaultTabRequest);
  if (pendingDefaultTabRequest) handleDefaultTabRequest();
  return () => window.removeEventListener(OPEN_DEFAULT_TAB_EVENT, handleDefaultTabRequest);
}
