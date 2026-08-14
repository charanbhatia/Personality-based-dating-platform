import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { personality, errorCode, qk } from '../../api';
import { TRAIT_KEYS, TRAIT_LABEL } from '../../lib/genders';
import { useAuth } from '../../context/AuthContext';
import Loading from '../../components/Loading';
import ErrorState from '../../components/ErrorState';

export default function Quiz() {
  const navigate = useNavigate();
  const { refreshUser } = useAuth();
  const { data, error, isPending, refetch } = useQuery({
    queryKey: qk.assessment,
    queryFn: () => personality.assessment().then((r) => r.data),
  });
  const [answers, setAnswers] = useState({});
  const [index, setIndex] = useState(0);
  const [saving, setSaving] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [traits, setTraits] = useState(null);

  if (isPending) return <Loading text="Loading the quiz…" />;
  if (error) return <ErrorState text="We couldn't load the personality quiz." onRetry={() => refetch()} />;

  const questions = data?.questions || [];
  const q = questions[index];
  const done = Object.keys(answers).length === questions.length && questions.length > 0;

  async function submit() {
    setSaving(true);
    setSubmitError('');
    try {
      const payload = questions.map((item) => ({
        question_id: item.id,
        value: Number(answers[item.id]),
      }));
      const res = await personality.submit(payload);
      setTraits(res.data.traits);
      await refreshUser();
    } catch (err) {
      if (errorCode(err) === 'retake_too_soon') {
        try {
          const me = await personality.me();
          setTraits(me.data.traits);
        } catch {
          setSubmitError(err.message);
        }
      } else {
        setSubmitError(err.message);
      }
    } finally {
      setSaving(false);
    }
  }

  if (traits) {
    return (
      <div className="page">
        <h1>Your traits</h1>
        <p>These scores come from the server, not from the browser.</p>
        <ul className="trait-list">
          {TRAIT_KEYS.map((key) => (
            <li key={key}>
              <span>{TRAIT_LABEL[key]}</span>
              <div className="trait-bar">
                <div style={{ width: `${Math.round((traits[key] || 0) * 100)}%` }} />
              </div>
              <b>{Math.round((traits[key] || 0) * 100)}%</b>
            </li>
          ))}
        </ul>
        <button type="button" className="btn btn-primary" onClick={() => navigate('/onboarding/preferences')}>
          Continue
        </button>
      </div>
    );
  }

  if (!data?.can_submit) {
    return (
      <div className="page">
        <h1>Quiz already complete</h1>
        <p>
          You can retake after {data?.next_retake_at ? new Date(data.next_retake_at).toLocaleDateString() : 'the waiting period'}.
        </p>
        <button type="button" className="btn btn-primary" onClick={() => navigate('/onboarding/preferences')}>
          Continue
        </button>
      </div>
    );
  }

  return (
    <div className="page">
      <p className="section-kicker">Question {index + 1} of {questions.length}</p>
      <h1>Personality quiz</h1>
      {q && (
        <>
          <p className="quiz-prompt">{q.prompt}</p>
          <div className="scale-row" role="group" aria-label="Answer from 1 disagree to 5 agree">
            {[1, 2, 3, 4, 5].map((n) => (
              <button
                key={n}
                type="button"
                className={`scale-btn${answers[q.id] === n ? ' is-on' : ''}`}
                onClick={() => {
                  setAnswers((a) => ({ ...a, [q.id]: n }));
                  if (index < questions.length - 1) setIndex(index + 1);
                }}
              >
                {n}
              </button>
            ))}
          </div>
          <p className="auth-sub">1 = strongly disagree · 5 = strongly agree</p>
          <div className="match-actions">
            <button type="button" className="btn btn-ghost" disabled={index === 0} onClick={() => setIndex((i) => i - 1)}>
              Back
            </button>
            {index < questions.length - 1 ? (
              <button type="button" className="btn btn-primary" disabled={!answers[q.id]} onClick={() => setIndex((i) => i + 1)}>
                Next
              </button>
            ) : (
              <button type="button" className="btn btn-primary" disabled={!done || saving} onClick={submit}>
                {saving ? 'Scoring…' : 'See my traits'}
              </button>
            )}
          </div>
        </>
      )}
      {submitError && <div className="alert alert-error" role="alert">{submitError}</div>}
    </div>
  );
}
