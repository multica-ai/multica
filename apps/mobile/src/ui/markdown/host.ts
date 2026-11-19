import { createContext, useContext } from "react";
import { Linking, type ImageURISource } from "react-native";

/** What Markdown needs from the app to show images and follow links. */
export interface MarkdownHost {
  /** The source to load an image URL from, credentials included; null when it can't be shown. */
  imageSource: (url: string) => ImageURISource | null;
  openLink: (url: string) => void;
}

const browserHost: MarkdownHost = {
  imageSource: (url) => (/^https?:\/\//i.test(url) ? { uri: url } : null),
  openLink: (url) => {
    if (/^(https?|mailto):/i.test(url)) void Linking.openURL(url);
  },
};

export const MarkdownHostContext = createContext<MarkdownHost>(browserHost);

export function useMarkdownHost(): MarkdownHost {
  return useContext(MarkdownHostContext);
}
