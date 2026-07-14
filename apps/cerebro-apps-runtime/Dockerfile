FROM node:22-alpine
WORKDIR /app
COPY apps/cerebro-apps-runtime/ ./
ENV NODE_ENV=production PORT=4310 APP_BUNDLE_ROOT=/var/lib/multica-apps
USER node
EXPOSE 4310
CMD ["node", "server.mjs"]
