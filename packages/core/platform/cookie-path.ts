// 仅服务端可写：由 apps/web/app/layout.tsx 模块初始化时调用一次，
// 确保 server action / middleware 写 Set-Cookie 时 Path 属性与部署前缀一致。
let cookieBasePath = "/";

// 仅在服务端调用（Next.js server 模块初始化阶段）。
// 浏览器端与服务端是独立的运行时，该变量不共享，无需在客户端调用。
export function setCookieBasePath(path: string) {
  cookieBasePath = path || "/";
}

// 服务端：返回由 setCookieBasePath 设置的模块变量。
// 客户端：server 模块状态不可访问，直接读取构建时内联的 NEXT_PUBLIC_BASE_PATH 常量，
// 两者来源相同（同一 env 变量），行为一致，属于故意设计而非不一致 bug。
export function getCookieBasePath(): string {
  if (typeof window !== "undefined") {
    return (process.env.NEXT_PUBLIC_BASE_PATH as string) || "/";
  }
  return cookieBasePath;
}
