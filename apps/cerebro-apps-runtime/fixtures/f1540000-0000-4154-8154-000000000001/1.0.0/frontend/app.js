import { createMulticaApp } from "/api/cerebro/apps-runtime/sdk/multica.js";

const appId = "f1540000-0000-4154-8154-000000000001";
const version = "1.0.0";
const multica = createMulticaApp({ appId, version });
let viewRequestId = "";

multica.views.onInput((input, requestId) => {
  viewRequestId = requestId;
  if (typeof input?.ingredients === "string") document.querySelector("#ingredients").value = input.ingredients;
});

document.querySelector("form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = document.querySelector("#result");
  output.hidden = false;
  output.textContent = "Formatting…";
  try {
    const token = await multica.registry.token();
    const result = await multica.workers.invoke({
      ingredients: document.querySelector("#ingredients").value,
      registryKey: token.key,
      aiBaseUrl: token.ai_base_url,
    });
    output.textContent = `${result.formatted_ingredients}\n\nAllergens: ${result.allergens.join(", ")}`;
    if (viewRequestId) window.parent.postMessage({ type: "multica.app-view.submit", value: result }, "*");
  } catch (error) {
    output.textContent = error instanceof Error ? error.message : "Formatting failed";
  }
});
