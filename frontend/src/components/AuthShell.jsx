import Brand from './Brand';
import { IconHeart, IconSpark, IconShield } from './Icons';

// Split-screen auth layout: an immersive brand story on the left, the form on
// the right. The brand panel collapses on small screens.
export default function AuthShell({ children }) {
  return (
    <div className="auth-wrap">
      <aside className="auth-brand">
        <Brand />

        <div className="auth-hero">
          <h2>
            Meet minds,<br />not just <span className="text-gradient">faces.</span>
          </h2>
          <p>
            Kindred pairs you on personality, values and the way you think — so every
            match is someone worth talking to.
          </p>

          <div className="auth-points">
            <div className="auth-point">
              <span className="ap-icon"><IconSpark /></span>
              Personality-first matching, not endless swiping
            </div>
            <div className="auth-point">
              <span className="ap-icon"><IconHeart /></span>
              Compatibility scores backed by your real traits
            </div>
            <div className="auth-point">
              <span className="ap-icon"><IconShield /></span>
              Private, secure, and yours to control
            </div>
          </div>
        </div>

        <p className="auth-legal" style={{ color: 'var(--text-dim)', fontSize: '0.85rem', margin: 0 }}>
          © {new Date().getFullYear()} Kindred · Built for meaningful connections
        </p>
      </aside>

      <section className="auth-panel">{children}</section>
    </div>
  );
}
