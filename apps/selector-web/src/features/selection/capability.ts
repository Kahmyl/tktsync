export function consumeCapability(
  source: Pick<Location, 'hash' | 'pathname' | 'search'>,
  browserHistory: Pick<History, 'replaceState'>,
) {
  const value = source.hash.slice(1);
  if (value) browserHistory.replaceState(null, '', source.pathname + source.search);
  return value;
}
