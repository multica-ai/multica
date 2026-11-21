/**
 * Runs `fn` once now and then at most once per `intervalMs` while `target`'s
 * subtree keeps changing. A live doc changes on every keystroke and caret
 * move of every collaborator, so per-frame work would run sixty times a
 * second; the last change is always picked up within `intervalMs`.
 */
export function watchDom(target: Node, fn: () => void, intervalMs = 200): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  const schedule = () => {
    if (timer) return;
    timer = setTimeout(() => {
      timer = null;
      fn();
    }, intervalMs);
  };
  fn();
  const observer = new MutationObserver(schedule);
  observer.observe(target, { childList: true, subtree: true, characterData: true });
  return () => {
    observer.disconnect();
    if (timer) clearTimeout(timer);
  };
}
