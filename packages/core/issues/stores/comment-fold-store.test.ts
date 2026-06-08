import { beforeEach, describe, expect, it } from "vitest";
import {
  COMMENT_FOLD_DEFAULTS,
  normalizeCommentFoldSettings,
  useCommentFoldStore,
} from "./comment-fold-store";

beforeEach(() => {
  useCommentFoldStore.setState({
    settings: COMMENT_FOLD_DEFAULTS,
  });
});

describe("normalizeCommentFoldSettings", () => {
  it("returns defaults for empty input", () => {
    expect(normalizeCommentFoldSettings(undefined)).toEqual(COMMENT_FOLD_DEFAULTS);
  });

  it("bumps threshold when head + tail would leave no hidden replies", () => {
    expect(
      normalizeCommentFoldSettings({
        threshold: 4,
        headCount: 2,
        tailCount: 2,
      }),
    ).toEqual({
      enabled: true,
      threshold: 5,
      headCount: 2,
      tailCount: 2,
    });
  });

  it("respects enabled=false", () => {
    expect(normalizeCommentFoldSettings({ enabled: false }).enabled).toBe(false);
  });
});

describe("useCommentFoldStore", () => {
  it("patchSettings normalizes values", () => {
    useCommentFoldStore.getState().patchSettings({ headCount: 99, tailCount: 99 });
    const { settings } = useCommentFoldStore.getState();
    expect(settings.headCount).toBe(20);
    expect(settings.tailCount).toBe(20);
    expect(settings.threshold).toBeGreaterThan(settings.headCount + settings.tailCount);
  });

  it("resetSettings restores defaults", () => {
    useCommentFoldStore.getState().patchSettings({ enabled: false, threshold: 10 });
    useCommentFoldStore.getState().resetSettings();
    expect(useCommentFoldStore.getState().settings).toEqual(COMMENT_FOLD_DEFAULTS);
  });
});
