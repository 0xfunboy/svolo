import test from 'node:test';import assert from 'node:assert/strict';import{CorePool}from'./cores.mjs';
test('configuration lock serializes refresh with chat deletion and continues after errors',async()=>{
 const pool=Object.create(CorePool.prototype),core={},order=[];let started;const ready=new Promise(done=>started=done);let release;const blocked=new Promise(done=>release=done);
 const refresh=pool.withConfigLock(core,async()=>{order.push('refresh-read');started();await blocked;order.push('refresh-write')});
 const deletion=pool.withConfigLock(core,async()=>{order.push('delete-chat')});
 await ready;assert.deepEqual(order,['refresh-read']);release();await Promise.all([refresh,deletion]);assert.deepEqual(order,['refresh-read','refresh-write','delete-chat']);assert.equal(core.configTail,undefined);
 await assert.rejects(pool.withConfigLock(core,async()=>{throw Error('fixture I/O failure')}),/fixture I\/O/);
 assert.equal(await pool.withConfigLock(core,async()=> 'recovered'),'recovered');assert.equal(core.configTail,undefined);
});
