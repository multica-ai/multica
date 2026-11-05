const test=require('node:test');
const assert=require('node:assert/strict');
const privatePath=require('./snapshot-policy.cjs')(require('../../server/pkg/codegraph/snapshot-policy.json'));
test('snapshot policy keeps configuration directories without admitting private files',()=>{
  for(const p of ['.agents/agent.yaml','.github/workflows/build.yml','docs/readme.md']) assert.equal(privatePath(p,false),false,p);
  for(const p of ['.agents','.env','.git/config','../outside','a/../outside','node_modules/test.md','foo.pem','id_rsa','.credentials/file.yaml','a\\b.yaml']) assert.equal(privatePath(p,false),true,p);
});
