export async function demoAPI<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`/demo-api${path}`, {
    ...options,
    headers: { 'content-type': 'application/json', ...options?.headers },
  });
  const data = await response.json();
  if (!response.ok)
    throw new Error(data?.error?.message || 'The ticket service is temporarily unavailable.');
  return data as T;
}
