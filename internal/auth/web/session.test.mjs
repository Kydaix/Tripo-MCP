import test from 'node:test';
import assert from 'node:assert/strict';
import {parseSession} from './session.mjs';

test('accepts the renewable cookie without importing other browser cookies',()=>{
  const jwt = ['e30', 'eyJzdWIiOiJmaXh0dXJlIn0', 'test'].join('.');
  const fields = {authorization:'Bearer '+jwt,device:'fixture-device',cookie:'ory_kratos_session=fixture%2F=='};
  assert.equal(parseSession('',fields).session_cookie,'fixture%2F==');
  const copied = `curl 'https://api.tripo3d.ai/' -H 'authorization: Bearer ${jwt}' -H 'x-tripo-device-id: fixture-device' -b 'analytics=private; ory_kratos_session=fixture-session; other=ignored'`;
  const result = parseSession(copied);
  assert.equal(result.session_cookie,'fixture-session');
  assert.ok(!JSON.stringify(result).includes('private'));
  for(const cookie of ['value; other=secret','value\r\nHeader: injection','white space','a'.repeat(8193)]) {
    assert.throws(()=>parseSession('',{...fields,cookie}));
  }
});

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
