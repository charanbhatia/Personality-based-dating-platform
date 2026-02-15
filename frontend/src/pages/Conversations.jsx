import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';

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

  if (loading) return <div className="loading-page">Loading...</div>;

  return (
    <div className="page">
      <h1>Messages</h1>
      <p>Your conversations.</p>
      {list.length === 0 ? (
        <p>No conversations yet. Start from a match.</p>
      ) : (
        <ul className="conv-list">
          {list.map((c) => {
            const otherId = c.user1_id === user?.id ? c.user2_id : c.user1_id;
            const label = otherId ? `Conversation (${String(otherId).slice(0, 8)}...)` : 'Conversation';
            return (
              <li key={c.id}>
                <Link to={`/conversations/${c.id}`}>{label}</Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
