import posthog from "posthog-js";

export interface PostHogClient {
  init: typeof posthog.init;
  register: typeof posthog.register;
  identify: typeof posthog.identify;
  reset: typeof posthog.reset;
  capture: typeof posthog.capture;
}

export default posthog satisfies PostHogClient;
