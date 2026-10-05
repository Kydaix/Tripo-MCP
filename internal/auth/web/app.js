import {parseSession} from './session.mjs';

const capability = location.hash.slice(1);
history.replaceState(null, '', '/');
const form = document.querySelector('#transfer');
const button = document.querySelector('#submit');
const status = document.querySelector('#status');
function message(text, error = false) { status.textContent = text; status.dataset.error = String(error); }
if (!capability) { button.disabled = true; message('Lien incomplet. Relance tripo-mcp login et ouvre le lien affiché.', true); }
form.addEventListener('submit', async event => {
  event.preventDefault();
  button.disabled = true;
  try {
    const input = parseSession(document.querySelector('#request').value, {
      authorization: document.querySelector('#authorization').value,
      device: document.querySelector('#device').value,
      region: document.querySelector('#region').value,
      cookie: document.querySelector('#cookie').value
    });
    form.reset();
    message(input.session_cookie ? 'Vérification du renouvellement auprès de Studio…' : 'Vérification du jeton temporaire auprès de Studio…');
    const response = await fetch('/session', {
      method: 'POST', cache: 'no-store',
      headers: {'Content-Type': 'application/json', 'X-Tripo-MCP-Transfer': capability},
      body: JSON.stringify(input)
    });
    if (!response.headers.get('content-type')?.includes('application/json')) throw new Error('Transfert refusé ou expiré. Relance tripo-mcp login.');
    const result = await response.json();
    if (!response.ok) throw new Error(result.error?.message || 'Session refusée.');
    if (result.renewable) {
      message(result.session_expires_at
        ? 'Renouvellement automatique activé. Connexion Studio valable jusqu’au ' + new Date(result.session_expires_at).toLocaleString() + ', sauf révocation. Tu peux fermer le navigateur.'
        : 'Renouvellement automatique activé tant que la session Studio reste valide. Tu peux fermer le navigateur.');
    } else {
      message('Jeton temporaire enregistré jusqu’au ' + new Date(result.expires_at).toLocaleString() + '. Pour activer le renouvellement, relance tripo-mcp login et ajoute le cookie de session.');
    }
    button.textContent = 'Connecté';
  } catch (error) { message(error.message || 'Service local arrêté. Relance tripo-mcp login.', true); button.disabled = false; }
});
