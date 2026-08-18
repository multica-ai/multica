#!/usr/bin/env node

import { mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { dirname, join, resolve } from "node:path";
import { deflateRawSync } from "node:zlib";

const root = resolve(import.meta.dirname, "..");
const sourcePath = resolve(root, "testdata/extensions/runtime-pool-demo");
const outputPath = resolve(root, "testdata/extensions/runtime-pool-demo.zip");

function crc32(data) {
  let value = 0xffffffff;
  for (const byte of data) {
    value ^= byte;
    for (let bit = 0; bit < 8; bit += 1) {
      value = (value >>> 1) ^ (value & 1 ? 0xedb88320 : 0);
    }
  }
  return (value ^ 0xffffffff) >>> 0;
}

function u16(value) {
  const out = Buffer.alloc(2);
  out.writeUInt16LE(value);
  return out;
}

function u32(value) {
  const out = Buffer.alloc(4);
  out.writeUInt32LE(value >>> 0);
  return out;
}

function zip(entries) {
  const locals = [];
  const central = [];
  let offset = 0;
  for (const [name, content] of entries) {
    const filename = Buffer.from(name);
    const raw = Buffer.from(content);
    const compressed = deflateRawSync(raw);
    const crc = crc32(raw);
    const local = Buffer.concat([
      u32(0x04034b50), u16(20), u16(0), u16(8), u16(0), u16(0), u32(crc),
      u32(compressed.length), u32(raw.length), u16(filename.length), u16(0), filename, compressed,
    ]);
    locals.push(local);
    central.push(Buffer.concat([
      u32(0x02014b50), u16(20), u16(20), u16(0), u16(8), u16(0), u16(0), u32(crc),
      u32(compressed.length), u32(raw.length), u16(filename.length), u16(0), u16(0), u16(0), u16(0),
      u32(0), u32(offset), filename,
    ]));
    offset += local.length;
  }
  const centralBytes = Buffer.concat(central);
  return Buffer.concat([
    ...locals,
    centralBytes,
    u32(0x06054b50), u16(0), u16(0), u16(entries.length), u16(entries.length),
    u32(centralBytes.length), u32(offset), u16(0),
  ]);
}

async function collectEntries(directory, prefix = "") {
  const entries = [];
  const children = await readdir(directory, { withFileTypes: true });
  children.sort((left, right) => left.name.localeCompare(right.name));
  for (const child of children) {
    if (child.name === ".DS_Store") continue;
    const relativePath = prefix ? `${prefix}/${child.name}` : child.name;
    const absolutePath = join(directory, child.name);
    if (child.isDirectory()) {
      entries.push(...await collectEntries(absolutePath, relativePath));
      continue;
    }
    if (child.isFile()) {
      entries.push([relativePath, await readFile(absolutePath)]);
    }
  }
  return entries;
}

const entries = await collectEntries(sourcePath);

await mkdir(dirname(outputPath), { recursive: true });
await writeFile(outputPath, zip(entries));
console.log(outputPath);
