import { createContext, useContext } from 'react';
import type { useOperatorSession } from './useOperatorSession';

export type OperatorSessionValue = ReturnType<typeof useOperatorSession>;

export const OperatorSessionContext = createContext<OperatorSessionValue | null>(null);

export function useOperator() {
  const session = useContext(OperatorSessionContext);
  if (!session) throw new Error('useOperator must be used within OperatorSessionProvider');
  return session;
}
