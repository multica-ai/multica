/**
 * Multica Client Review Widget
 *
 * Drop-in comment layer for dev sites. The client picks an element, types a
 * comment, and it becomes an assigned Multica issue with a screenshot.
 *
 * Zero dependencies, zero secrets. The widget only knows the ingest URL and
 * the review token from the query string; the mul_ token lives server-side.
 *
 * Usage:
 *   <script src="/review.js" data-ingest="https://review-ingest.flexmedia.is"></script>
 *
 * Activates only when ?review=<token> is present in the URL.
 */
(function () {
	'use strict';

	if (window.__multicaReview) return; // guard against double-injection
	window.__multicaReview = true;

	var script = document.currentScript;
	var INGEST = (script && script.dataset.ingest) || '';
	var TOKEN = new URLSearchParams(location.search).get('review');

	if (!TOKEN) return; // no token, no widget — silent no-op
	if (!INGEST) {
		console.warn('[multica-review] no data-ingest URL configured');
		return;
	}

	// ---------------------------------------------------------------- state
	var state = {
		picking: false,
		open: false,
		pins: [],
		target: null,
		sending: false
	};

	// ------------------------------------------------------------- selector
	// Stable-ish CSS selector for an element. Prefers ids and data-testids,
	// falls back to a nth-of-type path. Kept small deliberately; @medv/finder
	// is better but this avoids a build step and a third-party dependency.
	function selectorFor(el) {
		if (!el || el === document.body) return 'body';
		if (el.id && document.querySelectorAll('#' + CSS.escape(el.id)).length === 1) {
			return '#' + CSS.escape(el.id);
		}
		var testid = el.getAttribute('data-testid');
		if (testid) {
			var tsel = '[data-testid="' + testid + '"]';
			if (document.querySelectorAll(tsel).length === 1) return tsel;
		}
		var parts = [];
		var node = el;
		while (node && node.nodeType === 1 && node !== document.body && parts.length < 6) {
			var part = node.tagName.toLowerCase();
			var parent = node.parentElement;
			if (parent) {
				var sibs = Array.prototype.filter.call(parent.children, function (c) {
					return c.tagName === node.tagName;
				});
				if (sibs.length > 1) {
					part += ':nth-of-type(' + (sibs.indexOf(node) + 1) + ')';
				}
			}
			parts.unshift(part);
			// An id anywhere up the chain anchors the whole selector.
			if (parent && parent.id && document.querySelectorAll('#' + CSS.escape(parent.id)).length === 1) {
				parts.unshift('#' + CSS.escape(parent.id));
				break;
			}
			node = parent;
		}
		return parts.join(' > ');
	}

	function labelFor(el) {
		var text = (el.innerText || el.textContent || '').trim().replace(/\s+/g, ' ');
		if (text) return text.slice(0, 60);
		return el.tagName.toLowerCase();
	}

	// ----------------------------------------------------------- screenshot
	// Uses snapdom when present (loaded alongside this file). A hand-rolled
	// SVG foreignObject capture is NOT a workable fallback: Chromium taints the
	// canvas and toDataURL throws, so it can never return an image. Without
	// snapdom we simply send no screenshot — the comment still lands.
	function screenshot(el) {
		if (typeof window.snapdom !== 'function') return Promise.resolve(null);

		var timeout = new Promise(function (resolve) {
			setTimeout(function () { resolve(null); }, 5000); // never hang the submit
		});

		var capture = window
			.snapdom(el, { embedFonts: false, backgroundColor: '#ffffff' })
			.then(function (r) { return r.toPng(); })
			.then(function (img) {
				return img && img.src && img.src.length < 5 * 1024 * 1024 ? img.src : null;
			})
			.catch(function () { return null; });

		return Promise.race([capture, timeout]);
	}

	// ------------------------------------------------------------------ UI
	var css = [
		'.mcr-root{position:fixed;z-index:2147483000;font:14px/1.5 system-ui,-apple-system,sans-serif;color:#111}',
		'.mcr-launch{position:fixed;right:20px;bottom:20px;z-index:2147483000;background:#4f46e5;color:#fff;border:0;border-radius:999px;padding:12px 18px;font:600 14px system-ui,sans-serif;cursor:pointer;box-shadow:0 4px 14px rgba(0,0,0,.25)}',
		'.mcr-launch:hover{background:#4338ca}',
		'.mcr-panel{position:fixed;top:0;right:0;width:340px;height:100vh;background:#fff;border-left:1px solid #e5e7eb;box-shadow:-4px 0 24px rgba(0,0,0,.12);z-index:2147483001;display:flex;flex-direction:column;transform:translateX(100%);transition:transform .2s ease}',
		'.mcr-panel.mcr-on{transform:translateX(0)}',
		'.mcr-head{padding:16px;border-bottom:1px solid #e5e7eb;display:flex;align-items:center;justify-content:space-between}',
		'.mcr-title{font-weight:700}',
		'.mcr-sub{font-size:12px;color:#6b7280;margin-top:2px}',
		'.mcr-x{background:none;border:0;font-size:20px;cursor:pointer;color:#6b7280;line-height:1}',
		'.mcr-body{flex:1;overflow-y:auto;padding:16px}',
		'.mcr-btn{width:100%;background:#4f46e5;color:#fff;border:0;border-radius:8px;padding:10px;font:600 14px system-ui,sans-serif;cursor:pointer}',
		'.mcr-btn:hover{background:#4338ca}',
		'.mcr-btn[disabled]{opacity:.5;cursor:not-allowed}',
		'.mcr-btn.mcr-cancel{background:#e5e7eb;color:#374151}',
		'.mcr-ta{width:100%;box-sizing:border-box;border:1px solid #d1d5db;border-radius:8px;padding:8px;font:14px system-ui,sans-serif;resize:vertical;min-height:90px}',
		'.mcr-target{background:#f3f4f6;border-radius:6px;padding:8px;font-size:12px;color:#374151;margin-bottom:10px;word-break:break-all}',
		'.mcr-item{border:1px solid #e5e7eb;border-radius:8px;padding:10px;margin-bottom:8px;cursor:pointer}',
		'.mcr-item:hover{border-color:#4f46e5}',
		'.mcr-item-t{font-weight:600;font-size:13px;margin-bottom:2px}',
		'.mcr-item-m{font-size:11px;color:#6b7280}',
		'.mcr-empty{color:#6b7280;font-size:13px;text-align:center;padding:24px 0}',
		'.mcr-hl{position:fixed;pointer-events:none;z-index:2147483002;border:2px solid #4f46e5;background:rgba(79,70,229,.12);border-radius:3px;transition:all .05s}',
		'.mcr-tip{position:fixed;top:16px;left:50%;transform:translateX(-50%);z-index:2147483003;background:#111;color:#fff;padding:8px 16px;border-radius:999px;font:600 13px system-ui,sans-serif}',
		'.mcr-pin{position:absolute;z-index:2147482000;width:24px;height:24px;border-radius:999px 999px 999px 2px;background:#4f46e5;color:#fff;font:700 12px system-ui,sans-serif;display:flex;align-items:center;justify-content:center;cursor:pointer;box-shadow:0 2px 6px rgba(0,0,0,.3)}',
		'.mcr-pin.mcr-done{background:#059669}',
		'.mcr-msg{padding:8px;border-radius:6px;font-size:13px;margin-bottom:10px}',
		'.mcr-ok{background:#d1fae5;color:#065f46}',
		'.mcr-err{background:#fee2e2;color:#991b1b}'
	].join('');

	var styleEl = document.createElement('style');
	styleEl.textContent = css;
	document.head.appendChild(styleEl);

	var launcher = document.createElement('button');
	launcher.className = 'mcr-launch';
	launcher.textContent = '💬 Review';
	document.body.appendChild(launcher);

	var panel = document.createElement('div');
	panel.className = 'mcr-panel';
	panel.innerHTML =
		'<div class="mcr-head"><div><div class="mcr-title">Review</div>' +
		'<div class="mcr-sub mcr-who">Loading…</div></div>' +
		'<button class="mcr-x" aria-label="Close">×</button></div>' +
		'<div class="mcr-body"></div>';
	document.body.appendChild(panel);

	var body = panel.querySelector('.mcr-body');
	var who = panel.querySelector('.mcr-who');

	var highlight = null;
	var tip = null;

	// ------------------------------------------------------------- rendering
	function renderList(msg) {
		var html = '';
		if (msg) {
			html += '<div class="mcr-msg ' + (msg.ok ? 'mcr-ok' : 'mcr-err') + '">' + esc(msg.text) + '</div>';
		}
		html += '<button class="mcr-btn mcr-add">+ Add comment</button>';
		if (!state.pins.length) {
			html += '<div class="mcr-empty">No comments on this page yet.<br>Click “Add comment” and pick anything.</div>';
		} else {
			html += '<div style="margin-top:16px">';
			state.pins.forEach(function (p, i) {
				html +=
					'<div class="mcr-item" data-i="' + i + '">' +
					'<div class="mcr-item-t">' + esc(p.title || p.comment || '') + '</div>' +
					'<div class="mcr-item-m">' + esc(p.identifier || '') + ' · ' + esc(p.status || 'open') + '</div>' +
					'</div>';
			});
			html += '</div>';
		}
		body.innerHTML = html;
		body.querySelector('.mcr-add').onclick = startPicking;
		Array.prototype.forEach.call(body.querySelectorAll('.mcr-item'), function (n) {
			n.onclick = function () { scrollToPin(state.pins[+n.dataset.i]); };
		});
	}

	function renderComposer() {
		var label = state.target ? labelFor(state.target) : '';
		body.innerHTML =
			'<div class="mcr-target"><b>' + esc(state.target.tagName.toLowerCase()) + '</b>' +
			(label ? ' — ' + esc(label) : '') + '</div>' +
			'<textarea class="mcr-ta" placeholder="What should change here?"></textarea>' +
			'<div style="display:flex;gap:8px;margin-top:10px">' +
			'<button class="mcr-btn mcr-cancel mcr-back">Cancel</button>' +
			'<button class="mcr-btn mcr-send">Send</button></div>';
		var ta = body.querySelector('.mcr-ta');
		ta.focus();
		body.querySelector('.mcr-back').onclick = function () { state.target = null; renderList(); };
		body.querySelector('.mcr-send').onclick = function () { submit(ta.value); };
	}

	function esc(s) {
		return String(s == null ? '' : s).replace(/[&<>"']/g, function (c) {
			return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
		});
	}

	// --------------------------------------------------------------- picking
	function startPicking() {
		state.picking = true;
		setOpen(false);
		highlight = document.createElement('div');
		highlight.className = 'mcr-hl';
		document.body.appendChild(highlight);
		tip = document.createElement('div');
		tip.className = 'mcr-tip';
		tip.textContent = 'Click any element · Esc to cancel';
		document.body.appendChild(tip);
		document.addEventListener('mousemove', onMove, true);
		document.addEventListener('click', onPick, true);
		document.addEventListener('keydown', onEsc, true);
	}

	function stopPicking() {
		state.picking = false;
		document.removeEventListener('mousemove', onMove, true);
		document.removeEventListener('click', onPick, true);
		document.removeEventListener('keydown', onEsc, true);
		if (highlight) { highlight.remove(); highlight = null; }
		if (tip) { tip.remove(); tip = null; }
	}

	function isOurs(el) {
		return el.closest('.mcr-panel,.mcr-launch,.mcr-tip,.mcr-hl,.mcr-pin');
	}

	function onMove(e) {
		var el = document.elementFromPoint(e.clientX, e.clientY);
		if (!el || isOurs(el)) return;
		var r = el.getBoundingClientRect();
		highlight.style.cssText =
			'position:fixed;pointer-events:none;z-index:2147483002;border:2px solid #4f46e5;' +
			'background:rgba(79,70,229,.12);border-radius:3px;' +
			'top:' + r.top + 'px;left:' + r.left + 'px;width:' + r.width + 'px;height:' + r.height + 'px';
	}

	function onPick(e) {
		var el = document.elementFromPoint(e.clientX, e.clientY);
		if (!el || isOurs(el)) return;
		e.preventDefault();
		e.stopPropagation();
		state.target = el;
		stopPicking();
		setOpen(true);
		renderComposer();
	}

	function onEsc(e) {
		if (e.key === 'Escape') { stopPicking(); setOpen(true); renderList(); }
	}

	// ------------------------------------------------------------ submitting
	function submit(comment) {
		comment = (comment || '').trim();
		if (!comment) return;
		if (state.sending) return;
		state.sending = true;

		var sendBtn = body.querySelector('.mcr-send');
		if (sendBtn) { sendBtn.disabled = true; sendBtn.textContent = 'Sending…'; }

		var el = state.target;
		var sel = selectorFor(el);

		screenshot(el).then(function (shot) {
			return fetch(INGEST.replace(/\/$/, '') + '/comment', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({
					token: TOKEN,
					url: location.href,
					path: location.pathname,
					selector: sel,
					label: labelFor(el),
					comment: comment,
					screenshot: shot,
					viewport: { w: innerWidth, h: innerHeight },
					userAgent: navigator.userAgent
				})
			});
		}).then(function (res) {
			return res.json().then(function (d) { return { ok: res.ok, data: d }; });
		}).then(function (r) {
			state.sending = false;
			state.target = null;
			if (r.ok) {
				loadPins();
				renderList({ ok: true, text: 'Sent' + (r.data.identifier ? ' — ' + r.data.identifier : '') });
			} else {
				renderList({ ok: false, text: (r.data && r.data.error) || 'Could not send' });
			}
		}).catch(function (err) {
			state.sending = false;
			state.target = null;
			renderList({ ok: false, text: 'Network error: ' + err.message });
		});
	}

	// ------------------------------------------------------------------ pins
	function loadPins() {
		fetch(INGEST.replace(/\/$/, '') + '/pins?token=' + encodeURIComponent(TOKEN) +
			'&path=' + encodeURIComponent(location.pathname))
			.then(function (r) { return r.json(); })
			.then(function (d) {
				state.pins = (d && d.pins) || [];
				if (d && d.client) who.textContent = d.client;
				drawPins();
				if (state.open && !state.target) renderList();
			})
			.catch(function () { /* offline ingest — widget still usable for new comments */ });
	}

	function drawPins() {
		Array.prototype.forEach.call(document.querySelectorAll('.mcr-pin'), function (n) { n.remove(); });
		state.pins.forEach(function (p, i) {
			if (!p.selector) return;
			var el;
			try { el = document.querySelector(p.selector); } catch (e) { el = null; }
			if (!el) { p.orphaned = true; return; } // shown in the list, not on the page
			var r = el.getBoundingClientRect();
			var pin = document.createElement('div');
			pin.className = 'mcr-pin' + (p.status === 'done' ? ' mcr-done' : '');
			pin.textContent = i + 1;
			pin.style.top = (r.top + scrollY - 8) + 'px';
			pin.style.left = (r.left + scrollX - 8) + 'px';
			pin.title = p.title || p.comment || '';
			pin.onclick = function () { setOpen(true); renderList(); };
			document.body.appendChild(pin);
		});
	}

	function scrollToPin(p) {
		if (!p || !p.selector) return;
		var el;
		try { el = document.querySelector(p.selector); } catch (e) { return; }
		if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' });
	}

	// ----------------------------------------------------------------- wiring
	function setOpen(v) {
		state.open = v;
		panel.classList.toggle('mcr-on', v);
		launcher.style.display = v ? 'none' : '';
	}

	launcher.onclick = function () { setOpen(true); renderList(); };
	panel.querySelector('.mcr-x').onclick = function () { setOpen(false); };

	var redrawTimer;
	function scheduleRedraw() {
		clearTimeout(redrawTimer);
		redrawTimer = setTimeout(drawPins, 120);
	}
	addEventListener('resize', scheduleRedraw);
	addEventListener('scroll', scheduleRedraw, true);

	renderList();
	loadPins();
})();
