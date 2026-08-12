import { describe, expect, it } from "vitest";
import { visibleRuntimeModels } from "./model-options";

describe("visibleRuntimeModels", () => {
  it("adds the configured Pi provider catalog to agent model choices", () => {
    const models = visibleRuntimeModels([], {
      provider: "pi",
      has_default_model_api_key: true,
      default_model_config: {
        provider: "moonshot",
        api: "openai-completions",
        base_url: "https://api.moonshot.ai/v1",
        model: "kimi-k3",
      },
    });

    expect(models.map((model) => model.id)).toEqual(
      expect.arrayContaining([
        "kimi-k3",
        "kimi-k2.7-code",
        "kimi-k2.7-code-highspeed",
      ]),
    );
    expect(models.find((model) => model.id === "kimi-k3")).toMatchObject({
      provider: "moonshot",
      default: true,
    });
  });
});
