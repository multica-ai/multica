FROM node:22-alpine
WORKDIR /app
COPY apps/cerebro-apps-runtime/ ./
ENV NODE_ENV=production PORT=4310 CEREBRO_APPS_RUNTIME_PROVIDER=sliplane
EXPOSE 4310
CMD ["node", "server.mjs"]
