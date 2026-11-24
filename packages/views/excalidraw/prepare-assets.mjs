import { copyFileSync, cpSync, mkdirSync } from "node:fs";
import { join } from "node:path";
import { argv } from "node:process";
import { fileURLToPath, URL } from "node:url";

// The package builds both development and production entry points. Their
// hashed font names match. Distribute the production copy in every build.
const entry = import.meta.resolve("@excalidraw/excalidraw");
const fonts = fileURLToPath(new URL("../prod/fonts/", entry));
const output = join(argv[2], "assets/excalidraw");
mkdirSync(output, { recursive: true });

// Liberation Sans in the package is for server-side export. These are the
// browser families covered by the accompanying notices.
for (const family of ["Assistant", "Cascadia", "ComicShanns", "Excalifont", "Lilita", "Nunito", "Virgil", "Xiaolai"]) {
  cpSync(join(fonts, family), join(output, "fonts", family), { recursive: true });
}
copyFileSync(new URL("./THIRD-PARTY-NOTICES.txt", import.meta.url), join(output, "THIRD-PARTY-NOTICES.txt"));
