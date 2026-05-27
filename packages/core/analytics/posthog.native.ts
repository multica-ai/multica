import type { PostHogClient } from "./posthog";

const noop = () => undefined;

const posthog: PostHogClient = {
  init: noop as unknown as PostHogClient["init"],
  register: noop as PostHogClient["register"],
  identify: noop as PostHogClient["identify"],
  reset: noop as PostHogClient["reset"],
  capture: noop as PostHogClient["capture"],
};

export default posthog;
