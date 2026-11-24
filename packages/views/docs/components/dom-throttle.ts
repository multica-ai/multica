/**
 * Runs `fn` once now and then at most once per `intervalMs` while `target`'s
 * subtree keeps changing. A live doc changes on every keystroke and caret
 * move of every collaborator, so per-frame work would run sixty times a
 * second; the last change is always picked up within `intervalMs`. A change
 * after a quiet spell runs `fn` at once, before the next paint, so content
 * that just appeared (the editor mounting) is never shown without it.
 */
export function watchDom(target: Node, fn: () => void, intervalMs = 200): () => void {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let lastRun = 0;
  const run = () => {
    lastRun = performance.now();
    fn();
  };
  const schedule = () => {
    if (timer) return;
    const wait = intervalMs - (performance.now() - lastRun);
    if (wait <= 0) {
      run();
      return;
    }
    timer = setTimeout(() => {
      timer = null;
      run();
    }, wait);
  };
  run();
  const observer = new MutationObserver(schedule);
  observer.observe(target, { childList: true, subtree: true, characterData: true });
  return () => {
    observer.disconnect();
    if (timer) clearTimeout(timer);
  };
}
