import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';
import Avatar from '../components/Avatar';
import { IconChat, IconArrowLeft } from '../components/Icons';

export default function Conversations() {
  const { user } = useAuth();
  const [list, setList] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    convApi
      .list()
      .then((res) => setList(res.data.conversations || []))
      .catch(() => setList([]))
      .finally(() => setLoading(false));
  }, []);

  if (loading)
    return (
      <div className="loading-page">
        <div className="spinner" />
        <p>Loading messages…</p>
      </div>
    );

  return (
    <div className="page">
      <span className="section-kicker">
        <IconChat /> Messages
      </span>
      <h1>Your conversations</h1>
      <p>Meaningful chats with the people you've matched with.</p>

      {list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconChat /></span>
          <h3>No conversations yet</h3>
          <p>When you message a match, your chats will show up here.</p>
          <Link to="/matches" className="btn btn-primary">Find someone to talk to</Link>
        </div>
      ) : (
        <ul className="conv-list stagger">
          {list.map((c) => {
            const otherId = c.user1_id === user?.id ? c.user2_id : c.user1_id;
            const short = otherId ? String(otherId).slice(0, 8) : '';
            return (
              <li key={c.id}>
                <Link to={`/conversations/${c.id}`} className="conv-item">
                  <Avatar name={short || 'Chat'} seed={otherId} size={46} />
                  <span className="conv-text">
                    <span className="conv-title">
                      {otherId ? `Conversation · ${short}` : 'Conversation'}
                    </span>
                    <span className="conv-sub">Tap to open the chat</span>
                  </span>
                  <span className="conv-arrow">
                    <IconArrowLeft style={{ transform: 'rotate(180deg)' }} />
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
