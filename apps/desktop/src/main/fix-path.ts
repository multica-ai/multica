type FixPath = () => void;

export function applyFixPath(module: FixPath | { default: FixPath }): void {
  const fixPath = typeof module === "function" ? module : module.default;
  fixPath();
}
