import { useState } from 'react';

const MAX_CHIPS = 20;
const MAX_LEN = 40;

export default function InterestChips({ value = [], onChange, max = MAX_CHIPS }) {
  const [draft, setDraft] = useState('');

  function add() {
    const t = draft.trim().slice(0, MAX_LEN);
    if (!t || value.includes(t) || value.length >= max) {
      setDraft('');
      return;
    }
    onChange([...value, t]);
    setDraft('');
  }

  function onKeyDown(e) {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault();
      add();
    } else if (e.key === 'Backspace' && !draft && value.length) {
      onChange(value.slice(0, -1));
    }
  }

  return (
    <div className="interest-chips">
      <div className="interest-chip-row">
        {value.map((tag) => (
          <button
            key={tag}
            type="button"
            className="chip chip-removable"
            onClick={() => onChange(value.filter((x) => x !== tag))}
          >
            {tag}
            <span aria-hidden="true"> ×</span>
          </button>
        ))}
      </div>
      {value.length < max && (
        <input
          id="interests"
          className="input"
          value={draft}
          maxLength={MAX_LEN}
          onChange={(e) => setDraft(e.target.value.replace(/,/g, ''))}
          onKeyDown={onKeyDown}
          onBlur={add}
          placeholder={value.length ? 'Add another…' : 'coffee, hiking, music'}
          aria-label="Add an interest"
        />
      )}
      <p className="field-hint">{value.length}/{max} interests</p>
    </div>
  );
}
