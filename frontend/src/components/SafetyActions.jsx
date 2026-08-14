import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { safety, qk } from '../api';
import { REPORT_REASONS } from '../lib/genders';
import ConfirmDialog from './ConfirmDialog';

export default function SafetyActions({ userId, name, onHidden }) {
  const qc = useQueryClient();
  const [confirm, setConfirm] = useState(null);
  const [reportReason, setReportReason] = useState('spam');

  async function hide() {
    qc.invalidateQueries({ queryKey: qk.discover });
    qc.invalidateQueries({ queryKey: qk.matches });
    qc.invalidateQueries({ queryKey: qk.conversations });
    onHidden?.();
  }

  return (
    <>
      <div className="match-actions chat-safety">
        <button type="button" className="btn btn-ghost" onClick={() => setConfirm('block')}>
          Block
        </button>
        <button type="button" className="btn btn-ghost" onClick={() => setConfirm('report')}>
          Report
        </button>
      </div>
      {confirm === 'block' && (
        <ConfirmDialog
          title={`Block ${name || 'this person'}?`}
          text="They will disappear from discovery and chat."
          confirmLabel="Block"
          danger
          onCancel={() => setConfirm(null)}
          onConfirm={async () => {
            await safety.block(userId);
            setConfirm(null);
            await hide();
          }}
        />
      )}
      {confirm === 'report' && (
        <ConfirmDialog
          title={`Report ${name || 'this person'}?`}
          text={
            <>
              <p>We will also hide them from your feed and chat.</p>
              <select className="form-select" value={reportReason} onChange={(e) => setReportReason(e.target.value)}>
                {REPORT_REASONS.map(([v, l]) => (
                  <option key={v} value={v}>{l}</option>
                ))}
              </select>
            </>
          }
          confirmLabel="Report and hide"
          danger
          onCancel={() => setConfirm(null)}
          onConfirm={async () => {
            await safety.report(userId, reportReason);
            await safety.block(userId);
            setConfirm(null);
            await hide();
          }}
        />
      )}
    </>
  );
}
