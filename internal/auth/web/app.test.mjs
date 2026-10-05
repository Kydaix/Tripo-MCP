import test from 'node:test';
import assert from 'node:assert/strict';
import {connectPage} from './app.js';

// Minimal form surface: exercise actual submit handlers without a DOM dependency.
function page(fetch, hash = '#fixture-capability-123456789') {
  const nodes = new Map();
  const names = ['transfer', 'fields', 'submit', 'status', 'request', 'authorization', 'device', 'region', 'cookie', 'manual', 'form-content', 'success', 'success-title', 'success-detail'];
  let focused;
  for (const id of names) nodes.set(id, {
    id, value: '', dataset: {}, hidden: id === 'success', disabled: false, attributes: new Map(), events: {},
    addEventListener(name, handler) { this.events[name] = handler; },
    setAttribute(name, value) { this.attributes.set(name, value); },
    removeAttribute(name) { this.attributes.delete(name); },
    focus() { focused = id; }
  });
  const get = id => nodes.get(id);
  get('transfer').reset = () => { for (const node of nodes.values()) node.value = ''; };
  const events = {};
  const win = {fetch, location: {hash}, history: {replaceState() {}}, setTimeout, clearTimeout, addEventListener(name, handler) { events[name] = handler; }};
  connectPage({getElementById: get}, win);
  const fill = () => {
    get('authorization').value = 'Bearer ' + ['e30', 'fixture', 'test'].join('.');
    get('device').value = 'fixture-device';
    get('cookie').value = 'fixture-cookie';
  };
  return {get, fill, events, win, focus: () => focused, submit: () => get('transfer').events.submit({preventDefault() {}})};
}

test('failed verification preserves fields and allows a corrected retry', async () => {
  let calls = 0;
  const p = page(async () => {
    calls++;
    return calls === 1
      ? Response.json({error: {message: 'Cookie refusé.'}}, {status: 400})
      : Response.json({authenticated: true, renewable: true});
  });
  p.fill();
  await p.submit();
  assert.equal(p.get('cookie').value, 'fixture-cookie');
  assert.equal(p.get('fields').disabled, false);
  assert.equal(p.get('status').textContent, 'Cookie refusé.');
  p.get('cookie').value = 'corrected-fixture';
  await p.submit();
  assert.equal(calls, 2);
  assert.equal(p.get('cookie').value, '');
  assert.equal(p.get('authorization').value, '');
  assert.equal(p.get('success').hidden, false);
  assert.equal(p.get('form-content').hidden, true);
  assert.equal(p.focus(), 'success');
});

test('one transfer stays in flight and success cannot be submitted again', async () => {
  let resolve, calls = 0;
  const p = page(() => { calls++; return new Promise(done => { resolve = done; }); });
  p.fill();
  const pending = p.submit();
  assert.equal(p.get('fields').disabled, true);
  assert.equal(p.get('transfer').attributes.get('aria-busy'), 'true');
  await p.submit();
  assert.equal(calls, 1);
  resolve(Response.json({authenticated: true, renewable: false, expires_at: '2026-10-06T12:00:00Z'}));
  await pending;
  assert.match(p.get('success-title').textContent, /temporaire/);
  await p.submit();
  assert.equal(calls, 1);
});

test('missing capability and invalid fields never reach the receiver', async () => {
  let calls = 0;
  const p = page(async () => { calls++; }, '');
  p.fill();
  await p.submit();
  assert.equal(p.get('fields').disabled, true);
  const valid = page(async () => { calls++; });
  await valid.submit();
  assert.equal(valid.focus(), 'request');
  assert.equal(valid.get('request').attributes.get('aria-invalid'), 'true');
  assert.equal(calls, 0);
});

test('network failure is actionable and leaving the page clears credentials', async () => {
  const p = page(async () => { throw new TypeError('Failed to fetch'); });
  p.fill();
  await p.submit();
  assert.match(p.get('status').textContent, /service local est injoignable/);
  assert.equal(p.get('cookie').value, 'fixture-cookie');
  p.events.pagehide();
  assert.equal(p.get('cookie').value, '');
  assert.equal(p.get('authorization').value, '');
  assert.equal(p.get('fields').disabled, true);
});

test('timed out verification unlocks the preserved form', async () => {
  let expire;
  const p = page((_url, options) => new Promise((_resolve, reject) => {
    options.signal.addEventListener('abort', () => reject(new DOMException('Aborted', 'AbortError')));
  }));
  p.win.setTimeout = fn => { expire = fn; };
  p.win.clearTimeout = () => {};
  p.fill();
  const pending = p.submit();
  expire();
  await pending;
  assert.equal(p.get('fields').disabled, false);
  assert.equal(p.get('cookie').value, 'fixture-cookie');
  assert.match(p.get('status').textContent, /trop de temps/);
});

test('a malformed success response cannot claim authentication', async () => {
  const p = page(async () => Response.json({}));
  p.fill();
  await p.submit();
  assert.equal(p.get('success').hidden, true);
  assert.match(p.get('status').textContent, /pas confirmé/);
});
