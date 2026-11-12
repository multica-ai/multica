import assert from "node:assert/strict";
import test from "node:test";
import plugin from "./server.js";

async function harness({ terminal = "completed", abort = false, resume = false, stopUnconfirmed = false } = {}) {
  let cli, resolveEnv, metadata = { multica: true, runKey: "old" }, sent, stopped = 0, waited = 0;
  const controller = new AbortController();
  const sdk = { threads: {
    getPluginMetadata: async () => metadata,
    spawn: async args => {
      sent = args;
      metadata = args.pluginMetadata;
      const env = await resolveEnv({ threadId: "thr_test", hostId: "host_test" });
      assert.equal(env.find(entry => entry.name === "MULTICA_TOKEN")?.value, "mat_test_secret");
      assert.ok(!JSON.stringify(args).includes("mat_test_secret"));
      return { id: "thr_test" };
    },
    updatePluginMetadata: async ({ set }) => { metadata = { ...metadata, ...set }; },
    send: async () => ({ delivery: "queued" }),
    events: {
      list: async () => [{ seq: 17, data: { status: "completed" } }],
      wait: async args => {
        if (abort) controller.abort();
        if (resume) assert.equal(args.afterSeq, "17");
        waited++;
        if (resume && waited === 1) return null;
        return { seq: 18, data: { status: terminal } };
      },
    },
    output: async () => ({ output: "computed result" }),
    stop: async () => { stopped++; },
    wait: async () => { if (stopUnconfirmed) throw new Error("Thread remains stopping"); },
  } };
  await plugin({
    settings: { define: () => ({ get: async () => ({ projectId: "project_test", hostId: "host_test" }) }) },
    providers: { experimental_contributeEnv: (_provider, resolver) => { resolveEnv = resolver; } },
    cli: { register: registration => { cli = registration; } }, sdk,
  });
  const response = await cli.run(["run", "--request", JSON.stringify({
    prompt: "Read the task and compute the result", cwd: "/workspace",
    env: { MULTICA_TOKEN: "mat_test_secret" },
    ...(resume ? { sessionId: "thr_test" } : {}),
  })], { signal: controller.signal });
  return { response, sent, stopped, waited,
    remaining: await resolveEnv({ threadId: "thr_test", hostId: "host_test" }) };
}

test("injects credentials before spawn and removes them after stopping the provider", async () => {
  const result = await harness();
  assert.equal(result.response.exitCode, 0);
  assert.deepEqual(JSON.parse(result.response.stdout), { session_id: "thr_test", output: "computed result" });
  assert.equal(result.stopped, 1);
  assert.deepEqual(result.remaining, []);
});

test("a queued resumed turn waits beyond the previous turn's terminal event", async () => {
  const result = await harness({ resume: true });
  assert.equal(result.response.exitCode, 0);
  assert.equal(result.waited, 2);
  assert.deepEqual(result.remaining, []);
});

test("does not report partial output as success after a failed BB turn", async () => {
  const result = await harness({ terminal: "failed" });
  assert.equal(result.response.exitCode, 1);
  assert.equal(result.response.stdout, undefined);
  assert.deepEqual(result.remaining, []);
  assert.ok(!result.response.stderr.includes("mat_test_secret"));
});

test("cancellation stops the BB thread and clears credentials", async () => {
  const result = await harness({ abort: true });
  assert.equal(result.response.exitCode, 1);
  assert.equal(result.stopped, 1);
  assert.deepEqual(result.remaining, []);
});

test("an unconfirmed BB stop retains ownership and blocks automatic retry", async () => {
  const result = await harness({ stopUnconfirmed: true });
  assert.equal(result.response.exitCode, 1);
  assert.equal(JSON.parse(result.response.stdout).status, "cleanup_unconfirmed");
  assert.equal(result.remaining[0].name, "MULTICA_TOKEN");
  assert.ok(!result.response.stdout.includes("mat_test_secret"));
});
