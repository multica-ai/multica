import { RELEASE_ASYNC, newQuickJSAsyncWASMModule } from "quickjs-emscripten";
import { posix } from "node:path";

const DEFAULT_MEMORY_BYTES = 64 << 20;
const MAX_INPUT_BYTES = 512 << 10;
const MAX_OUTPUT_BYTES = 1 << 20;

export async function executeSandbox(options) {
  const inputJSON = JSON.stringify(options.input ?? null);
  if (Buffer.byteLength(inputJSON) > (options.maxInputBytes ?? MAX_INPUT_BYTES)) throw new Error("App worker failed");
  const source = String(options.source ?? "");
  if (!/\bexport\s+default\b/.test(source) || /\b(?:process|require|fetch|WebSocket)\s*=/.test(source)) throw new Error("App worker failed");
  const modules = new Map(Object.entries(options.modules ?? {}).map(([name, value]) => [posix.normalize(name), String(value)]));
  modules.set("backend/index.mjs", source);

  const quickJS = await newQuickJSAsyncWASMModule(options.variant ?? RELEASE_ASYNC);
  const runtime = quickJS.newRuntime();
  runtime.setMemoryLimit(options.memoryBytes ?? DEFAULT_MEMORY_BYTES);
  runtime.setMaxStackSize(1 << 20);
  const deadline = Date.now() + (options.deadlineMs ?? 5_000);
  runtime.setInterruptHandler(() => Date.now() > deadline);
  runtime.setModuleLoader(
    (moduleName) => modules.get(moduleName) ?? new Error("Module import is not allowed"),
    (baseModuleName, requestedName) => normalizeModuleName(baseModuleName, requestedName, modules),
  );
  const vm = runtime.newContext();

  try {
    const inputHandle = vm.newString(inputJSON);
    vm.setProp(vm.global, "__inputJSON", inputHandle);
    inputHandle.dispose();

    installHostFunction(vm, "__hostRegistry", async (args) => {
      if (typeof options.host?.registryCall !== "function") throw new Error("Registry access is unavailable");
      return options.host.registryCall(...args);
    });
    installHostFunction(vm, "__hostConnection", async (args) => {
      if (typeof options.host?.connectionCall !== "function") throw new Error("Connection access is unavailable");
      return options.host.connectionCall(...args);
    });
    installHostFunction(vm, "__hostLog", async (args) => {
      options.host?.log?.(...args);
      return null;
    });

    const bootstrap = `
      globalThis.__multica = Object.freeze({
        registry: Object.freeze({ call: async (...args) => JSON.parse(await __hostRegistry(JSON.stringify(args))) }),
        connections: Object.freeze({ call: async (...args) => JSON.parse(await __hostConnection(JSON.stringify(args))) }),
        log: (...args) => __hostLog(JSON.stringify(args)),
      });`;
    const bootstrapped = await vm.evalCodeAsync(bootstrap, "bootstrap.js", { type: "global" });
    vm.unwrapResult(bootstrapped).dispose();
    const runner = `import __handler from "./backend/index.mjs";
      globalThis.__resultPromise = Promise.resolve(__handler(JSON.parse(__inputJSON), __multica))
        .then((value) => JSON.stringify(value));`;
    const evaluated = await vm.evalCodeAsync(runner, "__runner.mjs", { type: "module" });
    vm.unwrapResult(evaluated).dispose();
    const promiseHandle = vm.getProp(vm.global, "__resultPromise");
    const pendingResult = vm.resolvePromise(promiseHandle);
    runtime.executePendingJobs();
    const resolved = await pendingResult;
    promiseHandle.dispose();
    const resultHandle = vm.unwrapResult(resolved);
    const outputJSON = vm.getString(resultHandle);
    resultHandle.dispose();
    if (Buffer.byteLength(outputJSON) > (options.maxOutputBytes ?? MAX_OUTPUT_BYTES)) throw new Error("output too large");
    return JSON.parse(outputJSON);
  } catch {
    console.error("mini-app sandbox execution failed");
    throw new Error("App worker failed");
  } finally {
    vm.dispose();
    runtime.dispose();
  }
}

function normalizeModuleName(baseModuleName, requestedName, modules) {
  if (!requestedName.startsWith("./")) return new Error("Module import is not allowed");
  const normalized = posix.normalize(posix.join(posix.dirname(baseModuleName), requestedName));
  if (!normalized.startsWith("backend/") || normalized.includes("..") || !modules.has(normalized)) return new Error("Module import is not allowed");
  return normalized;
}

function installHostFunction(vm, name, implementation) {
  const handle = vm.newFunction(name, (argsHandle) => {
    const args = JSON.parse(vm.getString(argsHandle));
    const deferred = vm.newPromise();
    Promise.resolve(implementation(args)).then(
      (result) => {
        const resultHandle = vm.newString(JSON.stringify(result ?? null));
        deferred.resolve(resultHandle);
        resultHandle.dispose();
      },
      (error) => {
        const errorHandle = vm.newError(error instanceof Error ? error.message : "Host call failed");
        deferred.reject(errorHandle);
        errorHandle.dispose();
      },
    );
    deferred.settled.then(() => vm.runtime.executePendingJobs());
    return deferred.handle;
  });
  vm.setProp(vm.global, name, handle);
  handle.dispose();
}
