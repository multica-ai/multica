FROM node:22-alpine
RUN apk add --no-cache docker-cli su-exec
WORKDIR /app
COPY apps/cerebro-apps-runtime/ ./
RUN chmod +x /app/docker-entrypoint.sh
ENV NODE_ENV=production PORT=4310 APP_BUNDLE_ROOT=/var/lib/multica-apps
EXPOSE 4310
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["node", "server.mjs"]
