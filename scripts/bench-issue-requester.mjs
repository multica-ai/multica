// Synthetic predicate comparison; all fixture writes roll back.
// make env-exec ARGS="-- node scripts/bench-issue-requester.mjs ../requester-perf.json"
import { createRequire } from 'node:module';
import { writeFileSync } from 'node:fs';
const require = createRequire(process.cwd() + '/package.json');
const { Client } = require('pg');
if (!process.env.MULTICA_DEV_PROFILE) throw new Error('Run through make env-exec in an isolated development environment.');
const db = new Client({ connectionString: process.env.DATABASE_URL });
await db.connect();
const results = [];
try {
  await db.query('BEGIN');
  await db.query("SET LOCAL statement_timeout = '120s'");
  const ids = (await db.query('SELECT gen_random_uuid()::text AS me, gen_random_uuid()::text AS peer, gen_random_uuid()::text AS ws, gen_random_uuid()::text AS agent, gen_random_uuid()::text AS absent')).rows[0];
  await db.query(`INSERT INTO "user" (id,name,email) VALUES ($1::uuid,'Perf member',$1::text || '@example.test'),($2::uuid,'Perf peer',$2::text || '@example.test')`,[ids.me,ids.peer]);
  await db.query(`INSERT INTO workspace (id,name,slug,issue_prefix) VALUES ($1::uuid,'Requester benchmark',$1::text,'PERF')`,[ids.ws]);
  ids.runtime = (await db.query(`INSERT INTO agent_runtime (workspace_id,owner_id,name,runtime_mode,provider,status,device_info,metadata,last_seen_at,visibility) VALUES ($1,$2,'Perf runtime','cloud','handler_test_runtime','online','','{}',now(),'private') RETURNING id`,[ids.ws,ids.peer])).rows[0].id;
  await db.query(`INSERT INTO agent (id,workspace_id,name,owner_id,runtime_mode,runtime_id) VALUES ($1,$2,'Perf agent',$3,'cloud',$4)`,[ids.agent,ids.ws,ids.peer,ids.runtime]);
  await db.query(`CREATE TEMP TABLE requester_perf_runs AS SELECT n, gen_random_uuid() AS id FROM generate_series(1,200000) n`);
  await db.query(`INSERT INTO agent_task_queue (id,agent_id,runtime_id,status,originator_user_id,accountable_user_id,originator_source,trigger_evidence_kind)
    SELECT id,$1,$4,'completed',CASE WHEN n%100=0 THEN $2::uuid ELSE $3::uuid END,CASE WHEN n%100=0 THEN $2::uuid ELSE $3::uuid END,'direct_human',CASE WHEN n%10=0 THEN 'chat' ELSE 'comment' END FROM requester_perf_runs`,[ids.agent,ids.me,ids.peer,ids.runtime]);
  await db.query(`INSERT INTO issue (workspace_id,title,number,creator_type,creator_id,requester_user_id,origin_type,origin_id,position)
    SELECT $1,'Requester benchmark ' || n,n,CASE WHEN n%7=0 THEN 'member' ELSE 'agent' END,
      CASE WHEN n%7=0 THEN $3::uuid ELSE $2::uuid END,
      CASE WHEN n%100=0 AND n%7<>0 THEN $4::uuid ELSE NULL END,
      CASE WHEN n%7<>0 THEN 'agent_create' ELSE NULL END,
      CASE WHEN n%7<>0 THEN id ELSE NULL END,n
    FROM requester_perf_runs WHERE n<=100000`,[ids.ws,ids.agent,ids.peer,ids.me]);
  await db.query('ANALYZE issue');
  await db.query('ANALYZE agent_task_queue');
  const member = `(i.creator_type='member' AND i.creator_id=$2::uuid)`;
  const predicates = {
    before: member,
    origin_lookup: `(${member} OR (i.creator_type='agent' AND i.origin_type='agent_create' AND i.origin_id IN (SELECT id FROM agent_task_queue WHERE originator_user_id=$2::uuid AND originator_source='direct_human' AND trigger_evidence_kind='chat')))`,
    snapshot: `(${member} OR i.requester_user_id=$2::uuid)`,
  };
  const shapes = {
    rows: ['i.id','ORDER BY i.position,i.id LIMIT 50'],
    total: ['count(*)',''],
    groups: ['i.status,count(*)','GROUP BY i.status'],
    facets: ['i.priority,count(*)','GROUP BY i.priority'],
  };
  for (const who of ['me','absent']) for (const scope of ['created','any']) for (const [name,predicate] of Object.entries(predicates)) for (const [shape,[select,tail]] of Object.entries(shapes)) {
    const where = scope==='any' ? `(${predicate} OR (i.assignee_type='member' AND i.assignee_id=$2::uuid) OR (i.assignee_type='agent' AND i.assignee_id IN (SELECT id FROM agent WHERE workspace_id=$1 AND owner_id=$2::uuid)))` : predicate;
    const sql = `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) SELECT ${select} FROM issue i WHERE i.workspace_id=$1 AND ${where} ${tail}`;
    const plan = (await db.query(sql,[ids.ws,ids[who]])).rows[0]['QUERY PLAN'][0];
    results.push({who,scope,name,shape,ms:plan['Execution Time'],hit:plan.Plan['Shared Hit Blocks'],read:plan.Plan['Shared Read Blocks'],plan:plan.Plan});
  }
  writeFileSync(process.argv[2] ?? 'requester-perf.json', JSON.stringify({ issues:100000,runs:200000,results },null,2));
  for (const x of results) console.log(`${x.who} ${x.scope} ${x.name} ${x.shape}: ${x.ms}ms; buffers=${x.hit+x.read}`);
} finally {
  await db.query('ROLLBACK');
  await db.query('ANALYZE issue');
  await db.query('ANALYZE agent_task_queue');
  await db.end();
}
