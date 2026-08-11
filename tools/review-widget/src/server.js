/**
 * Multica Client Review — ingest service
 *
 * The trust boundary between a client's browser and the Multica API.
 *
 * The widget holds no secrets. This service holds the mul_ token, verifies
 * signed review links, and can only ever create issues in the workspace a
 * token is mapped to. It never exposes broader Multica capability.
 *
 * Env (see .env.dev / .env.prod):
 *   PORT                 listen port (default 8091)
 *   MULTICA_API          Multica backend base URL (default http://localhost:8090)
 *   MULTICA_TOKEN        mul_ API token  — SECRET
 *   REVIEW_SECRET        HMAC signing secret for review links — SECRET
 *   REVIEW_PROJECTS_FILE path to projects.json (default ../projects.json)
 *   ALLOWED_ORIGINS      comma-separated origins allowed to POST (CORS)
 */

import { createServer } from 'node:http';
import { createHmac, timingSafeEqual, randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const PORT = Number(process.env.PORT || 8091);
const MULTICA_API = (process.env.MULTICA_API || 'http://localhost:8090').replace(/\/$/, '');
const MULTICA_TOKEN = process.env.MULTICA_TOKEN || '';
const REVIEW_SECRET = process.env.REVIEW_SECRET || '';
const ALLOWED_ORIGINS = (process.env.ALLOWED_ORIGINS || '')
	.split(',')
	.map((s) => s.trim())
	.filter(Boolean);

// Project mapping lives in a JSON file (projects.json by default), not an env
// var — it is structured config, and shell quoting mangles inline JSON.
const PROJECTS_FILE =
	process.env.REVIEW_PROJECTS_FILE ||
	join(dirname(fileURLToPath(import.meta.url)), '..', 'projects.json');

let PROJECTS = {};
try {
	PROJECTS = JSON.parse(readFileSync(PROJECTS_FILE, 'utf8'));
} catch (err) {
	console.error(`could not read ${PROJECTS_FILE}: ${err.message} — no projects configured`);
}

if (!MULTICA_TOKEN) console.error('WARNING: MULTICA_TOKEN unset — issue creation will fail');
if (!REVIEW_SECRET) console.error('WARNING: REVIEW_SECRET unset — token verification will fail');

const MAX_BODY = 6 * 1024 * 1024; // 6MB — screenshots are the bulk of it

// Machine-readable marker so /pins can tell review issues from dev-generated
// ones even when they share a project.
const REVIEW_MARKER = '<!-- multica-review -->';

// ------------------------------------------------------------------- tokens
// A review token is base64url(payload).base64url(hmac), payload =
// {project, client, exp}. Stateless to verify; revoke by rotating the secret.

const b64u = {
	encode: (buf) => Buffer.from(buf).toString('base64url'),
	decode: (str) => Buffer.from(str, 'base64url')
};

export function mintToken(payload, secret = REVIEW_SECRET) {
	const body = b64u.encode(JSON.stringify(payload));
	const sig = createHmac('sha256', secret).update(body).digest();
	return `${body}.${b64u.encode(sig)}`;
}

function verifyToken(token) {
	if (!token || typeof token !== 'string' || !REVIEW_SECRET) return null;
	const parts = token.split('.');
	if (parts.length !== 2) return null;

	const expected = createHmac('sha256', REVIEW_SECRET).update(parts[0]).digest();
	const got = b64u.decode(parts[1]);
	if (got.length !== expected.length) return null;
	if (!timingSafeEqual(got, expected)) return null;

	let payload;
	try {
		payload = JSON.parse(b64u.decode(parts[0]).toString('utf8'));
	} catch {
		return null;
	}
	if (payload.exp && Date.now() / 1000 > payload.exp) return null;
	if (!payload.project || !PROJECTS[payload.project]) return null;

	return { ...payload, config: PROJECTS[payload.project] };
}

// -------------------------------------------------------------- rate limits
// One review link is one client. Cap comment spam without needing storage.
const buckets = new Map();
const RATE_MAX = 20;
const RATE_WINDOW = 10 * 60 * 1000; // 20 comments / 10 min per token

function rateLimited(key) {
	const now = Date.now();
	const hits = (buckets.get(key) || []).filter((t) => now - t < RATE_WINDOW);
	if (hits.length >= RATE_MAX) {
		buckets.set(key, hits);
		return true;
	}
	hits.push(now);
	buckets.set(key, hits);
	return false;
}

// ------------------------------------------------------------ multica calls
async function multica(path, init = {}) {
	const res = await fetch(`${MULTICA_API}${path}`, {
		...init,
		headers: {
			Authorization: `Bearer ${MULTICA_TOKEN}`,
			...(init.headers || {})
		}
	});
	const text = await res.text();
	let data = null;
	try {
		data = text ? JSON.parse(text) : null;
	} catch {
		data = { raw: text };
	}
	if (!res.ok) {
		const err = new Error(`multica ${path} → ${res.status}`);
		err.status = res.status;
		err.body = data;
		throw err;
	}
	return data;
}

// The review token is a working credential — never let it reach an issue
// description, which syncs to GitHub and is visible to anyone with the board.
function stripToken(rawUrl) {
	try {
		const u = new URL(rawUrl);
		u.searchParams.delete('review');
		return u.toString();
	} catch {
		return String(rawUrl).replace(/([?&])review=[^&]*/g, '$1').replace(/[?&]$/, '');
	}
}

async function createIssue({ config, client, comment, url, path, selector, label, viewport }) {
	const title = `[Review] ${comment.split('\n')[0].slice(0, 60)}`;
	const description = [
		comment,
		'',
		'---',
		`Page: ${stripToken(url)}`,
		`Element: \`${selector}\`${label ? ` — ${label}` : ''}`,
		viewport ? `Viewport: ${viewport.w}×${viewport.h}` : '',
		`Reported by: ${client} via review link`,
		REVIEW_MARKER
	]
		.filter(Boolean)
		.join('\n');

	const body = {
		title,
		description,
		priority: 'medium',
		// Land in `backlog`, NOT `todo`. A client comment is a request, not an
		// approved work order — it must be triaged by a human before any agent
		// touches the codebase. `backlog` is the one status the handoff contract
		// allows to be unassigned, and the daemon does not dispatch from it.
		//
		// This is load-bearing. Creating these as assigned `todo` had agents
		// opening real PRs against the live repo within seconds of a comment
		// (ips PR #264, from a test comment, reached CI before being caught).
		status: config.status || 'backlog'
	};
	if (config.project_id) body.project_id = config.project_id;
	// Assign only when a project explicitly opts in. Assignment plus `todo` is
	// what makes the daemon pick an issue up, so the default (no assignee,
	// backlog) is deliberately inert.
	if (config.assignee_id) {
		body.assignee_type = config.assignee_type || 'user';
		body.assignee_id = config.assignee_id;
	}

	return multica(`/api/issues?workspace_id=${encodeURIComponent(config.workspace_id)}`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body)
	});
}

// Uploads go through /api/upload-file with an issue_id form field — there is
// no POST on /api/issues/{id}/attachments (that path is GET-only, hence 405).
async function attachScreenshot(issueId, dataUrl, workspaceId) {
	const m = /^data:(image\/[a-z+]+);base64,(.+)$/i.exec(dataUrl || '');
	if (!m) return null;
	const buf = Buffer.from(m[2], 'base64');
	if (buf.length > 5 * 1024 * 1024) return null;

	const form = new FormData();
	form.append('file', new Blob([buf], { type: m[1] }), `review-${randomUUID().slice(0, 8)}.png`);
	form.append('issue_id', issueId);

	return multica(`/api/upload-file?workspace_id=${encodeURIComponent(workspaceId)}`, {
		method: 'POST',
		body: form
	});
}

// ------------------------------------------------------------------ helpers
function readBody(req) {
	return new Promise((resolve, reject) => {
		let size = 0;
		const chunks = [];
		req.on('data', (c) => {
			size += c.length;
			if (size > MAX_BODY) {
				reject(Object.assign(new Error('payload too large'), { status: 413 }));
				req.destroy();
				return;
			}
			chunks.push(c);
		});
		req.on('end', () => {
			try {
				resolve(JSON.parse(Buffer.concat(chunks).toString('utf8') || '{}'));
			} catch {
				reject(Object.assign(new Error('invalid JSON'), { status: 400 }));
			}
		});
		req.on('error', reject);
	});
}

function cors(req, res) {
	const origin = req.headers.origin;
	// Empty ALLOWED_ORIGINS means dev mode: reflect the caller.
	if (!ALLOWED_ORIGINS.length || (origin && ALLOWED_ORIGINS.includes(origin))) {
		res.setHeader('Access-Control-Allow-Origin', origin || '*');
		res.setHeader('Vary', 'Origin');
	}
	res.setHeader('Access-Control-Allow-Methods', 'GET,POST,OPTIONS');
	res.setHeader('Access-Control-Allow-Headers', 'Content-Type');
}

function send(res, status, obj) {
	const payload = JSON.stringify(obj);
	res.writeHead(status, {
		'Content-Type': 'application/json',
		'Content-Length': Buffer.byteLength(payload)
	});
	res.end(payload);
}

// -------------------------------------------------------------------- routes
async function handle(req, res) {
	const url = new URL(req.url, `http://${req.headers.host || 'localhost'}`);
	cors(req, res);

	if (req.method === 'OPTIONS') {
		res.writeHead(204);
		res.end();
		return;
	}

	if (url.pathname === '/health') {
		return send(res, 200, {
			status: 'ok',
			projects: Object.keys(PROJECTS),
			multica: MULTICA_API,
			configured: Boolean(MULTICA_TOKEN && REVIEW_SECRET)
		});
	}

	// GET /pins?token=…&path=…  — existing review issues for this page
	if (url.pathname === '/pins' && req.method === 'GET') {
		const auth = verifyToken(url.searchParams.get('token'));
		if (!auth) return send(res, 401, { error: 'invalid or expired review link' });

		const wanted = url.searchParams.get('path') || '';
		try {
			const qs = new URLSearchParams({ workspace_id: auth.config.workspace_id });
			if (auth.config.project_id) qs.set('project_id', auth.config.project_id);
			const data = await multica(`/api/issues?${qs}`);
			const issues = Array.isArray(data) ? data : data?.issues || data?.data || [];

			// Cancelled work is not something the client should still see pinned
			// to the page — it reads as "you ignored me" rather than "we decided
			// against this". Done stays visible so they can see what was fixed.
			const HIDDEN = new Set(['cancelled']);

			const pins = issues
				.filter((i) => typeof i.description === 'string' && i.description.includes(REVIEW_MARKER))
				.filter((i) => !HIDDEN.has(i.status))
				.map((i) => {
					const sel = /Element: `([^`]+)`/.exec(i.description || '');
					const pg = /Page: (\S+)/.exec(i.description || '');
					let ipath = '';
					try {
						ipath = pg ? new URL(pg[1]).pathname : '';
					} catch {
						ipath = '';
					}
					return {
						id: i.id,
						identifier: i.identifier || i.key || '',
						title: i.title,
						status: i.status,
						selector: sel ? sel[1] : null,
						path: ipath
					};
				})
				.filter((p) => !wanted || p.path === wanted);

			return send(res, 200, { pins, client: auth.client, project: auth.project });
		} catch (err) {
			console.error('pins failed', err.status, err.body || err.message);
			return send(res, 502, { error: 'could not load comments' });
		}
	}

	// POST /comment — the main path: comment → assigned Multica issue
	if (url.pathname === '/comment' && req.method === 'POST') {
		let payload;
		try {
			payload = await readBody(req);
		} catch (err) {
			return send(res, err.status || 400, { error: err.message });
		}

		const auth = verifyToken(payload.token);
		if (!auth) return send(res, 401, { error: 'invalid or expired review link' });

		if (rateLimited(payload.token)) {
			return send(res, 429, { error: 'too many comments — try again shortly' });
		}

		const comment = String(payload.comment || '').trim().slice(0, 4000);
		if (!comment) return send(res, 400, { error: 'comment is empty' });

		try {
			const issue = await createIssue({
				config: auth.config,
				client: auth.client || 'client',
				comment,
				url: String(payload.url || '').slice(0, 500),
				path: String(payload.path || '').slice(0, 300),
				selector: String(payload.selector || '').slice(0, 500),
				label: String(payload.label || '').slice(0, 120),
				viewport: payload.viewport
			});

			const issueId = issue?.id || issue?.issue?.id;
			let attached = false;
			if (issueId && payload.screenshot) {
				try {
					await attachScreenshot(issueId, payload.screenshot, auth.config.workspace_id);
					attached = true;
				} catch (err) {
					// A failed screenshot must never lose the comment.
					console.error('attachment failed', err.status, err.body || err.message);
				}
			}

			console.log(`comment → ${issue?.identifier || issueId} (${auth.project}/${auth.client})`);
			return send(res, 200, {
				ok: true,
				id: issueId,
				identifier: issue?.identifier || issue?.key || '',
				screenshot: attached
			});
		} catch (err) {
			console.error('create failed', err.status, err.body || err.message);
			return send(res, 502, { error: 'could not create issue' });
		}
	}

	return send(res, 404, { error: 'not found' });
}

createServer((req, res) => {
	handle(req, res).catch((err) => {
		console.error('unhandled', err);
		if (!res.headersSent) send(res, 500, { error: 'internal error' });
	});
}).listen(PORT, '127.0.0.1', () => {
	console.log(`review ingest on 127.0.0.1:${PORT} → ${MULTICA_API}`);
	console.log(`projects: ${Object.keys(PROJECTS).join(', ') || '(none)'}`);
});
