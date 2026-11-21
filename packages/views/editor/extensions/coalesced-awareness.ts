import type { Awareness } from "y-protocols/awareness";

type Listener = (...args: unknown[]) => void;

/**
 * A view of `awareness` whose "change" listeners fire at most once per
 * `intervalMs`. The caret plugin redraws every collaborator's caret on each
 * change, and every caret move of every collaborator is a change, so with
 * dozens of people typing it would redraw hundreds of times a second.
 * Everything else, including "update" and all writes, passes straight through.
 */
export function coalesceAwarenessChanges(awareness: Awareness, intervalMs = 50): Awareness {
  const wrapped = new Map<Listener, { listener: Listener; cancel: () => void }>();

  const coalesce = (listener: Listener) => {
    let timer: ReturnType<typeof setTimeout> | null = null;
    let lastArgs: unknown[] = [];
    return {
      listener: (...args: unknown[]) => {
        lastArgs = args;
        if (timer) return;
        timer = setTimeout(() => {
          timer = null;
          listener(...lastArgs);
        }, intervalMs);
      },
      cancel: () => {
        if (timer) clearTimeout(timer);
        timer = null;
      },
    };
  };

  return new Proxy(awareness, {
    get(target, prop) {
      if (prop === "on") {
        return (event: string, listener: Listener) => {
          if (event !== "change") return target.on(event as "change", listener);
          const coalesced = coalesce(listener);
          wrapped.set(listener, coalesced);
          target.on("change", coalesced.listener);
        };
      }
      if (prop === "off") {
        return (event: string, listener: Listener) => {
          const coalesced = event === "change" ? wrapped.get(listener) : undefined;
          if (!coalesced) return target.off(event as "change", listener);
          coalesced.cancel();
          wrapped.delete(listener);
          target.off("change", coalesced.listener);
        };
      }
      const value = Reflect.get(target, prop, target);
      return typeof value === "function" ? value.bind(target) : value;
    },
  });
}
