import { useState, useEffect, useRef, useLayoutEffect } from 'react';
import { useParams, useLocation, useNavigate } from 'react-router-dom';
import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi, qk } from '../api';
import useChatSocket from '../hooks/useChatSocket';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import SafetyActions from '../components/SafetyActions';
import { IconSend, IconChat } from '../components/Icons';

const time = (iso) =>
  new Date(iso).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
const dayKey = (iso) => new Date(iso).toDateString();
const dayLabel = (iso) =>
  new Date(iso).toLocaleDateString(undefined, { weekday: 'long', month: 'short', day: 'numeric' });

function mergeById(prev, incoming) {
  const byId = new Map(prev.map((m) => [m.id, m]));
  const clientToId = new Map(
    prev.filter((m) => m.client_msg_id).map((m) => [m.client_msg_id, m.id])
  );
  for (const m of incoming) {
    const oldId = m.client_msg_id && clientToId.get(m.client_msg_id);
    if (oldId && oldId !== m.id) byId.delete(oldId);
    byId.set(m.id, m);
  }
  return [...byId.values()].sort(
    (a, b) => new Date(a.created_at) - new Date(b.created_at)
  );
}

function lastSeenMine(messages, userId, peerLastReadAt) {
  if (!peerLastReadAt || !userId) return null;
  const cutoff = new Date(peerLastReadAt).getTime();
  let last = null;
  for (const m of messages) {
    if (m.sender_id === userId && m.created_at && new Date(m.created_at).getTime() <= cutoff) {
      last = m.id;
    }
  }
  return last;
}

export default function Chat() {
  const { id } = useParams();
  const { user } = useAuth();
  const qc = useQueryClient();
  const navigate = useNavigate();
  const loc = useLocation().state || {};
  const thread = useQuery({
    queryKey: qk.conversation(id),
    queryFn: () => convApi.get(id).then((r) => r.data),
  });
  const history = useInfiniteQuery({
    queryKey: qk.messages(id),
    queryFn: ({ pageParam }) =>
      convApi
        .getMessages(id, { limit: 50, ...(pageParam ? { cursor: pageParam } : {}) })
        .then((r) => r.data),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    staleTime: 0,
  });
  const [liveTail, setLiveTail] = useState([]);
  const [peerLastReadAt, setPeerLastReadAt] = useState(null);
  const [content, setContent] = useState('');
  const [sending, setSending] = useState(false);
  const [sendError, setSendError] = useState('');
  const [peerTyping, setPeerTyping] = useState(false);
  const typingTimer = useRef(null);
  const bottomRef = useRef(null);
  const scrollerRef = useRef(null);
  const stickRef = useRef(true);
  const prevHeight = useRef(0);

  const pageItems = (history.data?.pages || [])
    .slice()
    .reverse()
    .flatMap((p) => p.items || p.messages || []);
  const messages = mergeById(pageItems, liveTail);
  const peer = thread.data?.peer || {
    name: loc.name,
    bio: loc.bio,
    photo_url: loc.photo_url,
    user_id: loc.user_id,
  };

  useEffect(() => {
    if (thread.data?.peer_last_read_at) setPeerLastReadAt(thread.data.peer_last_read_at);
  }, [thread.data?.peer_last_read_at]);

  const { live, send: wsSend, sendTyping } = useChatSocket(id, (msg) => {
    if (msg?.__typing) {
      if (msg.user_id !== user?.id) setPeerTyping(Boolean(msg.typing));
      return;
    }
    if (msg?.__read) {
      if (msg.user_id && msg.user_id !== user?.id) setPeerLastReadAt(msg.last_read_at);
      return;
    }
    setLiveTail((prev) => mergeById(prev, [msg]));
    qc.invalidateQueries({ queryKey: qk.conversations });
  });

  useEffect(() => {
    convApi.markRead(id).catch(() => {});
  }, [id]);

  useEffect(() => () => {
    window.clearTimeout(typingTimer.current);
    sendTyping(false);
  }, [id, sendTyping]);

  useEffect(() => {
    setLiveTail([]);
    setPeerLastReadAt(null);
  }, [id]);

  useEffect(() => {
    if (live) return undefined;
    const t = setInterval(() => {
      history.refetch();
      thread.refetch();
    }, 8000);
    return () => clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, live]);

  useLayoutEffect(() => {
    const el = scrollerRef.current;
    if (el && prevHeight.current) {
      el.scrollTop = el.scrollHeight - prevHeight.current;
      prevHeight.current = 0;
      return;
    }
    if (stickRef.current) bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages.length]);

  async function loadOlder() {
    const el = scrollerRef.current;
    prevHeight.current = el?.scrollHeight ?? 0;
    stickRef.current = false;
    await history.fetchNextPage();
  }

  function onScroll() {
    const el = scrollerRef.current;
    if (!el) return;
    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    if (el.scrollTop < 48 && history.hasNextPage && !history.isFetchingNextPage) {
      loadOlder();
    }
  }

  async function handleSend(e) {
    e.preventDefault();
    if (!content.trim() || sending) return;
    setSending(true);
    setSendError('');
    const clientId = crypto.randomUUID();
    try {
      const sent = wsSend(content.trim(), clientId);
      if (sent) {
        setLiveTail((prev) =>
          mergeById(prev, [{
            id: clientId,
            client_msg_id: clientId,
            sender_id: user?.id,
            content: content.trim(),
            created_at: new Date().toISOString(),
          }])
        );
      } else {
        const res = await convApi.sendMessage(id, content.trim(), clientId);
        setLiveTail((prev) => mergeById(prev, [res.data]));
      }
      setContent('');
      stickRef.current = true;
      convApi.markRead(id).catch(() => {});
      qc.invalidateQueries({ queryKey: qk.conversations });
    } catch (err) {
      setSendError(err.message);
    } finally {
      setSending(false);
    }
  }

  if (history.isPending) return <Loading text="Opening chat…" />;

  if (history.isError)
    return (
      <div className="page">
        <ErrorState text="We couldn't open this conversation." onRetry={() => history.refetch()} />
      </div>
    );

  const peerName = peer.name;
  const seenId = lastSeenMine(messages, user?.id, peerLastReadAt);

  return (
    <div className="page chat-page">
      <div className="chat-head">
        <Avatar name={peerName} src={peer.photo_url || undefined} seed={peer.user_id || id} size={40} />
        <span className="chat-peer">
          <b>{peerName || 'Conversation'}</b>
          {peer.bio && <span>{peer.bio}</span>}
          <span className="conv-sub">{live ? (peerTyping ? 'Typing…' : 'Live') : 'Reconnecting… polling'}</span>
        </span>
        {peer.user_id && (
          <SafetyActions
            userId={peer.user_id}
            name={peerName}
            onHidden={() => navigate('/app/conversations', { replace: true })}
          />
        )}
      </div>

      <div className="chat-messages" ref={scrollerRef} onScroll={onScroll}>
        {history.hasNextPage && (
          <button
            type="button"
            className="btn btn-ghost chat-load-older"
            disabled={history.isFetchingNextPage}
            onClick={loadOlder}
          >
            {history.isFetchingNextPage ? 'Loading…' : 'Load earlier messages'}
          </button>
        )}
        {messages.length === 0 ? (
          <div className="chat-empty">
            <span className="empty-icon"><IconChat /></span>
            {peer.bio ? (
              <>
                <blockquote className="chat-opener">{peer.bio}</blockquote>
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
                      {mine && m.id === seenId ? ' · Seen' : ''}
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
          onChange={(e) => {
            setContent(e.target.value);
            sendTyping(true);
            window.clearTimeout(typingTimer.current);
            typingTimer.current = window.setTimeout(() => sendTyping(false), 1500);
          }}
          placeholder={peer.bio ? 'Reply to what they wrote…' : 'Type a message…'}
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
