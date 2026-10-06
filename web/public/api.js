import { t, locale } from './i18n.js?v=20261006-10';
// Same-origin, cookie-based access. The core credential never reaches this client.
let csrf = '';
export const setCsrf = value => { csrf = typeof value === 'string' ? value : ''; };
export class ApiError extends Error {
  constructor(message, status) { super(message); this.name = 'ApiError'; this.status = status; }
}
export async function api(path, { method = 'GET', body, timeout = 20000, signal } = {}) {
  if (typeof path !== 'string' || !path.startsWith('/api/') || path.startsWith('//')) throw new Error(t("Percorso API non valido."));
  const headers = { Accept: 'application/json' };
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (!['GET', 'HEAD'].includes(method)) headers['X-CSRF-Token'] = csrf;
  let response;
  try {
    response = await fetch(path, { method, headers, credentials: 'same-origin', redirect: 'error', cache: 'no-store', body: body === undefined ? undefined : JSON.stringify(body), signal: signal ? AbortSignal.any([signal, AbortSignal.timeout(timeout)]) : AbortSignal.timeout(timeout) });
  } catch (error) {
    if (error.name === 'TimeoutError') throw new ApiError(t("Il servizio non ha risposto in tempo. Riprova."), 0);
    if (error.name === 'AbortError') throw error;
    throw new ApiError(t("Connessione al servizio non disponibile."), 0);
  }
  if (response.status === 204) return null;
  const raw = await response.text();
  let result;
  try { result = raw ? JSON.parse(raw) : null; } catch { throw new ApiError(t("Il servizio ha restituito una risposta non valida."), response.status); }
  if (!response.ok) throw new ApiError(t(result?.error || result?.message) || `${t("Richiesta non riuscita (")}${response.status}).`, response.status);
  return result;
}
export const core = (path, options) => api('/api/core' + path, options);
export const post = (path, body, options = {}) => api(path, { ...options, method: 'POST', body });
export const escapeHtml = value => String(value ?? '').replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[char]));
export const array = value => Array.isArray(value) ? value : [];
export function quotaText(quota) {
  if (quota?.remaining !== null && quota?.remaining !== undefined && Number.isFinite(Number(quota.remaining))) {
    return `${new Intl.NumberFormat(locale(), { maximumFractionDigits: 4 }).format(Number(quota.remaining))} ${quota.unit || t("unità")} ${t("disponibili")}`;
  }
  const headers = quota?.headers;
  if(headers) {
    const remaining=headers['x-ratelimit-remaining-tokens'] ?? headers['x-ratelimit-remaining-requests'] ?? headers['ratelimit-remaining'];
    if(remaining!==undefined && Number.isFinite(Number(remaining)))return `${new Intl.NumberFormat(locale()).format(Number(remaining))} ${headers['x-ratelimit-remaining-tokens']!==undefined?'token':headers['x-ratelimit-remaining-requests']!==undefined?t("richieste"):t("unità")} ${t("disponibili")}`;
  }
  const windows = Object.values(quota?.buckets || {}).flatMap(bucket=>[bucket?.primary,bucket?.secondary].filter(Boolean));
  if(windows.length) {
    return windows.filter(window=>window.usedPercent!==null && Number.isFinite(Number(window.usedPercent))).map(window=>`${new Intl.NumberFormat(locale(),{maximumFractionDigits:1}).format(Math.max(0,100-Number(window.usedPercent)))}${t("% disponibile")}${window.windowDurationMins?` · ${window.windowDurationMins>=60?window.windowDurationMins/60+'h':window.windowDurationMins+'min'}`:''}`).join(' / ') || t("Dettagli quota disponibili");
  }
  if(quota?.data?.remaining!==null && quota?.data?.remaining!==undefined && Number.isFinite(Number(quota.data.remaining)))return `${new Intl.NumberFormat(locale()).format(Number(quota.data.remaining))} ${t("unità disponibili")}`;
  if(quota?.status==='unavailable')return quota?.reason===t("Provider non collegato")?t("Provider non collegato"):t("Quota non disponibile dal provider");
  return t(quota?.message) || (quota?.status==='available'?t("Dettagli quota disponibili"):t("Quota non disponibile dal provider"));
}
