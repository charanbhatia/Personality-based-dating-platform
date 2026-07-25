import { useState, useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';
import Avatar from '../components/Avatar';
import { IconSend, IconChat } from '../components/Icons';

export default function Chat() {
  const { id } = useParams();
  const { user } = useAuth();
  const [messages, setMessages] = useState([]);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [sending, setSending] = useState(false);
  const bottomRef = useRef(null);

  useEffect(() => {
    if (!id) return;
    convApi
      .getMessages(id)
      .then((res) => setMessages(res.data.messages || []))
      .catch(() => setMessages([]))
      .finally(() => setLoading(false));
  }, [id]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  async function handleSend(e) {
    e.preventDefault();
    if (!content.trim() || sending) return;
    setSending(true);
    try {
      const res = await convApi.sendMessage(id, content.trim());
      setMessages((prev) => [...prev, res.data]);
      setContent('');
    } catch {
      /* ignore send error for now */
    } finally {
      setSending(false);
    }
  }

  if (loading)
    return (
      <div className="loading-page">
        <div className="spinner" />
        <p>Opening chat…</p>
      </div>
    );

  const shortId = String(id || '').slice(0, 8);

  return (
    <div className="page chat-page">
      <div className="chat-head">
        <Avatar name={shortId || 'Chat'} seed={id} size={44} ring />
        <span className="chat-peer">
          <b>Conversation · {shortId}</b>
          <span>active now</span>
        </span>
      </div>

      <div className="chat-messages">
        {messages.length === 0 ? (
          <div className="chat-empty">
            <span className="empty-icon" style={{ margin: '0 auto 0.6rem' }}>
              <IconChat />
            </span>
            <p>No messages yet — say hello and break the ice.</p>
          </div>
        ) : (
          messages.map((m) => (
            <div
              key={m.id}
              className={m.sender_id === user?.id ? 'bubble mine' : 'bubble theirs'}
            >
              {m.content}
            </div>
          ))
        )}
        <div ref={bottomRef} />
      </div>

      <form onSubmit={handleSend} className="chat-form">
        <input
          className="input"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="Type a message…"
        />
        <button type="submit" className="chat-send" disabled={sending || !content.trim()} title="Send">
          <IconSend />
        </button>
      </form>
    </div>
  );
}
