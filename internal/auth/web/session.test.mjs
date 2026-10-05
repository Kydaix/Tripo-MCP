import test from 'node:test';
import assert from 'node:assert/strict';
import {parseSession} from './session.mjs';

test('extracts copied fetch and cURL without executing pasted code',()=>{
  for(const text of [
    `fetch("https://api.tripo3d.ai/v2/studio/team/list",{headers:{"authorization":"Bearer e30.fixture.signature","x-tripo-device-id":"device-1"}})`,
    `curl 'https://api.tripo3d.ai/v2/studio/team/list' -H 'authorization: Bearer e30.fixture.signature' -H 'x-tripo-device-id: device-1'`,
    `curl.exe ^"https://api.tripo3d.ai/^" -H ^"authorization: Bearer e30.fixture.signature^" -H ^"x-tripo-device-id: device-1^"`
  ]) assert.deepEqual(parseSession(text),{authorization:'Bearer e30.fixture.signature',device_id:'device-1',region:''});
  globalThis.executed=false;
  assert.throws(()=>parseSession('globalThis.executed=true'));
  assert.equal(globalThis.executed,false);
});
test('requires both headers and supports manual fields',()=>{
  assert.throws(()=>parseSession('authorization: Bearer e30.fixture.signature'));
  assert.deepEqual(parseSession('',{authorization:'Bearer e30.fixture.signature',device:'device-2',region:'ov'}),{authorization:'Bearer e30.fixture.signature',device_id:'device-2',region:'ov'});
  assert.throws(()=>parseSession('',{authorization:'Bearer e30.fixture.signature',device:'bad\r\nheader'}));
});
