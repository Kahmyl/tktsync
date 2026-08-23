export function getIntentKey(
  store: Map<string, string>,
  intent: string,
  factory: () => string = () => crypto.randomUUID(),
) {
  const existing = store.get(intent);
  if (existing) return existing;

  const created = factory();
  store.set(intent, created);
  return created;
}

export function clearIntentKey(store: Map<string, string>, intent: string) {
  store.delete(intent);
}
