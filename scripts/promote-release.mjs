import { execFileSync } from "node:child_process";
import { pathToFileURL } from "node:url";
import { parse } from "yaml";

export const channels = [
  ["latest-mac.yml", "mac", "arm64", "zip"],
  ["latest-x64-mac.yml", "mac", "x64", "zip"],
  ["latest.yml", "windows", "x64", "exe"],
  ["latest-arm64.yml", "windows", "arm64", "exe"],
  ["latest-linux.yml", "linux", "x64", "AppImage"],
  ["latest-linux-arm64.yml", "linux", "arm64", "AppImage"],
];

function stableVersion(tag) {
  if (!/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(tag)) {
    throw new Error(`Expected a stable vX.Y.Z tag, got ${tag}`);
  }
  return tag.slice(1).split(".").map(BigInt);
}

export function isNewer(tag, latestTag) {
  const target = stableVersion(tag);
  if (latestTag === null) return true;
  const latest = stableVersion(latestTag);
  for (let i = 0; i < target.length; i++) {
    if (target[i] !== latest[i]) return target[i] > latest[i];
  }
  return false;
}

export async function validateRelease(tag, release, assets, readMetadata) {
  stableVersion(tag);
  if (release.tag_name !== tag || release.draft || release.prerelease) {
    throw new Error("Promotion requires the matching published stable release");
  }
  const version = tag.slice(1);
  const uploaded = new Map(assets.filter((asset) =>
    asset.state === "uploaded" && Number.isSafeInteger(asset.size) && asset.size > 0,
  ).map((asset) => [asset.name, asset]));
  const problems = [];
  for (const [name, platform, arch, extension] of channels) {
    const asset = uploaded.get(name);
    if (!asset) { problems.push(`Missing ${name}`); continue; }
    // Authentication/transport failures are workflow errors, not incomplete
    // artifacts. Only malformed metadata belongs in the waiting result.
    const contents = await readMetadata(asset);
    try {
      const metadata = parse(contents);
      if (metadata?.version !== version) throw new Error(`version must be ${version}`);
      if (!Array.isArray(metadata.files) || metadata.files.length === 0) {
        throw new Error("files must contain update artifacts");
      }
      // electron-builder uses package-format architecture names on Linux:
      // AppImage/RPM can use x86_64 or aarch64, while DEB uses amd64/arm64.
      const arches = platform === "linux"
        ? arch === "x64" ? ["x64", "x86_64", "amd64"] : ["arm64", "aarch64"]
        : [arch];
      const expected = arches.map((cpu) => `multica-desktop-${version}-${platform}-${cpu}`);
      let hasUpdatePackage = false;
      for (const file of metadata.files) {
        if (typeof file?.url !== "string") throw new Error("invalid files[].url");
        const filename = decodeURIComponent(file.url);
        if (!expected.some((prefix) => filename.startsWith(`${prefix}.`)) || /[/\\]/.test(filename)) {
          throw new Error(`wrong version/platform/architecture in ${file.url}`);
        }
        const binary = uploaded.get(filename);
        if (!binary) throw new Error(`missing ${filename}`);
        if (!Number.isSafeInteger(file.size) || file.size !== binary.size) {
          throw new Error(`size mismatch for ${filename}`);
        }
        if (expected.some((prefix) => filename === `${prefix}.${extension}`)) hasUpdatePackage = true;
      }
      if (!hasUpdatePackage) throw new Error(`missing ${extension} update package`);
    } catch (error) {
      problems.push(`${name}: ${error.message}`);
    }
  }
  // Both naming schemes are consumed by shipped CLI clients/installers.
  for (const os of ["darwin", "linux", "windows"]) {
    for (const arch of ["amd64", "arm64"]) {
      const ext = os === "windows" ? "zip" : "tar.gz";
      for (const name of [`multica_${os}_${arch}.${ext}`, `multica-cli-${version}-${os}-${arch}.${ext}`]) {
        if (!uploaded.has(name)) problems.push(`Missing ${name}`);
      }
    }
  }
  if (!uploaded.has("checksums.txt")) problems.push("Missing checksums.txt");
  return problems;
}

// All callers run in the workflow's single stable-channel concurrency group.
// Missing uploads are a normal waiting state; malformed API/auth responses fail.
export async function promoteRelease({ tag, getRelease, getAssets, readMetadata, getLatest, promote }) {
  stableVersion(tag);
  const release = await getRelease(tag);
  const problems = await validateRelease(tag, release, await getAssets(release.id), readMetadata);
  if (problems.length) return { status: "waiting", problems };
  // Read Latest immediately before writing it, inside the serialized job.
  const latest = await getLatest();
  if (!isNewer(tag, latest)) return { status: "skipped", latest };
  await promote(tag);
  return { status: "promoted" };
}

function gh(args) {
  return execFileSync("gh", args, { encoding: "utf8", timeout: 60_000, maxBuffer: 8 * 1024 * 1024 });
}

export async function main() {
  const tag = process.env.RELEASE_TAG;
  stableVersion(tag);
  const repo = process.env.GH_REPO;
  if (repo !== "multica-ai/multica") throw new Error("GH_REPO must be multica-ai/multica");
  const api = (path) => JSON.parse(gh(["api", `repos/${repo}/${path}`]));
  const result = await promoteRelease({
    tag,
    getRelease: (tag) => api(`releases/tags/${encodeURIComponent(tag)}`),
    getAssets: (id) => JSON.parse(gh(["api", `repos/${repo}/releases/${id}/assets?per_page=100`, "--paginate", "--slurp"])).flat(),
    readMetadata: (asset) => gh(["api", `repos/${repo}/releases/assets/${asset.id}`, "-H", "Accept: application/octet-stream"]),
    getLatest: () => {
      try { return api("releases/latest").tag_name; }
      catch (error) {
        if (/\(HTTP 404\)/.test(String(error.stderr))) return null;
        throw error;
      }
    },
    promote: (tag) => gh(["release", "edit", tag, "--repo", repo, "--latest"]),
  });
  console.log(JSON.stringify(result, null, 2));
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => { console.error(error.message); process.exitCode = 1; });
}
