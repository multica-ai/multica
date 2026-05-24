import { describe, it, expect, beforeEach } from "vitest";
import { configStore } from "./index";

describe("configStore – apiBaseUrl", () => {
  beforeEach(() => {
    configStore.setState({ apiBaseUrl: "", cdnDomain: "" });
  });

  it("defaults to empty string", () => {
    expect(configStore.getState().apiBaseUrl).toBe("");
  });

  it("setApiBaseUrl stores the URL", () => {
    configStore.getState().setApiBaseUrl("https://api.example.com");
    expect(configStore.getState().apiBaseUrl).toBe("https://api.example.com");
  });

  it("setApiBaseUrl overwrites the previous value", () => {
    configStore.getState().setApiBaseUrl("https://old.example.com");
    configStore.getState().setApiBaseUrl("https://new.example.com");
    expect(configStore.getState().apiBaseUrl).toBe("https://new.example.com");
  });

  it("setApiBaseUrl does not affect other fields", () => {
    configStore.setState({ cdnDomain: "cdn.example.com" });
    configStore.getState().setApiBaseUrl("https://api.example.com");
    expect(configStore.getState().cdnDomain).toBe("cdn.example.com");
  });
});
