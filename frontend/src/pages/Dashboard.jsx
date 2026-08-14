import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import TiltCard from '../components/TiltCard';
import { IconHeart, IconChat, IconUser, IconSpark, IconArrowLeft } from '../components/Icons';

const features = [
  {
    to: '/app/matches',
    icon: <IconHeart />,
    title: 'Discover matches',
    desc: 'See people whose personality and values line up with yours, ranked by real compatibility.',
    cta: 'Browse matches',
  },
  {
    to: '/app/conversations',
    icon: <IconChat />,
    title: 'Your conversations',
    desc: 'Pick up meaningful chats with people you actually connect with.',
    cta: 'Open messages',
  },
  {
    to: '/app/profile',
    icon: <IconUser />,
    title: 'Shape your profile',
    desc: 'Tune your bio and details so we can find your kindred spirits.',
    cta: 'Edit profile',
  },
];

export default function Dashboard() {
  const { user } = useAuth();

  return (
    <div>
      <section className="dash-hero">
        <span className="section-kicker">
          <IconSpark /> Your space
        </span>
        <h1>
          Welcome back, {(user?.name || user?.email || 'there').split(' ')[0]}
        </h1>
        <p>
          Great connections start with who you are. Here's where you manage your profile,
          discover compatible people, and keep your conversations going.
        </p>
      </section>

      <div className="feature-grid stagger">
        {features.map((f) => (
          <TiltCard key={f.to} max={9}>
            <Link to={f.to} className="feature-card">
              <span className="feature-icon">{f.icon}</span>
              <h3>{f.title}</h3>
              <p>{f.desc}</p>
              <span className="feature-cta">
                {f.cta} <IconArrowLeft style={{ transform: 'rotate(180deg)' }} />
              </span>
            </Link>
          </TiltCard>
        ))}
      </div>
    </div>
  );
}
