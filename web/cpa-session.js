/* CPA saved-login compatibility: official WebUI secureStorage/encryption format. */
'use strict';
(() => {
 const prefix = 'enc::v1::';
 const salt = 'cli-proxy-api-webui::secure-storage';
 function resourcePrefix(win) {
  const marker = '/v0/resource/plugins/cpa-window-starter/';
  const index = win.location.pathname.indexOf(marker);
  return index >= 0 ? win.location.pathname.slice(0, index) : '';
 }
 function readValue(win, name) {
  try {
   let raw = win.localStorage.getItem(name);
   if (typeof raw !== 'string' || raw.length > 65536) return null;
   if (raw.startsWith('enc::') && !raw.startsWith(prefix)) return null;
   if (raw.startsWith(prefix)) {
    const bytes = Uint8Array.from(atob(raw.slice(prefix.length)), ch => ch.charCodeAt(0));
    const key = new TextEncoder().encode(`${salt}|${win.location.host}|${win.navigator.userAgent}`);
    for (let i = 0; i < bytes.length; i++) bytes[i] ^= key[i % key.length];
    raw = new TextDecoder().decode(bytes);
   }
   try { return JSON.parse(raw); } catch { return name === 'cli-proxy-auth' ? null : raw; }
  } catch { return null; }
 }
 function connection(win, base, key) {
  if (typeof base !== 'string' || typeof key !== 'string' || !key.trim() || key.length > 8192 || /[\r\n]/.test(key)) return null;
  try {
   const url = new URL(base);
   const path = url.pathname.replace(/\/(v0|v8)\/management\/?$/, '').replace(/\/+$/, '');
   if (url.origin !== win.location.origin || url.search || url.hash || url.username || url.password || path !== resourcePrefix(win)) return null;
   return { managementKey: key.trim() };
  } catch { return null; }
 }
 function savedConnection(win) {
  try {
   if (win.localStorage.getItem('isLoggedIn') !== 'true') return null;
   const stored = readValue(win, 'cli-proxy-auth');
   const state = stored && typeof stored === 'object' ? (stored.state || stored) : null;
   if (state && typeof state === 'object') {
    if (state.rememberPassword !== true) return null;
    return connection(win, state.apiBase, state.managementKey);
   }
   return connection(win, readValue(win, 'apiBase') || readValue(win, 'apiUrl'), readValue(win, 'managementKey'));
  } catch { return null; }
 }
 globalThis.CPAPluginSession = Object.freeze({
  savedConnection,
  resourcePrefix,
  nativeSettingsURL: win => `${win.location.origin}${resourcePrefix(win)}/management.html#/plugins`
 });
})();
