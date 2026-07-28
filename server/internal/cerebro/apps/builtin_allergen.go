package apps

import (
	"crypto/sha256"
	"encoding/hex"
)

const allergenFormatterVersion = "1.0.3"

func allergenFormatterBundleFiles() []BundleFile {
	return []BundleFile{
		builtinFile("app.json", "application/json", string(allergenFormatterSnapshot)),
		builtinFile("frontend/index.html", "text/html; charset=utf-8", `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Allergen Formatter</title><style>body{font:15px system-ui;margin:0;padding:24px;color:#18181b}main{max-width:720px;margin:auto}textarea{box-sizing:border-box;width:100%;min-height:180px;padding:12px}button{margin-top:12px;padding:11px 16px}pre{white-space:pre-wrap}</style></head><body><main><h1>Allergen Formatter</h1><p>Paste ingredients to uppercase regulated allergens.</p><form><textarea id="ingredients" required></textarea><button>Format ingredients</button></form><pre id="result" hidden></pre></main><script type="module" src="./app.js" crossorigin="use-credentials"></script></body></html>`),
		builtinFile("frontend/app.js", "text/javascript; charset=utf-8", `import { createMulticaApp } from "/api/cerebro/apps-runtime/sdk/multica.js";
const identity=window.location.pathname.match(/\/apps-runtime\/apps\/([^/]+)\/([^/]+)(?:\/|$)/);
if(!identity)throw new Error("Invalid app runtime URL");
const multica=createMulticaApp({appId:decodeURIComponent(identity[1]),version:decodeURIComponent(identity[2])});
document.querySelector("form").addEventListener("submit",async(event)=>{event.preventDefault();const output=document.querySelector("#result");output.hidden=false;output.textContent="Formatting…";try{const result=await multica.workers.invoke({ingredients:document.querySelector("#ingredients").value});output.textContent=result.formatted_ingredients+"\n\nAllergens: "+result.allergens.join(", ")}catch{output.textContent="Formatting failed"}});`),
		builtinFile("backend/index.mjs", "text/javascript; charset=utf-8", `export default async ({ingredients}, multica) => {
  const response = await multica.connections.call("ai_gateway", "chat.completions", {
    model: "claude-haiku-4-5",
    messages: [
      { role: "system", content: "Return only a JSON object without Markdown fences. formatted_ingredients must be one string with regulated allergens uppercased. allergens must be an array of uppercase allergen names." },
      { role: "user", content: String(ingredients || "") }
    ],
    response_format: { type: "json_object" }
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
  for (const allergen of allergens) {
    const escaped = allergen.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
    formatted = formatted.replace(new RegExp("\\b" + escaped + "\\b", "gi"), allergen);
  }
  return { formatted_ingredients: formatted, allergens };
}`),
	}
}

func builtinFile(path, mediaType, content string) BundleFile {
	hash := sha256.Sum256([]byte(content))
	return BundleFile{Path: path, MediaType: mediaType, Content: []byte(content), SHA256: hex.EncodeToString(hash[:])}
}
