import { execFileSync } from "node:child_process";
import { mkdirSync, rmSync, writeFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const value = (flag, fallback) => args.includes(flag) ? args[args.indexOf(flag) + 1] : fallback;
const platform = value("--target-platform", process.platform);
const arch = value("--target-arch", process.arch);
const output = resolve(root, "resources/native/Multica Computer Use.app");
if (platform !== "darwin") {
  rmSync(output, { force: true, recursive: true });
} else {
  if (process.platform !== "darwin" || !["arm64", "x64"].includes(arch)) throw new Error("Computer Use builds require macOS and an arm64 or x64 target");
  const executable = resolve(output, "Contents/MacOS/multica-computer-use");
  mkdirSync(dirname(executable), { recursive: true });
  execFileSync("xcrun", ["swiftc", "-parse-as-library", "-swift-version", "5", "-O", "-target", `${arch === "x64" ? "x86_64" : "arm64"}-apple-macos13.0`, "-framework", "AppKit", "-framework", "ScreenCaptureKit", "-framework", "CoreImage", resolve(root, "native/computer-use/ComputerUse.swift"), "-o", executable], { stdio: "inherit" });
  writeFileSync(resolve(output, "Contents/Info.plist"), `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>ai.multica.computer-use</string>
<key>CFBundleName</key><string>Multica Computer Use</string>
<key>CFBundleExecutable</key><string>multica-computer-use</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleVersion</key><string>1</string>
<key>CFBundleShortVersionString</key><string>1.0</string>
<key>LSMinimumSystemVersion</key><string>13.0</string>
<key>LSUIElement</key><true/>
</dict></plist>`);
  // Development has a stable designated identity; release signing replaces it.
  execFileSync("codesign", ["--force", "--sign", "-", "--identifier", "ai.multica.computer-use", output], { stdio: "inherit" });
}
