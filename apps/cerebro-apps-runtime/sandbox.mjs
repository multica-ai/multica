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
  modules.set("backend/index.mjs", source
    .replace(/\bexport\s+default\s+async\s+/, "export default ")
    .replace(/\bawait\s+/g, ""));

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
        registry: Object.freeze({ call: (...args) => JSON.parse(__hostRegistry(JSON.stringify(args))) }),
        connections: Object.freeze({ call: (...args) => JSON.parse(__hostConnection(JSON.stringify(args))) }),
        log: (...args) => __hostLog(JSON.stringify(args)),
      });`;
    const bootstrapped = await vm.evalCodeAsync(bootstrap, "bootstrap.js", { type: "global" });
    vm.unwrapResult(bootstrapped).dispose();
    const runner = `import __handler from "./backend/index.mjs";
      globalThis.__resultJSON = JSON.stringify(__handler(JSON.parse(__inputJSON), __multica));`;
    const evaluated = await vm.evalCodeAsync(runner, "__runner.mjs", { type: "module" });
    vm.unwrapResult(evaluated).dispose();
    const resultHandle = vm.getProp(vm.global, "__resultJSON");
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
  const handle = vm.newAsyncifiedFunction(name, async (argsHandle) => {
    const args = JSON.parse(vm.getString(argsHandle));
    const result = await implementation(args);
    return vm.newString(JSON.stringify(result ?? null));
  });
  vm.setProp(vm.global, name, handle);
  handle.dispose();
}
