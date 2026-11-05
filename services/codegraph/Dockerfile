FROM node:22.23.1-bookworm-slim
ENV DO_NOT_TRACK=1 CODEGRAPH_TELEMETRY=0 CODEGRAPH_KERNEL=0 NODE_ENV=production
WORKDIR /opt/codegraph
COPY services/codegraph/source-revision.txt /opt/multica/source-revision.txt
RUN node -e 'const fs=require("node:fs");const revision=fs.readFileSync("/opt/multica/source-revision.txt","utf8").trim();if(!/^[0-9a-f]{40}$/.test(revision))throw Error("Invalid source revision");fetch("https://codeload.github.com/colbymchenry/codegraph/tar.gz/"+revision).then(async r=>{if(!r.ok)throw Error("Source download HTTP "+r.status);fs.writeFileSync("/tmp/codegraph.tar.gz",Buffer.from(await r.arrayBuffer()));}).catch(e=>{console.error(e.message);process.exitCode=1;});' \
    && tar -xzf /tmp/codegraph.tar.gz --strip-components=1 && rm /tmp/codegraph.tar.gz \
    && npm ci --include=dev --ignore-scripts \
    && npx tsc && npm run copy-assets && npm prune --omit=dev --ignore-scripts
COPY services/codegraph/engine.cjs services/codegraph/server.cjs services/codegraph/copy-snapshot.cjs /opt/multica/
COPY services/codegraph/snapshot-policy.cjs server/pkg/codegraph/snapshot-policy.json /opt/multica/
ENV MULTICA_CODEGRAPH_ROOT=/data/codegraphs MULTICA_CODEGRAPH_WORK_ROOT=/tmp/multica-codegraph-builds
EXPOSE 8787
CMD ["node", "/opt/multica/server.cjs"]
