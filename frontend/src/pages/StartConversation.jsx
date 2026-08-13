import { useEffect } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import { conversations as convApi } from '../api';
import Loading from '../components/Loading';

export default function StartConversation() {
  const { userId } = useParams();
  const navigate = useNavigate();

  useEffect(() => {
    if (!userId) return;
    // `replace` matters: without it, Back returns to this interstitial, which
    // immediately re-POSTs and forwards again — trapping the user in the chat.
    convApi
      .start(userId)
      .then((res) => navigate(`/app/conversations/${res.data.id}`, { replace: true }))
      .catch(() => navigate('/app/conversations', { replace: true }));
  }, [userId, navigate]);

  return <Loading text="Starting your conversation…" />;
}
