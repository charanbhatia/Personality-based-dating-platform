import { createContext, useContext, useState, useEffect, useCallback } from 'react';
import {
  auth as authApi,
  persistTokens,
  clearTokens,
  restoreSession,
  hasSession,
  sessionUser,
  sessionSocket,
} from '../api';
import { registerWebDevice, unregisterWebDevice } from '../lib/push';

const AuthContext = createContext(null);

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [loading, setLoading] = useState(() => hasSession());

  const refreshUser = useCallback(async () => {
    const res = await authApi.me();
    const u = sessionUser(res.data);
    setUser(u);
    return u;
  }, []);

  useEffect(() => {
    let alive = true;
    (async () => {
      const ok = await restoreSession();
      if (!ok) {
        if (alive) setLoading(false);
        return;
      }
      try {
        const res = await authApi.me();
        if (alive) setUser(sessionUser(res.data));
      } catch (err) {
        if (err.response?.status === 401) clearTokens();
      } finally {
        if (alive) setLoading(false);
      }
    })();
    return () => {
      alive = false;
    };
  }, []);

  useEffect(() => {
    if (!user?.id) {
      sessionSocket.disconnect();
      return undefined;
    }
    sessionSocket.connect();
    registerWebDevice().catch(() => {});
    return undefined;
  }, [user?.id]);

  const applyAuth = async (data) => {
    persistTokens(data);
    try {
      return await refreshUser();
    } catch {
      const u = sessionUser(data);
      setUser(u);
      return u;
    }
  };

  const login = async (email, password) => {
    const res = await authApi.login({ email, password });
    return applyAuth(res.data);
  };

  const register = async (data) => {
    const res = await authApi.register(data);
    return applyAuth(res.data);
  };

  const logout = async () => {
    try {
      await authApi.logout();
    } catch {
      // Client still has to drop the session even if the server is gone.
    }
    sessionSocket.disconnect();
    unregisterWebDevice().catch(() => {});
    clearTokens();
    setUser(null);
  };

  return (
    <AuthContext.Provider value={{ user, loading, login, register, logout, refreshUser, setUser }}>
      {children}
    </AuthContext.Provider>
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
