export interface ScrollMetrics {
  scrollTop: number;
  scrollHeight: number;
  clientHeight: number;
}

export const CHAT_LIVE_END_THRESHOLD = 4;
const INPUT_WINDOW_MS = 300;

export function distanceFromBottom(m: ScrollMetrics): number {
  return Math.max(0, m.scrollHeight - m.scrollTop - m.clientHeight);
}

// Chat follows reader intent, not a pixel budget: browsers can round, scale,
// and animate input across frames. Only movement of this list confirms it.
// The newest-first task transcript keeps its separate prepend-anchor policy.
export function createChatFollow(now: () => number = Date.now) {
  let following = true;
  let lastScrollTop = 0;
  let direction = 0;
  let inputAt = -INPUT_WINDOW_MS;
  let inputAllowed = () => true;
  let mouseHeld = false;
  let scrollbarHeld = false;
  let touchHeld = false;

  const freshInput = () => now() - inputAt < INPUT_WINDOW_MS && inputAllowed();
  const shouldPin = (m: ScrollMetrics) =>
    following && !mouseHeld && !touchHeld && distanceFromBottom(m) > CHAT_LIVE_END_THRESHOLD;

  return {
    isFollowing: () => following,
    syncPosition(scrollTop: number) {
      lastScrollTop = scrollTop;
    },
    input(delta: number, allowed: () => boolean = () => true) {
      direction = Math.sign(delta);
      inputAt = now();
      inputAllowed = allowed;
    },
    clearInput() {
      direction = 0;
      inputAt = -INPUT_WINDOW_MS;
      inputAllowed = () => true;
    },
    resume() {
      following = true;
      direction = 0;
    },
    pointerDown(onScrollbar: boolean) {
      mouseHeld = true;
      scrollbarHeld = onScrollbar;
    },
    pointerUp() {
      mouseHeld = false;
      scrollbarHeld = false;
    },
    touchStart() {
      touchHeld = true;
    },
    touchEnd() {
      touchHeld = false;
    },
    onScroll(m: ScrollMetrics) {
      const moved = m.scrollTop - lastScrollTop;
      lastScrollTop = m.scrollTop;
      const distance = distanceFromBottom(m);
      const readerMoved = scrollbarHeld || (freshInput() && Math.sign(moved) === direction);
      if (readerMoved) {
        // Confirmed touch/key momentum may outlast its initial input event.
        // A quiet interval still expires the candidate without a timer.
        inputAt = now();
        // A shrinking scroll extent clamps scrollTop at the bottom. It is
        // not a reader leaving, even with an unconsumed input still pending.
        if (moved < 0 && distance > 0) following = false;
        else if (moved > 0 && distance <= CHAT_LIVE_END_THRESHOLD) following = true;
      }
      return { pin: shouldPin(m), readerMoved, moved };
    },
    onResize: shouldPin,
  };
}
