export default function Loading({ text = 'Loading…' }) {
  return (
    <div className="loading-page">
      <div className="spinner" />
      <p>{text}</p>
    </div>
  );
}
