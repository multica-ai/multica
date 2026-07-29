package apps

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

const allergenFormatterVersion = "1.0.4"

//go:embed builtin/allergen/backend/index.mjs
var allergenFormatterBackend []byte

func allergenFormatterBundleFiles() []BundleFile {
	return []BundleFile{
		builtinFile("app.json", "application/json", string(allergenFormatterSnapshot)),
		builtinFile("frontend/index.html", "text/html; charset=utf-8", `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Allergen Formatter</title><style>body{font:15px system-ui;margin:0;padding:24px;color:#18181b}main{max-width:720px;margin:auto}textarea{box-sizing:border-box;width:100%;min-height:180px;padding:12px}button{margin-top:12px;padding:11px 16px}pre{white-space:pre-wrap}</style></head><body><main><h1>Allergen Formatter</h1><p>Paste ingredients to uppercase regulated allergens.</p><form><textarea id="ingredients" required></textarea><button>Format ingredients</button></form><pre id="result" hidden></pre></main><script type="module" src="./app.js" crossorigin="use-credentials"></script></body></html>`),
		builtinFile("frontend/app.js", "text/javascript; charset=utf-8", `import { createMulticaApp } from "/api/cerebro/apps-runtime/sdk/multica.js";
const identity=window.location.pathname.match(/\/apps-runtime\/apps\/([^/]+)\/([^/]+)(?:\/|$)/);
if(!identity)throw new Error("Invalid app runtime URL");
const multica=createMulticaApp({appId:decodeURIComponent(identity[1]),version:decodeURIComponent(identity[2])});
document.querySelector("form").addEventListener("submit",async(event)=>{event.preventDefault();const ingredients=document.querySelector("#ingredients").value;const output=document.querySelector("#result");output.hidden=false;output.textContent="Formatting…";try{const result=await multica.workers.invoke({ingredients});const formatted=typeof result.formatted_ingredients==="string"&&result.formatted_ingredients.trim()?result.formatted_ingredients:ingredients;const allergens=Array.isArray(result.allergens)?result.allergens:[];output.textContent="Ingredients:\n"+formatted+"\n\nAllergens:\n"+allergens.join(", ")}catch{output.textContent="Formatting failed"}});`),
		builtinFile("backend/index.mjs", "text/javascript; charset=utf-8", string(allergenFormatterBackend)),
	}
}

func builtinFile(path, mediaType, content string) BundleFile {
	hash := sha256.Sum256([]byte(content))
	return BundleFile{Path: path, MediaType: mediaType, Content: []byte(content), SHA256: hex.EncodeToString(hash[:])}
}
