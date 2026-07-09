import { beforeEach, describe, expect, it } from "vitest";
import { useWelcomeStore, type WelcomeSignal } from "./welcome-store";

const sample: WelcomeSignal = {
  workspaceId: "ws-1",
  choice: "runtime",
  runtimeId: "rt-1",
};

describe("welcome-store", () => {
  beforeEach(() => {
    useWelcomeStore.getState().reset();
  });

  it("starts empty and not dismissed", () => {
    expect(useWelcomeStore.getState().signal).toBeNull();
    expect(useWelcomeStore.getState().dismissed).toBe(false);
  });

  it("set() stores the signal and clears any prior dismissed flag", () => {
    useWelcomeStore.getState().dismiss();
    expect(useWelcomeStore.getState().dismissed).toBe(true);
    useWelcomeStore.getState().set(sample);
    expect(useWelcomeStore.getState().signal).toEqual(sample);
    // A fresh set() means a fresh Welcome session — dismissed resets so
    // the next mount renders the new signal.
    expect(useWelcomeStore.getState().dismissed).toBe(false);
  });

  it("dismiss() marks dismissed without clearing the signal", () => {
    useWelcomeStore.getState().set(sample);
    useWelcomeStore.getState().dismiss();
    expect(useWelcomeStore.getState().signal).toEqual(sample);
    expect(useWelcomeStore.getState().dismissed).toBe(true);
  });

  it("reset() clears signal AND dismissed (used on logout)", () => {
    useWelcomeStore.getState().set(sample);
    useWelcomeStore.getState().dismiss();
    useWelcomeStore.getState().reset();
    expect(useWelcomeStore.getState().signal).toBeNull();
    expect(useWelcomeStore.getState().dismissed).toBe(false);
  });

  it("skip-path signals omit runtimeId", () => {
    const skip: WelcomeSignal = { workspaceId: "ws-2", choice: "skip" };
    useWelcomeStore.getState().set(skip);
    expect(useWelcomeStore.getState().signal).toEqual(skip);
    expect(useWelcomeStore.getState().signal?.runtimeId).toBeUndefined();
  });
});
