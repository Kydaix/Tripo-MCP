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
  return {authorization: "Bearer " + bearer.slice(7), device_id: device, region};
}
