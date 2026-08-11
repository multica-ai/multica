#!/usr/bin/env node
/**
 * mint-review-link — issue a signed review link for a client.
 *
 * Usage:
 *   mint-review-link <project> <client name> [--days 30] [--base https://dart.flexmedia.is]
 *
 * Example:
 *   mint-review-link ips "Jón at IPS" --days 60
 *
 * Reads REVIEW_SECRET from the environment (or ../.env). Prints the link only —
 * the secret is never echoed.
 */

import { createHmac } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));

function loadEnv() {
	if (process.env.REVIEW_SECRET) return;
	for (const p of [join(here, '..', '.env'), join(here, '.env')]) {
		try {
			for (const line of readFileSync(p, 'utf8').split('\n')) {
				const m = /^\s*([A-Z_]+)\s*=\s*(.*)\s*$/.exec(line);
				if (m && !process.env[m[1]]) {
					process.env[m[1]] = m[2].replace(/^["']|["']$/g, '');
				}
			}
		} catch {
			/* no env file here */
		}
	}
}

loadEnv();

const args = process.argv.slice(2);
if (!args.length || args.includes('--help') || args.includes('-h')) {
	console.log(`
mint-review-link — issue a signed review link for a client

USAGE
  mint-review-link <project> <client name> [flags]

FLAGS
  --days <n>     link lifetime in days (default 30)
  --base <url>   site the link points at (default from REVIEW_BASE_<PROJECT>)
  --help         this help

EXAMPLES
  mint-review-link ips "Jón at IPS"
  mint-review-link ips "Jón at IPS" --days 60 --base https://dart.flexmedia.is
`);
	process.exit(0);
}

function flag(name, fallback) {
	const i = args.indexOf(`--${name}`);
	if (i === -1) return fallback;
	const v = args[i + 1];
	args.splice(i, 2);
	return v ?? fallback;
}

const days = Number(flag('days', '30'));
const baseFlag = flag('base', null);

const [project, ...clientParts] = args;
const client = clientParts.join(' ').trim();

if (!project || !client) {
	console.error('error: both <project> and <client name> are required');
	process.exit(1);
}
if (!process.env.REVIEW_SECRET) {
	console.error('error: REVIEW_SECRET is not set (env or .env)');
	process.exit(1);
}
if (!Number.isFinite(days) || days <= 0) {
	console.error('error: --days must be a positive number');
	process.exit(1);
}

const base =
	baseFlag ||
	process.env[`REVIEW_BASE_${project.toUpperCase().replace(/-/g, '_')}`] ||
	'';

const payload = {
	project,
	client,
	exp: Math.floor(Date.now() / 1000) + days * 86400
};

const body = Buffer.from(JSON.stringify(payload)).toString('base64url');
const sig = createHmac('sha256', process.env.REVIEW_SECRET).update(body).digest('base64url');
const token = `${body}.${sig}`;

const expires = new Date(payload.exp * 1000).toISOString().slice(0, 10);

console.log('');
console.log(`  project:  ${project}`);
console.log(`  client:   ${client}`);
console.log(`  expires:  ${expires} (${days} days)`);
console.log('');
if (base) {
	console.log(`  ${base.replace(/\/$/, '')}/?review=${token}`);
} else {
	console.log(`  token: ${token}`);
	console.log(`  (no base URL — pass --base or set REVIEW_BASE_${project.toUpperCase()})`);
}
console.log('');
