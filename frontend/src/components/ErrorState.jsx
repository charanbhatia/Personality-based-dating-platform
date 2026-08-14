import { IconAlert } from './Icons';

// Shown when a request fails, so a dead backend no longer reads as "no results".
export default function ErrorState({
  title = 'Something went wrong',
  text = "We couldn't load this. Check your connection and try again.",
  onRetry,
}) {
  return (
    <div className="empty-state">
      <span className="empty-icon">
        <IconAlert />
      </span>
      <h3>{title}</h3>
      <p>{text}</p>
      {onRetry && (
        <button type="button" className="btn btn-ghost" onClick={onRetry}>
          Try again
        </button>
      )}
    </div>
  );
}
