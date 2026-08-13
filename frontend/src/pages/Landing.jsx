import { useState } from 'react';
import { Link } from 'react-router-dom';
import Brand from '../components/Brand';
import CompatibilityRing from '../components/CompatibilityRing';
import { IconHeart, IconSpark, IconShield, IconChat, IconUser, IconCompass } from '../components/Icons';
import './Landing.css';

// The five traits the assessment scores. Order matters — it is the order the
// results screen will use, so the demo teaches the same shape.
const TRAITS = [
  { key: 'openness', label: 'Openness', blurb: 'Curiosity, and appetite for the unfamiliar' },
  { key: 'conscientiousness', label: 'Conscientiousness', blurb: 'Planning, follow-through, structure' },
  { key: 'extraversion', label: 'Extraversion', blurb: 'Where your energy comes from' },
  { key: 'agreeableness', label: 'Agreeableness', blurb: 'Warmth, and how you handle friction' },
  { key: 'neuroticism', label: 'Emotional volatility', blurb: 'How much stress moves you' },
];

// A sample profile, not a member. Fixed so the demo is reproducible.
const SAMPLE = {
  name: 'Priya',
  detail: '29 · Bangalore',
  bio: 'Reads too much, plans too little. Looking for someone to argue with kindly.',
  traits: { openness: 0.82, conscientiousness: 0.44, extraversion: 0.35, agreeableness: 0.78, neuroticism: 0.4 },
};

const START = { openness: 0.7, conscientiousness: 0.5, extraversion: 0.45, agreeableness: 0.7, neuroticism: 0.45 };

// The same arithmetic the server runs: mean of (1 - |difference|) across every
// trait both people have answered. Nothing is hidden in a black box.
function similarity(a, b) {
  const shared = Object.keys(a).filter((k) => k in b);
  if (shared.length === 0) return 0.5;
  return shared.reduce((sum, k) => sum + (1 - Math.abs(a[k] - b[k])), 0) / shared.length;
}

const STEPS = [
  {
    icon: <IconSpark />,
    title: 'Take the assessment',
    body: 'Answer questions about how you think, decide, and handle people. Around ten minutes, once.',
  },
  {
    icon: <IconCompass />,
    title: 'See who fits',
    body: 'Everyone is scored against your traits and ranked. You get the number and the reasoning behind it, not a shuffled deck.',
  },
  {
    icon: <IconChat />,
    title: 'Start talking',
    body: 'Message the people who actually line up. No queue to grind through before you reach someone worth your evening.',
  },
];

const FAQ = [
  {
    q: 'How is this different from every other dating app?',
    a: 'Those rank people by photo and let you sort it out by swiping. Kindred scores compatibility from a personality assessment before you ever see a face, and shows you why two people scored what they did.',
  },
  {
    q: 'How long does the assessment take?',
    a: 'About ten minutes, and you only do it once. It is more upfront effort than a swipe — that is the trade for not spending weeks on conversations that were never going to work.',
  },
  {
    q: 'Do I have to upload photos?',
    a: 'You can add one, and people will see it on your profile. It is never an input to matching.',
  },
  {
    q: 'Is there an app to install?',
    a: 'No. Kindred runs in your browser on your phone or laptop. Nothing to download, nothing taking up space, no app store in the middle.',
  },
  {
    q: 'What happens to what I write?',
    a: 'Your profile is yours to edit or empty whenever you want. Passwords are hashed, never stored as text, and sessions are token-based.',
  },
];

export default function Landing() {
  const [you, setYou] = useState(START);
  const score = Math.round(similarity(you, SAMPLE.traits) * 100);

  return (
    <div className="lp">
      <header className="lp-nav">
        <Brand to="/" />
        <div className="lp-nav-actions">
          <Link to="/login" className="lp-link">Log in</Link>
          <Link to="/register" className="btn btn-primary lp-nav-cta">Take the assessment</Link>
        </div>
      </header>

      <section className="lp-hero">
        <span className="section-kicker"><IconSpark /> Personality-first dating</span>
        <h1>
          Meet minds,<br />not just <span className="text-gradient">faces.</span>
        </h1>
        <p className="lp-hero-sub">
          Kindred matches you on how you actually think — your traits, your values, what you
          need from someone. Not a photograph you swiped past in half a second.
        </p>
        <div className="lp-cta-row">
          <Link to="/register" className="btn btn-primary">Take the assessment</Link>
          <Link to="/login" className="btn btn-ghost">I already have an account</Link>
        </div>
        <p className="lp-microcopy">Around ten minutes · Nothing to install · Free while we are in beta</p>
      </section>

      <section className="lp-section">
        <h2>How it works</h2>
        <ol className="lp-steps">
          {STEPS.map((s, i) => (
            <li key={s.title}>
              <span className="lp-step-n">{i + 1}</span>
              <span className="lp-step-icon">{s.icon}</span>
              <h3>{s.title}</h3>
              <p>{s.body}</p>
            </li>
          ))}
        </ol>
      </section>

      <section className="lp-section lp-demo-section">
        <h2>Try the matching</h2>
        <p className="lp-section-sub">
          These are the five traits we score on. Move yours and watch the compatibility
          recompute — this is the same arithmetic the server runs, not an animation.
        </p>

        <div className="lp-demo">
          <div className="lp-demo-controls">
            <span className="lp-demo-label">You</span>
            {TRAITS.map((t) => (
              <div className="lp-slider" key={t.key}>
                <label htmlFor={`t-${t.key}`}>
                  <span>{t.label}</span>
                  <b>{Math.round(you[t.key] * 100)}</b>
                </label>
                <input
                  id={`t-${t.key}`}
                  type="range"
                  min="0"
                  max="100"
                  value={Math.round(you[t.key] * 100)}
                  onChange={(e) => setYou((v) => ({ ...v, [t.key]: Number(e.target.value) / 100 }))}
                />
                <span className="lp-slider-blurb">{t.blurb}</span>
              </div>
            ))}
          </div>

          <div className="lp-demo-result">
            {/* The visitor is driving this one, so it must track the value
                immediately rather than waiting to be scrolled into view. */}
            <CompatibilityRing value={score} size={132} stroke={10} animateOnView={false} />
            <div className="lp-sample">
              <b>{SAMPLE.name}</b>
              <span className="lp-sample-detail">{SAMPLE.detail}</span>
              <p>{SAMPLE.bio}</p>
            </div>
            <ul className="lp-bars">
              {TRAITS.map((t) => {
                const gap = Math.abs(you[t.key] - SAMPLE.traits[t.key]);
                return (
                  <li key={t.key}>
                    <span className="lp-bar-label">{t.label}</span>
                    <span className="lp-bar-track">
                      <span className="lp-bar-fill" style={{ width: `${(1 - gap) * 100}%` }} />
                    </span>
                    <span className="lp-bar-val">{Math.round((1 - gap) * 100)}</span>
                  </li>
                );
              })}
            </ul>
            <p className="lp-disclaimer">
              Sample profile for illustration. Not a member, and not anyone's real results.
            </p>
          </div>
        </div>
      </section>

      <section className="lp-section">
        <h2>What we deliberately don't do</h2>
        <div className="lp-nots">
          <div>
            <h3>No swiping</h3>
            <p>Sorting humans with your thumb is a fast way to see many people and meet none of them.</p>
          </div>
          <div>
            <h3>No photo-first ranking</h3>
            <p>A photo can be on your profile. It never decides who reaches you, or who you reach.</p>
          </div>
          <div>
            <h3>No infinite feed</h3>
            <p>There is no reward for opening Kindred twelve times a day. A good match is the point, not your session count.</p>
          </div>
        </div>
      </section>

      <section className="lp-section lp-trust">
        <h2>What we can tell you today</h2>
        <div className="lp-trust-grid">
          <div>
            <span className="lp-trust-icon"><IconShield /></span>
            <h3>Your credentials</h3>
            <p>Passwords are hashed with bcrypt and never stored as readable text. Sessions use signed tokens.</p>
          </div>
          <div>
            <span className="lp-trust-icon"><IconUser /></span>
            <h3>Your profile</h3>
            <p>Everything on it is yours to rewrite or empty whenever you want, from one screen.</p>
          </div>
          <div>
            <span className="lp-trust-icon"><IconHeart /></span>
            <h3>Your matches</h3>
            <p>Scored on the traits you gave us, and nothing else. No behavioural profiling, no ad network.</p>
          </div>
        </div>
      </section>

      <section className="lp-section">
        <h2>Questions people ask</h2>
        <div className="lp-faq">
          {FAQ.map((f) => (
            <details key={f.q}>
              <summary>{f.q}</summary>
              <p>{f.a}</p>
            </details>
          ))}
        </div>
      </section>

      <section className="lp-final">
        <h2>Worth ten minutes?</h2>
        <p>Take the assessment and find out who actually lines up with you.</p>
        <Link to="/register" className="btn btn-primary">Take the assessment</Link>
      </section>

      <footer className="lp-footer">
        <Brand to="/" />
        <nav className="lp-footer-links">
          <Link to="/login">Log in</Link>
          <Link to="/register">Create an account</Link>
        </nav>
        <p>© {new Date().getFullYear()} Kindred · Built for meaningful connections</p>
      </footer>
    </div>
  );
}
