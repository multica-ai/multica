import { useEffect } from "react";

/**
 * Wraps useEffect with an empty dependency array to make the "run once on
 * mount" intent explicit. Use only for one-time external sync:
 * DOM integration, third-party widget lifecycles, browser API subscriptions.
 *
 * @see https://react.dev/learn/you-might-not-need-an-effect
 */
export function useMountEffect(effect: () => void | (() => void)) {
  /* eslint-disable no-restricted-syntax */
  useEffect(effect, []);
}
