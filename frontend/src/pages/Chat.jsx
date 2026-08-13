import { useState, useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';
import useFetch from '../hooks/useFetch';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconSend, IconChat } from '../components/Icons';

export default function Chat() {
  const { id } = useParams();
  const { user } = useAuth();
  const { data, error, loading, retry } = useFetch(() => convApi.getMessages(id), [id]);
  const [messages, setMessages] = useState([]);
  const [content, setContent] = useState('');
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState('');
  const bottomRef = useRef(null);

  // History seeds the thread; sends append to it locally from there.
  useEffect(() => {
    if (data) setMessages(data.messages || []);
  }, [data]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  async function handleSend(e) {
    e.preventDefault();
    if (!content.trim() || sending) return;
    setSending(true);
    setSendError('');
    try {
      const res = await convApi.sendMessage(id, content.trim());
      setMessages((prev) => [...prev, res.data]);
      setContent('');
    } catch (err) {
      // Keep whatever they typed in the box so the message isn't lost.
      setSendError(err.message);
    } finally {
      setSending(false);
    }
  }

  if (loading) return <Loading text="Opening chat…" />;

  if (error)
    return (
      <div className="page">
        <ErrorState text="We couldn't open this conversation." onRetry={retry} />
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

      {sendError && (
        <div className="alert alert-error chat-alert" role="alert">
          {sendError}
        </div>
      )}

      <form onSubmit={handleSend} className="chat-form">
        <input
          className="input"
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="Type a message…"
          aria-label="Message"
        />
        <button
          type="submit"
          className="chat-send"
          disabled={sending || !content.trim()}
          aria-label="Send message"
        >
          <IconSend />
        </button>
      </form>
    </div>
  );
}
