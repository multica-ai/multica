import { readdir, readFile } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { dirname, resolve, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../..");
const execute = promisify(execFile);
const forbidden = [
  "MULTICA_VERIFICATION_SESSION",
  "MULTICA_VERIFICATION_HOME",
  "verification-dev:request",
  "verification-dev:response",
  "multicaVerificationDev",
  "Verification launch capability",
];
let checked = 0;
async function inspect(directory) {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const path = join(directory, entry.name);
    if (entry.isDirectory()) await inspect(path);
    else if (entry.name.endsWith(".js") || entry.name.endsWith(".cjs")) {
      const text = await readFile(path, "utf8");
      checked++;
      if (forbidden.some((value) => text.includes(value)))
        throw new Error(`Development verification code leaked into ${path}`);
    }
  }
}
await inspect(join(root, "apps/desktop/out"));
if (!checked)
  throw new Error(
    "Build Desktop with electron-vite build before checking its output",
  );
for (const name of [
  "daemon/localhost",
  "localconversation/service",
  "verificationenv",
]) {
  const { stdout } = await execute(
    "go",
    ["list", "-tags=", "-f", '{{join .GoFiles " "}}', `./internal/${name}`],
    { cwd: join(root, "server"), timeout: 30000 },
  );
  if (
    stdout.includes("verification_dev.go") ||
    stdout.includes("home_dev.go") ||
    stdout.includes("verification_data_dev.go")
  )
    throw new Error(
      `Go production file selection includes verification implementation: ${name}`,
    );
}
console.log(
  `Production boundary passed: ${checked} JavaScript artifacts and three Go package selections.`,
);
