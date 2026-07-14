const appId = "f1540000-0000-4154-8154-000000000001";
const version = "1.0.0";
const scriptPath = document.currentScript?.src ? new URL(document.currentScript.src).pathname : "";
const runtimeBase = scriptPath.replace(new RegExp(`/apps/${appId}/${version}/app\\.js$`), "");

document.querySelector("form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const output = document.querySelector("#result");
  output.hidden = false;
  output.textContent = "Formatting…";
  try {
    const tokenResponse = await fetch(`/api/cerebro/apps/${appId}/token`, {
      method: "POST",
      credentials: "include",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version }),
    });
    const token = await tokenResponse.json();
    if (!tokenResponse.ok) throw new Error(token.error || "Could not authorize the app");

    const response = await fetch(`${runtimeBase}/workers/${appId}/${version}/invoke`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        ingredients: document.querySelector("#ingredients").value,
        registryKey: token.key,
        aiBaseUrl: token.ai_base_url,
      }),
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || "Formatting failed");
    output.textContent = `${result.formatted_ingredients}\n\nAllergens: ${result.allergens.join(", ")}`;
  } catch (error) {
    output.textContent = error instanceof Error ? error.message : "Formatting failed";
  }
});
