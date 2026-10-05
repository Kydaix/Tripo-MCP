// Parse known copied-header forms as text. Never eval a copied fetch/cURL command.
export function parseSession(text, fields = {}) {
  const bearer = fields.authorization?.trim() || text.match(/\bBearer\s+([A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)/i)?.[0] || "";
  function header(name) {
    return text.match(new RegExp(name + `["']?\\s*:\\s*["']?([a-zA-Z0-9_.-]{1,256})`, "i"))?.[1] || "";
  }
  const device = fields.device?.trim() || header("x-tripo-device-id");
  const region = fields.region?.trim() || header("x-tripo-region");
  if (!/^Bearer [A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/i.test(bearer) || !device) {
    throw new Error("La copie doit contenir Authorization: Bearer … et x-tripo-device-id. Sinon, utilise les champs manuels.");
  }
  if (/[\r\n]/.test(device + region)) throw new Error("En-tête invalide.");
  const input = {authorization: "Bearer " + bearer.slice(7), device_id: device, region};
  const cookie = fields.cookie?.trim() || text.match(/\bory_kratos_session=([^;\s"'\\^]+)/)?.[1] || '';
  if (cookie) {
    const value = cookie.replace(/^ory_kratos_session=/, '');
    if (!value || value.length > 8192 || /[^\x21-\x7e]|[";,\\]/.test(value)) throw new Error('Copie uniquement la valeur du cookie ory_kratos_session.');
    input.session_cookie = value;
  }
  return input;
}
