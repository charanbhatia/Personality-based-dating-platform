import { useEffect, useRef, useState, useCallback } from 'react';
import { sessionSocket } from '../api';

export default function useChatSocket(conversationId, onMessage) {
  const [live, setLive] = useState(sessionSocket.live);
  const onMessageRef = useRef(onMessage);

  useEffect(() => {
    onMessageRef.current = onMessage;
  }, [onMessage]);

  useEffect(() => sessionSocket.onLive(setLive), []);

  useEffect(() => {
    if (!conversationId) return undefined;
    const handler = (msg) => onMessageRef.current?.(msg);
    const unsub = sessionSocket.subscribe(conversationId, handler);
    return () => {
      sessionSocket.sendTyping(conversationId, false);
      unsub();
    };
  }, [conversationId]);

  const send = useCallback((content, client_msg_id) => {
    return sessionSocket.sendMessage(conversationId, content, client_msg_id);
  }, [conversationId]);

  const sendTyping = useCallback((typing) => {
    sessionSocket.sendTyping(conversationId, typing);
  }, [conversationId]);

  return { live, send, sendTyping };
}
