// Node-only Vitest replacement. Pure helper suites import the translation
// adapter, but must not load React Native native modules in the headless lane.
export function getLocales() {
  return [{ languageCode: "en", languageTag: "en-US" }];
}
