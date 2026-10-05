import test from 'node:test';
import assert from 'node:assert/strict';
import {parseSession} from './session.mjs';

test('normalizes bearer casing and validates complete header lengths', () => {
  const jwt = ['e30', 'fixture', 'test'].join('.');
  const fields = {authorization: 'bearer\t' + jwt, device: 'fixture'};
  assert.equal(parseSession('', fields).authorization, 'Bearer ' + jwt);
  for (const device of ['a'.repeat(257), 'bad\x00value', 'bad value']) {
    assert.throws(() => parseSession('', {...fields, device}), {field: 'device'});
  }
  assert.throws(() => parseSession('', {...fields, region: 'x'.repeat(33)}), {field: 'region'});
  assert.throws(() => parseSession('x'.repeat(65537), fields), {field: 'request'});
  const copy = `fetch('https://api.tripo3d.ai/', {headers: {'authorization': 'Bearer ${jwt}', 'x-tripo-device-id': '${'x'.repeat(257)}'}})`;
  assert.throws(() => parseSession(copy), {field: 'request'});
});

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
