// Fixed animated aurora backdrop rendered behind the whole app.
export default function BackgroundFX() {
  return (
    <div className="bg-fx" aria-hidden="true">
      <div className="bg-orb bg-orb--rose" />
      <div className="bg-orb bg-orb--violet" />
      <div className="bg-orb bg-orb--cyan" />
      <div className="bg-grid" />
      <div className="bg-noise" />
    </div>
  );
}
