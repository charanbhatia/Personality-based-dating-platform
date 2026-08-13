import { Link } from 'react-router-dom';
import { IconHeart } from './Icons';

export default function Brand() {
  return (
    <Link to="/" className="brand">
      <span className="brand-mark">
        <IconHeart />
      </span>
      <span className="brand-name">
        <b>Kin</b>
        <span className="text-gradient">dred</span>
      </span>
    </Link>
  );
}
