// ==UserScript==
// @name         Multica Non-Terminal Issue Auto-Pin & Sync
// @namespace    https://multica.ai/
// @version      2.0.0
// @description  Pin open Issues automatically and synchronize manual unpin/comment actions with Issue status.
// @author       Multica Agent
// @match        https://multica.ai/*
// @match        https://*.multica.ai/*
// @match        http://localhost/*
// @match        http://127.0.0.1/*
// @run-at       document-start
// @grant        none
// ==/UserScript==

(function (root) {
  "use strict";

  const INSTALL_KEY = "__multicaAutoPinController__";
  const OPEN_ISSUES_PATH = "/api/issues?open_only=true";
  const PINS_PATH = "/api/pins";
  const TERMINAL_STATUSES = new Set(["done", "cancelled"]);
  const NON_WORKSPACE_PATHS = new Set([
    "api",
    "auth",
    "changelog",
    "docs",
    "download",
    "login",
    "_next",
  ]);

  function isNonTerminal(issue) {
    return Boolean(
      issue &&
        typeof issue.id === "string" &&
        issue.id &&
        typeof issue.status === "string" &&
        !TERMINAL_STATUSES.has(issue.status),
    );
  }

  function decodePathPart(value) {
    try {
      return decodeURIComponent(value);
    } catch {
      return value;
    }
  }

  function requestInfo(input, init, target) {
    const request = input && typeof input === "object" ? input : null;
    const rawUrl = typeof input === "string" ? input : request?.url;
    if (typeof rawUrl !== "string" || !rawUrl) return null;

    let url;
    try {
      url = new URL(rawUrl, target?.location?.href || "http://localhost/");
    } catch {
      return null;
    }

    const method = String(init?.method || request?.method || "GET").toUpperCase();
    const path = url.pathname.replace(/\/+$/, "") || "/";
    return { method, path };
  }

  function issueIdFromCommentPath(path) {
    const match = path.match(/^\/api\/issues\/([^/]+)\/comments$/);
    return match ? decodePathPart(match[1]) : null;
  }

  function issueIdFromIssueUpdatePath(path) {
    const match = path.match(/^\/api\/issues\/([^/]+)$/);
    return match ? decodePathPart(match[1]) : null;
  }

  function issueIdFromIssuePinDeletePath(path) {
    const match = path.match(/^\/api\/pins\/issue\/([^/]+)$/);
    return match ? decodePathPart(match[1]) : null;
  }

  function issuesFromResponse(data) {
    if (
      !data ||
      typeof data !== "object" ||
      Array.isArray(data) ||
      !Array.isArray(data.issues) ||
      typeof data.total !== "number"
    ) {
      return null;
    }
    return data.issues;
  }

  function pinsFromResponse(data) {
    if (Array.isArray(data)) return data;
    if (data && Array.isArray(data.pins)) return data.pins;
    return null;
  }

  function readCookie(target, name) {
    const cookie = target?.document?.cookie;
    if (typeof cookie !== "string") return null;
    const prefix = `${name}=`;
    const part = cookie.split("; ").find((entry) => entry.startsWith(prefix));
    return part ? part.slice(prefix.length) || null : null;
  }

  function workspaceSlug(target) {
    let pathname = target?.location?.pathname;
    if (!pathname && target?.location?.href) {
      try {
        pathname = new URL(target.location.href).pathname;
      } catch {
        return null;
      }
    }
    const firstSegment = pathname?.split("/").find(Boolean);
    if (!firstSegment || NON_WORKSPACE_PATHS.has(firstSegment)) return null;
    return decodePathPart(firstSegment);
  }

  function copyHeaders(headers) {
    const copied = {};
    if (!headers) return copied;
    if (typeof headers.forEach === "function") {
      headers.forEach((value, key) => {
        copied[key] = value;
      });
    } else {
      Object.assign(copied, headers);
    }
    return copied;
  }

  function hasHeader(headers, name) {
    return Object.keys(headers).some((key) => key.toLowerCase() === name.toLowerCase());
  }

  function prepareInit(input, init, target) {
    const request = input && typeof input === "object" ? input : null;
    const method = String(init?.method || request?.method || "GET").toUpperCase();
    const headers = copyHeaders(init?.headers || request?.headers);
    const slug = workspaceSlug(target);
    if (slug && !hasHeader(headers, "X-Workspace-Slug")) {
      headers["X-Workspace-Slug"] = slug;
    }
    if (
      !["GET", "HEAD", "OPTIONS"].includes(method) &&
      !hasHeader(headers, "X-CSRF-Token")
    ) {
      const csrf = readCookie(target, "multica_csrf");
      if (csrf) headers["X-CSRF-Token"] = csrf;
    }
    return {
      ...(init || {}),
      credentials: init?.credentials || "include",
      headers,
    };
  }

  function createController(options = {}) {
    const target = options.target || root;
    const originalFetch = options.fetchImpl || target?.fetch;
    if (typeof originalFetch !== "function") {
      throw new Error("Multica Auto-Pin requires fetch");
    }

    const schedule =
      options.schedule ||
      ((callback, delay) => target.setTimeout(callback, delay));
    const debounceMs = options.debounceMs ?? 150;
    const debug = options.debug === true || target.__MULTICA_AUTO_PIN_DEBUG__ === true;
    const pending = new Set();
    let reconcileScheduled = false;
    let reconciling = false;
    let reconcileAgain = false;

    function logError(error) {
      if (debug && target.console && typeof target.console.warn === "function") {
        target.console.warn("[Multica Auto-Pin]", error);
      }
    }

    function track(task) {
      const promise = Promise.resolve(task).catch((error) => {
        logError(error);
      });
      pending.add(promise);
      promise.finally(() => pending.delete(promise));
      return promise;
    }

    async function waitForIdle() {
      while (pending.size > 0) {
        await Promise.all([...pending]);
      }
    }

    async function rawRequest(input, init) {
      return originalFetch.call(target, input, prepareInit(input, init, target));
    }

    async function readJson(input, init) {
      let response;
      try {
        response = await rawRequest(input, init);
      } catch (error) {
        logError(error);
        return { ok: false, data: null };
      }
      if (!response || response.ok !== true) {
        return { ok: false, data: null };
      }
      try {
        return { ok: true, data: await response.json() };
      } catch (error) {
        logError(error);
        return { ok: false, data: null };
      }
    }

    async function writeIssueStatus(issueId, status) {
      return rawRequest(`/api/issues/${encodeURIComponent(issueId)}`, {
        method: "PUT",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status }),
      });
    }

    async function syncManualIssueUnpin(issueId) {
      try {
        const response = await writeIssueStatus(issueId, "done");
        if (response?.ok === true) queueReconcile();
      } catch (error) {
        logError(error);
      }
    }

    async function syncCommentStatus(issueId) {
      const issueResult = await readJson(`/api/issues/${encodeURIComponent(issueId)}`, {
        credentials: "include",
      });
      if (!issueResult.ok) return;

      const status = issueResult.data?.status;
      if (typeof status !== "string" || status === "in_progress") return;

      try {
        const response = await writeIssueStatus(issueId, "in_progress");
        if (response?.ok === true) queueReconcile();
      } catch (error) {
        logError(error);
      }
    }

    async function reconcile() {
      if (reconciling) {
        reconcileAgain = true;
        return;
      }

      reconciling = true;
      try {
        const [issueResult, pinResult] = await Promise.all([
          readJson(OPEN_ISSUES_PATH),
          readJson(PINS_PATH),
        ]);
        if (!issueResult.ok || !pinResult.ok) return;

        const issues = issuesFromResponse(issueResult.data);
        const pins = pinsFromResponse(pinResult.data);
        if (!issues || !pins) return;

        const openIssueIds = new Set(
          issues.filter(isNonTerminal).map((issue) => issue.id),
        );
        const pinnedIssueIds = new Set(
          pins
            .filter(
              (pin) =>
                pin && pin.item_type === "issue" && typeof pin.item_id === "string",
            )
            .map((pin) => pin.item_id),
        );

        for (const issueId of openIssueIds) {
          if (pinnedIssueIds.has(issueId)) continue;
          try {
            const response = await rawRequest(PINS_PATH, {
              method: "POST",
              credentials: "include",
              headers: { "Content-Type": "application/json" },
              body: JSON.stringify({ item_type: "issue", item_id: issueId }),
            });
            if (response?.ok === true) pinnedIssueIds.add(issueId);
          } catch (error) {
            logError(error);
          }
        }

        // Keep the sidebar aligned when an Issue reaches done/cancelled. These
        // deletes deliberately use the original fetch, so the manual-unpin
        // handler below cannot turn an automatic cleanup into done again.
        for (const pin of pins) {
          if (
            !pin ||
            pin.item_type !== "issue" ||
            typeof pin.item_id !== "string" ||
            openIssueIds.has(pin.item_id)
          ) {
            continue;
          }
          try {
            await rawRequest(`/api/pins/issue/${encodeURIComponent(pin.item_id)}`, {
              method: "DELETE",
              credentials: "include",
            });
          } catch (error) {
            logError(error);
          }
        }
      } finally {
        reconciling = false;
        if (reconcileAgain) {
          reconcileAgain = false;
          queueReconcile();
        }
      }
    }

    function queueReconcile() {
      if (reconciling) {
        reconcileAgain = true;
        return;
      }
      if (reconcileScheduled) return;

      reconcileScheduled = true;
      schedule(() => {
        reconcileScheduled = false;
        track(reconcile());
      }, debounceMs);
    }

    function handleResponse(info, response) {
      if (!info || response?.ok !== true) return;

      const unpinnedIssueId =
        info.method === "DELETE" ? issueIdFromIssuePinDeletePath(info.path) : null;
      if (unpinnedIssueId) {
        track(syncManualIssueUnpin(unpinnedIssueId));
        return;
      }

      const commentedIssueId =
        info.method === "POST" ? issueIdFromCommentPath(info.path) : null;
      if (commentedIssueId) {
        track(syncCommentStatus(commentedIssueId));
        return;
      }

      if (info.method === "GET" && info.path === "/api/issues") {
        queueReconcile();
        return;
      }

      if (
        (info.method === "POST" &&
          (info.path === "/api/issues" ||
            info.path === "/api/issues/batch-update")) ||
        (info.method === "PUT" && issueIdFromIssueUpdatePath(info.path)) ||
        (info.method === "POST" &&
          /^\/api\/issues\/[^/]+\/move$/.test(info.path))
      ) {
        queueReconcile();
      }
    }

    function wrapFetch() {
      return async function wrappedFetch(input, init) {
        const info = requestInfo(input, init, target);
        const response = arguments.length > 1
          ? await originalFetch.call(this || target, input, init)
          : await originalFetch.call(this || target, input);
        handleResponse(info, response);
        return response;
      };
    }

    return {
      fetch: null,
      handleResponse,
      queueReconcile,
      reconcile,
      waitForIdle,
      wrapFetch,
    };
  }

  function install(target = root, options = {}) {
    if (!target || typeof target !== "object") return null;
    if (target[INSTALL_KEY]) return target[INSTALL_KEY];
    if (typeof target.fetch !== "function") return null;

    const controller = createController({
      ...options,
      fetchImpl: options.fetchImpl || target.fetch,
      target,
    });
    controller.fetch = controller.wrapFetch();
    target.fetch = controller.fetch;
    target[INSTALL_KEY] = controller;
    controller.queueReconcile();
    return controller;
  }

  const api = { createController, install, isNonTerminal };
  root.MulticaAutoPin = api;

  if (root && typeof root.fetch === "function") {
    install(root);
  }
})(typeof globalThis === "object" ? globalThis : window);
