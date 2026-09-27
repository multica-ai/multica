# Weixin host. Built from apps/weixin-host on its own: the runtime needs only
# the pinned plugin and the local openclaw-sdk shim, installed with npm.
FROM node:22-alpine

WORKDIR /app

COPY package.json ./package.workspace.json
COPY openclaw-sdk ./openclaw-sdk
# Keep only the runtime dependencies (the dev ones use pnpm's catalog: which
# npm cannot read). The shim is the plugin's `openclaw` peer by design, so
# its placeholder version never satisfies the peer range.
RUN node -e ' \
    const p = require("./package.workspace.json"); \
    const deps = Object.fromEntries(Object.entries(p.dependencies) \
      .map(([k, v]) => [k, v.replace(/^link:/, "file:")])); \
    require("fs").writeFileSync("package.json", JSON.stringify( \
      { name: p.name, private: true, type: "module", dependencies: deps }, null, 2)); \
  ' \
  && npm install --omit=dev --legacy-peer-deps --no-audit --no-fund \
  && rm package.workspace.json

COPY src ./src

ENV WEIXIN_HOST_LISTEN=0.0.0.0:8790 \
    WEIXIN_HOST_STATE_DIR=/data
# Owned by `node` before the volume mounts so a fresh named volume inherits it.
RUN mkdir -p /data && chown node:node /data
VOLUME /data
EXPOSE 8790

USER node
CMD ["node", "src/main.ts"]
