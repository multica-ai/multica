const fs = require("node:fs");
const path = require("node:path");

function copyConcurrency(
  value = process.env.MULTICA_CODEGRAPH_COPY_CONCURRENCY,
) {
  const raw = String(value || "").trim();
  const count = /^\d+$/.test(raw) ? Number(raw) : NaN;
  return Number.isInteger(count) && count >= 1 && count <= 16 ? count : 4;
}

// Keep the persisted layout unchanged while overlapping slow storage requests.
async function copySnapshot(
  source,
  destination,
  concurrency = copyConcurrency(),
) {
  if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 16)
    throw Error("Invalid copy concurrency");
  if (!(await fs.promises.lstat(source)).isDirectory())
    throw Error("Invalid snapshot directory");
  await fs.promises.mkdir(destination);
  let failed,
    files = 0,
    bytes = 0;
  async function* entries(from, to) {
    for (const entry of await fs.promises.readdir(from, {
      withFileTypes: true,
    })) {
      if (failed) return;
      const src = path.join(from, entry.name),
        dst = path.join(to, entry.name);
      if (entry.isDirectory()) {
        await fs.promises.mkdir(dst);
        yield* entries(src, dst);
      } else if (entry.isFile()) {
        yield { src, dst };
      } else {
        throw Error("Snapshot contains a symlink or special file");
      }
    }
  }
  const iterator = entries(source, destination);
  async function worker() {
    try {
      while (!failed) {
        const { value, done } = await iterator.next();
        if (done || failed) return;
        const size = (await fs.promises.stat(value.src)).size;
        await fs.promises.copyFile(
          value.src,
          value.dst,
          fs.constants.COPYFILE_EXCL,
        );
        bytes += size;
        files++;
      }
    } catch (error) {
      failed ||= error;
    }
  }
  // Drain in-flight writes before reporting failure; cleanup must not race them.
  await Promise.all(Array.from({ length: concurrency }, () => worker()));
  await iterator.return();
  if (failed) throw failed;
  return { files, bytes, concurrency };
}

module.exports = { copySnapshot, copyConcurrency };
