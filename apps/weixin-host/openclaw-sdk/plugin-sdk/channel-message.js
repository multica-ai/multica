// Typing indicator with keepalive: start() sends the indicator and repeats it
// until stop(). Errors are reported through the callbacks, never thrown.
export function createTypingCallbacks({
  start,
  stop,
  onStartError,
  onStopError,
  keepaliveIntervalMs = 5000,
}) {
  let timer;
  const send = () => Promise.resolve().then(start).catch((err) => onStartError?.(err));
  return {
    async start() {
      if (timer) return;
      await send();
      timer = setInterval(send, keepaliveIntervalMs);
    },
    async stop() {
      if (!timer) return;
      clearInterval(timer);
      timer = undefined;
      await Promise.resolve().then(stop).catch((err) => onStopError?.(err));
    },
  };
}
