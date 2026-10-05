export class SessionInputError extends Error {
  constructor(message, field) {
    super(message);
    this.field = field;
  }
}

// Parse copied headers as text. Never evaluate a fetch/cURL command.
export function parseSession(text, fields = {}) {
  if (text.length > 65536) throw new SessionInputError('Copie uniquement la requête team/list (64 Ko maximum).', 'request');
  const authorization = fields.authorization?.trim() || text.match(/\bBearer\s+[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+/i)?.[0] || '';
  const token = authorization.match(/^Bearer\s+([A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)$/i)?.[1];
  if (!token || token.length + 7 > 16384) {
    throw new SessionInputError('Jeton introuvable ou invalide. Recopie la requête team/list, ou renseigne Authorization manuellement.', fields.authorization ? 'authorization' : 'request');
  }
  function header(name) {
    return text.match(new RegExp(name + `["']?\\s*:\\s*["']?([^\\s"'\\\\^,;}]+)`, 'i'))?.[1] || '';
  }
  const device = fields.device?.trim() || header('x-tripo-device-id');
  const region = fields.region?.trim() || header('x-tripo-region');
  if (!device || device.length > 256 || /[^\x21-\x7e]/.test(device)) {
    throw new SessionInputError('Identifiant d’appareil absent ou invalide. Recopie la requête, ou renseigne x-tripo-device-id manuellement.', fields.device ? 'device' : 'request');
  }
  if (region.length > 32 || /[^\x21-\x7e]/.test(region)) {
    throw new SessionInputError('La région doit être un en-tête court sans espace. Corrige x-tripo-region ou laisse-le vide.', fields.region ? 'region' : 'request');
  }
  const input = {authorization: 'Bearer ' + token, device_id: device, region};
  const cookie = fields.cookie?.trim() || text.match(/\bory_kratos_session=([^;\s"'\\^]+)/)?.[1] || '';
  if (cookie) {
    const value = cookie.replace(/^ory_kratos_session=/, '');
    if (!value || value.length > 8192 || /[^\x21-\x7e]|[";,\\]/.test(value)) {
      throw new SessionInputError('Copie uniquement la valeur du cookie ory_kratos_session, sans les autres cookies.', 'cookie');
    }
    input.session_cookie = value;
  }
  return input;
}
