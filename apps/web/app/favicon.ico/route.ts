import { resolveBasePath, withBasePath } from "@/config/base-path";

export function GET(request: Request) {
  const basePath = resolveBasePath({
    NEXT_PUBLIC_BASE_PATH: process.env.NEXT_PUBLIC_BASE_PATH,
    BASE_PATH: process.env.BASE_PATH,
  });
  return Response.redirect(
    new URL(withBasePath(basePath, "/favicon.svg"), request.url),
    308,
  );
}
