import { useState, useEffect, useRef } from 'react';
import { useParams } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';

export default function Chat() {
  const { id } = useParams();
  const { user } = useAuth();
  const [messages, setMessages] = useState([]);
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
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
    bottomRef.current?.scrollIntoView();
  }, [messages]);

  async function handleSend(e) {
    e.preventDefault();
    if (!content.trim()) return;
    try {
      const res = await convApi.sendMessage(id, content.trim());
      setMessages((prev) => [...prev, res.data]);
      setContent('');
    } catch {}
  }

  if (loading) return <div className="loading-page">Loading...</div>;

  return (
    <div className="page chat-page">
      <div className="chat-messages">
        {messages.map((m) => (
          <div
            key={m.id}
            className={m.sender_id === user?.id ? 'message mine' : 'message theirs'}
          >
            {m.content}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>
      <form onSubmit={handleSend} className="chat-form">
        <input
          value={content}
          onChange={(e) => setContent(e.target.value)}
          placeholder="Type a message..."
        />
        <button type="submit">Send</button>
      </form>
    </div>
  );
}
