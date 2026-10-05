import {parseSession, SessionInputError} from './session.mjs';

export function connectPage(doc, win) {
  let capability = win.location.hash.slice(1);
  win.history.replaceState(null, '', '/');
  const byID = id => doc.getElementById(id);
  const form = byID('transfer');
  const fields = byID('fields');
  const button = byID('submit');
  const status = byID('status');
  const inputs = ['request', 'authorization', 'device', 'region', 'cookie'].map(byID);
  let pending = false;
  let completed = false;
  let controller;
  function message(text, state = 'error') {
    status.textContent = text;
    status.dataset.state = state;
  }
  const expired = 'Ce lien est incomplet ou expiré. Relance tripo-mcp login et ouvre le nouveau lien affiché.';
  if (!/^[A-Za-z0-9_-]{20,128}$/.test(capability)) {
    capability = '';
    message(expired);
  }
  fields.disabled = !capability;
  form.addEventListener('input', event => event.target.removeAttribute('aria-invalid'));
  form.addEventListener('submit', async event => {
    event.preventDefault();
    if (!capability || pending || completed) return;
    inputs.forEach(input => input.removeAttribute('aria-invalid'));
    let input;
    try {
      input = parseSession(byID('request').value, {
        authorization: byID('authorization').value,
        device: byID('device').value,
        region: byID('region').value,
        cookie: byID('cookie').value
      });
    } catch (error) {
      message(error instanceof SessionInputError ? error.message : 'Copie la requête Studio ou renseigne les en-têtes manuellement.');
      const field = byID(error.field || 'request');
      if (['authorization', 'device', 'region'].includes(field.id)) byID('manual').open = true;
      field.setAttribute('aria-invalid', 'true');
      field.focus();
      return;
    }
    pending = true;
    fields.disabled = true;
    form.setAttribute('aria-busy', 'true');
    button.textContent = 'Vérification en cours…';
    message(input.session_cookie ? 'Vérification du renouvellement auprès de Studio…' : 'Vérification du jeton temporaire auprès de Studio…', 'pending');
    controller = new AbortController();
    const timeout = win.setTimeout(() => controller.abort(), 65000);
    try {
      const response = await win.fetch('/session', {
        method: 'POST', cache: 'no-store', signal: controller.signal,
        headers: {'Content-Type': 'application/json', 'X-Tripo-MCP-Transfer': capability},
        body: JSON.stringify(input)
      });
      if (!response.headers.get('content-type')?.includes('application/json')) {
        if ([403, 409].includes(response.status)) {
          capability = '';
          form.reset();
        }
        throw new Error(expired);
      }
      const result = await response.json();
      if (!response.ok) throw new Error(result.error?.message || 'La connexion a été refusée. Vérifie les données copiées puis réessaie.');
      if (result.authenticated !== true) throw new Error('Le service local n’a pas confirmé la connexion. Relance tripo-mcp login.');
      completed = true;
      capability = '';
      form.reset();
      byID('form-content').hidden = true;
      byID('success').hidden = false;
      const date = value => {
        const parsed = new Date(value);
        return Number.isFinite(parsed.getTime()) ? parsed.toLocaleString('fr-FR') : '';
      };
      const until = date(result.renewable ? result.session_expires_at : result.expires_at);
      byID('success-title').textContent = result.renewable ? 'Connexion établie' : 'Connexion temporaire établie';
      byID('success-detail').textContent = result.renewable
        ? 'Renouvellement automatique activé.' + (until ? ' Session valable jusqu’au ' + until + ', sauf révocation par Tripo.' : ' Il reste actif tant que la session Studio est valide.')
        : (until ? 'Le jeton expire le ' + until + '. ' : '') + 'Pour une connexion renouvelable, relance tripo-mcp login et ajoute le cookie de connexion.';
      message('', 'success');
      byID('success').focus();
    } catch (error) {
      const text = controller.signal.aborted
        ? 'La vérification a pris trop de temps. Vérifie la connexion puis réessaie ; aucune donnée saisie n’a été effacée.'
        : error instanceof TypeError
          ? 'Le service local est injoignable. Vérifie que tripo-mcp login est toujours ouvert, puis réessaie.'
          : error.message || 'Transfert interrompu. Réessaie ou relance tripo-mcp login.';
      message(text);
      status.focus();
    } finally {
      win.clearTimeout(timeout);
      pending = false;
      fields.disabled = completed || !capability;
      form.removeAttribute('aria-busy');
      button.textContent = 'Vérifier et connecter';
      input = null;
    }
  });
  win.addEventListener('pagehide', () => {
    capability = '';
    controller?.abort();
    form.reset();
    fields.disabled = true;
  });
  win.addEventListener('pageshow', event => {
    if (event.persisted && !completed) message(expired);
  });
}

if (typeof document !== 'undefined') connectPage(document, window);
