import { useEffect, useEffectEvent } from "react";
import Constants from "expo-constants";
import * as Linking from "expo-linking";
import { parseDeepLink, type DeepLink } from "./deep-link";
import { endpoints } from "./env";

const configuredScheme = Constants.expoConfig?.scheme;
const scheme = (Array.isArray(configuredScheme) ? configuredScheme[0] : configuredScheme) ?? "multica";

/** Delivers the link the app was launched with, then every link opened while it runs. */
export function useDeepLinks(onLink: (link: DeepLink) => void): void {
  const deliver = useEffectEvent(onLink);

  useEffect(() => {
    let active = true;
    const handle = (url: string | null) => {
      if (!active || !url) return;
      const link = parseDeepLink(url, endpoints.webUrl, scheme);
      if (link) deliver(link);
    };
    void Linking.getInitialURL().then(handle);
    const subscription = Linking.addEventListener("url", ({ url }) => handle(url));
    return () => {
      active = false;
      subscription.remove();
    };
  }, []);
}
