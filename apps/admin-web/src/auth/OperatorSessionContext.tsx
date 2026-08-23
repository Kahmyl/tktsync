import type { ReactNode } from 'react';
import { OperatorSessionContext } from './OperatorSession';
import { useOperatorSession } from './useOperatorSession';

export function OperatorSessionProvider({ children }: { children: ReactNode }) {
  const session = useOperatorSession();
  return (
    <OperatorSessionContext.Provider value={session}>{children}</OperatorSessionContext.Provider>
  );
}
