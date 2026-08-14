import { getAccessToken } from './client';

export function wsURL() {
  const token = getAccessToken();
  const explicit = import.meta.env.VITE_WS_URL as string | undefined;
  if (explicit) {
    const u = new URL(explicit, window.location.origin);
    if (u.protocol === 'http:') u.protocol = 'ws:';
    if (u.protocol === 'https:') u.protocol = 'wss:';
    u.searchParams.set('access_token', token || '');
    return u.toString();
  }
  const apiBase = import.meta.env.VITE_API_URL as string | undefined;
  if (apiBase) {
    const u = new URL('/ws', apiBase);
    u.protocol = u.protocol === 'https:' ? 'wss:' : 'ws:';
    u.searchParams.set('access_token', token || '');
    return u.toString();
  }
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}/ws?access_token=${encodeURIComponent(token || '')}`;
}

type Frame = {
  type: string;
  conversation_id?: string;
  user_id?: string;
  typing?: boolean;
  last_read_at?: string;
  message?: unknown;
  content?: string;
  client_msg_id?: string;
};

type MessageHandler = (msg: unknown) => void;

class SessionSocket {
  ws: WebSocket | null = null;
  live = false;
  stopped = true;
  delay = 1000;
  ping: number | null = null;
  reconnectTimer: number | null = null;
  subs = new Map<string, Set<MessageHandler>>();
  liveListeners = new Set<(live: boolean) => void>();

  connect() {
    if (!this.stopped && this.ws && this.ws.readyState <= WebSocket.OPEN) return;
    this.stopped = false;
    this._open();
  }

  disconnect() {
    this.stopped = true;
    window.clearTimeout(this.reconnectTimer ?? undefined);
    window.clearInterval(this.ping ?? undefined);
    this.subs.clear();
    this.ws?.close();
    this.ws = null;
    this._setLive(false);
  }

  subscribe(conversationId: string, onMessage: MessageHandler) {
    if (!conversationId) return () => {};
    if (!this.subs.has(conversationId)) {
      this.subs.set(conversationId, new Set());
      this._send({ type: 'subscribe', conversation_id: conversationId });
    }
    this.subs.get(conversationId)!.add(onMessage);
    return () => this.unsubscribe(conversationId, onMessage);
  }

  unsubscribe(conversationId: string, onMessage: MessageHandler) {
    const set = this.subs.get(conversationId);
    if (!set) return;
    set.delete(onMessage);
    if (set.size === 0) {
      this.subs.delete(conversationId);
      this._send({ type: 'unsubscribe', conversation_id: conversationId });
    }
  }

  sendTyping(conversationId: string, typing: boolean) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return;
    this._send({
      type: typing ? 'typing.start' : 'typing.stop',
      conversation_id: conversationId,
    });
  }

  sendMessage(conversationId: string, content: string, client_msg_id: string) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return false;
    this._send({
      type: 'message.send',
      conversation_id: conversationId,
      content,
      client_msg_id,
    });
    return true;
  }

  onLive(fn: (live: boolean) => void) {
    this.liveListeners.add(fn);
    fn(this.live);
    return () => this.liveListeners.delete(fn);
  }

  _setLive(value: boolean) {
    this.live = value;
    this.liveListeners.forEach((fn) => fn(value));
  }

  _send(frame: Frame) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(frame));
    }
  }

  _open() {
    if (this.stopped) return;
    window.clearInterval(this.ping ?? undefined);
    try {
      this.ws = new WebSocket(wsURL());
    } catch {
      this._scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.delay = 1000;
      this._setLive(true);
      for (const convId of this.subs.keys()) {
        this._send({ type: 'subscribe', conversation_id: convId });
      }
      this.ping = window.setInterval(() => {
        if (this.ws?.readyState === WebSocket.OPEN) this._send({ type: 'ping' });
      }, 25000);
    };

    this.ws.onmessage = (ev) => {
      let frame: Frame;
      try {
        frame = JSON.parse(ev.data) as Frame;
      } catch {
        return;
      }
      if (frame.type === 'typing') {
        const convId = frame.conversation_id;
        if (convId && this.subs.has(convId)) {
          this.subs.get(convId)!.forEach((fn) =>
            fn({ __typing: true, user_id: frame.user_id, typing: frame.typing })
          );
        }
        return;
      }
      if (frame.type === 'conversation.read') {
        const convId = frame.conversation_id;
        if (convId && this.subs.has(convId)) {
          this.subs.get(convId)!.forEach((fn) =>
            fn({ __read: true, user_id: frame.user_id, last_read_at: frame.last_read_at })
          );
        }
        return;
      }
      if (frame.type !== 'message.new' && frame.type !== 'message.ack') return;
      const msg = typeof frame.message === 'string' ? JSON.parse(frame.message) : frame.message;
      if (!msg) return;
      const convId = frame.conversation_id || (msg as { conversation_id?: string }).conversation_id;
      if (convId && this.subs.has(convId)) {
        this.subs.get(convId)!.forEach((fn) => fn(msg));
        return;
      }
      this.subs.forEach((fns) => fns.forEach((fn) => fn(msg)));
    };

    this.ws.onclose = () => {
      this._setLive(false);
      window.clearInterval(this.ping ?? undefined);
      if (!this.stopped) this._scheduleReconnect();
    };

    this.ws.onerror = () => this.ws?.close();
  }

  _scheduleReconnect() {
    window.clearTimeout(this.reconnectTimer ?? undefined);
    const wait = this.delay * (0.5 + Math.random() * 0.5);
    this.reconnectTimer = window.setTimeout(() => this._open(), wait);
    this.delay = Math.min(this.delay * 2, 30000);
  }
}

export const sessionSocket = new SessionSocket();
