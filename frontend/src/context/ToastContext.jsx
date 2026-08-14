import { createContext, useContext, useState, useCallback, useEffect } from 'react';
import { setApiErrorHandler } from '../api';

const ToastContext = createContext(null);

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([]);

  const push = useCallback((text, kind = 'info') => {
    const id = crypto.randomUUID();
    setToasts((list) => [...list, { id, text, kind }]);
    window.setTimeout(() => {
      setToasts((list) => list.filter((t) => t.id !== id));
    }, 4200);
  }, []);

  useEffect(() => {
    setApiErrorHandler((text) => push(text, 'error'));
    return () => setApiErrorHandler(null);
  }, [push]);

  return (
    <ToastContext.Provider value={{ push }}>
      {children}
      <div className="toast-stack" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={`toast toast-${t.kind}`} role="status">
            {t.text}
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useToast() {
  const ctx = useContext(ToastContext);
  if (!ctx) return { push: () => {} };
  return ctx;
}
