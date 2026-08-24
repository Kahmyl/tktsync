import { useEffect, useMemo, useState } from 'react';
import { createClient, type Session } from '@supabase/supabase-js';

function nameFromEmail(email: string | undefined) {
  const local = (email ?? '').split('@')[0]?.replace(/\d+$/g, '') ?? '';
  const words = local
    .split(/[._-]+/)
    .filter(Boolean)
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1).toLowerCase());
  return words.join(' ') || 'Operator';
}

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
    const { error: signOutError } = await client.auth.signOut();
    if (signOutError) {
      setError(signOutError.message);
      return false;
    }
    setSession(null);
    return true;
  };

  const sendPasswordReset = async (email: string) => {
    if (!client) {
      setError('Operator authentication is not configured for this deployment.');
      return false;
    }
    setError('');
    setLoading(true);
    const { error: resetError } = await client.auth.resetPasswordForEmail(email.trim(), {
      redirectTo: `${window.location.origin}/set-password`,
    });
    setLoading(false);
    if (resetError) {
      setError(resetError.message);
      return false;
    }
    return true;
  };

  const updatePassword = async (password: string) => {
    if (!client) {
      setError('Operator authentication is not configured for this deployment.');
      return false;
    }
    setError('');
    setLoading(true);
    const { error: updateError } = await client.auth.updateUser({
      password,
      data: { tktsync_password_setup_required: false },
    });
    setLoading(false);
    if (updateError) {
      setError(updateError.message);
      return false;
    }
    return true;
  };

  const metadata = session?.user.user_metadata ?? {};
  const displayName = String(
    metadata.full_name ??
      metadata.name ??
      metadata.display_name ??
      nameFromEmail(session?.user.email),
  ).trim();
  const requiresPasswordSetup = metadata.tktsync_password_setup_required === true;

  return {
    token: session?.access_token ?? '',
    authenticated: Boolean(session?.access_token),
    requiresPasswordSetup,
    userLabel: session?.user.email ?? session?.user.id ?? 'Operator',
    user: session?.user
      ? {
          id: session.user.id,
          email: session.user.email ?? '',
          displayName,
        }
      : null,
    loading,
    error,
    signIn,
    signOut,
    sendPasswordReset,
    updatePassword,
  };
}
