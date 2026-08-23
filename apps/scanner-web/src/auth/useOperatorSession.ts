import { useEffect, useMemo, useState } from 'react';
import { createClient, type Session } from '@supabase/supabase-js';

export function useOperatorSession() {
  const url = String(import.meta.env.VITE_SUPABASE_URL ?? '').trim();
  const anonKey = String(import.meta.env.VITE_SUPABASE_ANON_KEY ?? '').trim();

  const client = useMemo(
    () =>
      url && anonKey
        ? createClient(url, anonKey, {
            auth: {
              persistSession: true,
              autoRefreshToken: true,
              detectSessionInUrl: true,
            },
          })
        : null,
    [url, anonKey],
  );

  const [session, setSession] = useState<Session | null>(null);
  const [loading, setLoading] = useState(Boolean(client));
  const [error, setError] = useState(
    client ? '' : 'Operator authentication is not configured for this deployment.',
  );

  useEffect(() => {
    if (!client) {
      setLoading(false);
      return;
    }

    let mounted = true;

    void client.auth.getSession().then(({ data, error: sessionError }) => {
      if (!mounted) return;

      if (sessionError) {
        setError(sessionError.message);
      }

      setSession(data.session ?? null);
      setLoading(false);
    });

    const {
      data: { subscription },
    } = client.auth.onAuthStateChange((_event, nextSession) => {
      if (!mounted) return;
      setSession(nextSession);
      setLoading(false);
    });

    return () => {
      mounted = false;
      subscription.unsubscribe();
    };
  }, [client]);

  const signIn = async (email: string, password: string) => {
    if (!client) {
      setError('Operator authentication is not configured for this deployment.');
      return false;
    }

    setError('');
    setLoading(true);

    const { error: signInError } = await client.auth.signInWithPassword({
      email: email.trim(),
      password,
    });

    setLoading(false);

    if (signInError) {
      setError(signInError.message);
      return false;
    }

    return true;
  };

  const signOut = async () => {
    if (!client) return;

    setError('');
    await client.auth.signOut();
    setSession(null);
  };

  return {
    token: session?.access_token ?? '',
    authenticated: Boolean(session?.access_token),
    userLabel: session?.user.email ?? session?.user.id ?? 'Operator',
    loading,
    error,
    signIn,
    signOut,
  };
}
