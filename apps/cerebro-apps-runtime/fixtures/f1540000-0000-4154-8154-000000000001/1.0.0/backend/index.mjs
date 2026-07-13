export default async function formatAllergens(input) {
  if (!input?.ingredients || !input?.registryKey || !input?.aiBaseUrl) {
    throw new Error("ingredients, registryKey, and aiBaseUrl are required");
  }
  const response = await fetch(`${String(input.aiBaseUrl).replace(/\/$/, "")}/v1/chat/completions`, {
    method: "POST",
    headers: { authorization: `Bearer ${input.registryKey}`, "content-type": "application/json" },
    body: JSON.stringify({
      model: input.model ?? "claude-haiku-4-5",
      messages: [{ role: "system", content: "Return JSON with formatted_ingredients and allergens. Uppercase regulated allergens only." }, { role: "user", content: String(input.ingredients) }],
      response_format: { type: "json_object" }
    }),
  });
  if (!response.ok) throw new Error(`AI gateway returned HTTP ${response.status}`);
  const body = await response.json();
  const content = body.choices?.[0]?.message?.content;
  if (typeof content !== "string") throw new Error("AI gateway response contained no result");
  return JSON.parse(content);
}
