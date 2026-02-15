import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { conversations as convApi } from '../api';

export default function StartConversation() {
  const { userId } = useParams();
  const navigate = useNavigate();

  useEffect(() => {
    if (!userId) return;
    convApi
      .start(userId)
      .then((res) => navigate(`/conversations/${res.data.id}`))
      .catch(() => navigate('/conversations'));
  }, [userId, navigate]);

  return (
    <div className="page redirect-msg">
      <p>Starting conversation...</p>
    </div>
  );
}
