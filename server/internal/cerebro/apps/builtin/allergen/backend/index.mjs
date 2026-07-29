export default async ({ ingredients }, multica) => {
  const inputIngredients = String(ingredients || "");
  const response = await multica.connections.call("ai_gateway", "chat.completions", {
    model: "claude-haiku-4-5",
    messages: [
      {
        role: "system",
        content: "Return only a JSON object without Markdown fences. formatted_ingredients must be one string containing the full ingredient input with regulated allergens uppercased. allergens must be an array of uppercase allergen names.",
      },
      { role: "user", content: inputIngredients },
    ],
    response_format: { type: "json_object" },
  });
  const body = response.result || response;
  const content = body.choices && body.choices[0] && body.choices[0].message && body.choices[0].message.content;
  if (typeof content !== "string") throw new Error("AI gateway response contained no result");
  const fence = String.fromCharCode(96).repeat(3);
  const cleaned = content.trim()
    .replace(new RegExp("^" + fence + "(?:json)?\\s*", "i"), "")
    .replace(new RegExp("\\s*" + fence + "$"), "");
  const result = JSON.parse(cleaned);
  const allergens = Array.isArray(result.allergens)
    ? result.allergens.map((allergen) => String(allergen).trim().toUpperCase()).filter(Boolean)
    : [];
  let formatted = Array.isArray(result.formatted_ingredients)
    ? result.formatted_ingredients.join(", ")
    : String(result.formatted_ingredients || "");
  if (!formatted.trim()) formatted = inputIngredients;
  for (const allergen of allergens) {
    const escaped = allergen.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    formatted = formatted.replace(new RegExp("\\b" + escaped + "\\b", "gi"), allergen);
  }
  return { formatted_ingredients: formatted, allergens };
};
