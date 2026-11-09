// vitest.config.ts aliases `electron` to this file, so a test that reaches the
// real package fails here instead. The real package downloads the Electron
// binary from GitHub on first load: CI never installs it, so an unmocked test
// passes on a dev machine and fails in CI whenever the release CDN is slow.
throw new Error(
  'This test loaded the real `electron` package. Mock it in the test file with `vi.mock("electron", () => ({ ... }))`, covering the APIs the code under test uses.',
);
