import { devices } from '../api';

const WEB_TOKEN_KEY = 'web_device_token';

function webToken() {
  let token = localStorage.getItem(WEB_TOKEN_KEY);
  if (!token) {
    token = `web-${crypto.randomUUID()}`;
    localStorage.setItem(WEB_TOKEN_KEY, token);
  }
  return token;
}

/** Register this browser with C's device-token table when permission is granted. */
export async function registerWebDevice() {
  if (typeof Notification === 'undefined' || Notification.permission !== 'granted') return;
  await devices.register(webToken(), 'web');
}

export async function unregisterWebDevice() {
  const token = localStorage.getItem(WEB_TOKEN_KEY);
  if (!token) return;
  try {
    await devices.unregister(token);
  } catch {
    /* still drop the local session */
  }
}
