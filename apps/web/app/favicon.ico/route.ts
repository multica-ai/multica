export function GET(request: Request) {
  return Response.redirect(new URL("/brand/favicon.png", request.url), 308);
}
