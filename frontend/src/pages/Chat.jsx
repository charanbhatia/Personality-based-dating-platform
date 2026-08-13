import { useState, useEffect, useRef } from 'react';
import { useParams, useLocation } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';
import useFetch from '../hooks/useFetch';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconSend, IconChat } from '../components/Icons';

const time = (iso) =>
  new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
const dayKey = (iso) => new Date(iso).toDateString();
const dayLabel = (iso) =>
  new Date(iso).toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' });

export default function Chat() {
  const { id } = useParams();
  const { user } = useAuth();
  // Carried from whichever screen opened the thread. No endpoint returns the
  // other participant, so a direct URL load simply has no name — which is
  // better than printing a slice of their UUID at them.
  const { name: peerName, bio: peerBio } = useLocation().state || {};
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

  return (
    <div className="page chat-page">
      <div className="chat-head">
        <Avatar name={peerName} seed={id} size={40} />
        <span className="chat-peer">
          <b>{peerName || 'Conversation'}</b>
          {peerBio && <span>{peerBio}</span>}
        </span>
      </div>

      <div className="chat-messages">
        {messages.length === 0 ? (
          <div className="chat-empty">
            <span className="empty-icon"><IconChat /></span>
            {peerBio ? (
              <>
                <blockquote className="chat-opener">{peerBio}</blockquote>
                <p>That is all {peerName || 'they'} wrote. Reply to it.</p>
              </>
            ) : (
              <p>No messages yet — say hello.</p>
            )}
          </div>
        ) : (
          messages.map((m, i) => {
            const mine = m.sender_id === user?.id;
            const prev = messages[i - 1];
            const newDay = !prev || dayKey(prev.created_at) !== dayKey(m.created_at);
            return (
              <div key={m.id} className="chat-row">
                {newDay && m.created_at && <div className="chat-day">{dayLabel(m.created_at)}</div>}
                <div className={mine ? 'bubble mine' : 'bubble theirs'}>
                  <span className="sr-only">{mine ? 'You said' : `${peerName || 'They'} said`}</span>
                  {m.content}
                  {m.created_at && (
                    <time dateTime={m.created_at} className="bubble-time">
                      {time(m.created_at)}
                    </time>
                  )}
                </div>
              </div>
            );
          })
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
          placeholder={peerBio ? 'Reply to what they wrote…' : 'Type a message…'}
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
