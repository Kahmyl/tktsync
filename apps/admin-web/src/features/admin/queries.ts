import { useRef } from 'react';
import { useMutation, useQuery, useQueryClient, type QueryKey } from '@tanstack/react-query';
import { useOperator } from '../../auth/OperatorSession';
import { AdminApiError, adminApi } from './api';

export const adminKeys = {
  dashboard: ['admin', 'dashboard'] as const,
  events: (query = '', state = '') => ['admin', 'events', query, state] as const,
  event: (id: string) => ['admin', 'event', id] as const,
  configuration: (id: string) => ['admin', 'event', id, 'configuration'] as const,
  inventory: (id: string) => ['admin', 'event', id, 'inventory'] as const,
  inventoryReport: (id: string) => ['admin', 'event', id, 'report', 'inventory'] as const,
  salesReport: (id: string) => ['admin', 'event', id, 'report', 'sales'] as const,
  admissionReport: (id: string) => ['admin', 'event', id, 'report', 'admission'] as const,
  audit: (id: string) => ['admin', 'event', id, 'audit'] as const,
  venues: ['admin', 'venues'] as const,
  venue: (id: string) => ['admin', 'venue', id] as const,
  layouts: (id: string) => ['admin', 'venue', id, 'layouts'] as const,
  partners: (query = '', state = '') => ['admin', 'partners', query, state] as const,
  partner: (id: string) => ['admin', 'partner', id] as const,
  tickets: (query = '', eventId = '', state = '') =>
    ['admin', 'tickets', query, eventId, state] as const,
  admissions: (eventId = '') => ['admin', 'admissions', eventId] as const,
  webhooks: ['admin', 'webhooks'] as const,
};

function useToken() {
  return useOperator().token;
}

export function useDashboard() {
  const token = useToken();
  return useQuery({ queryKey: adminKeys.dashboard, queryFn: () => adminApi.dashboard(token) });
}

export function useEvents(query = '', state = '') {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.events(query, state),
    queryFn: () => adminApi.events(token, query, state),
  });
}

export function useEvent(id: string) {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.event(id),
    queryFn: () => adminApi.event(token, id),
    enabled: Boolean(id),
  });
}

export function useEventWorkspace(id: string) {
  const token = useToken();
  return {
    configuration: useQuery({
      queryKey: adminKeys.configuration(id),
      queryFn: () => adminApi.eventConfiguration(token, id),
      enabled: Boolean(id),
    }),
    inventory: useQuery({
      queryKey: adminKeys.inventory(id),
      queryFn: () => adminApi.eventInventory(token, id),
      enabled: Boolean(id),
    }),
    inventoryReport: useQuery({
      queryKey: adminKeys.inventoryReport(id),
      queryFn: () => adminApi.inventoryReport(token, id),
      enabled: Boolean(id),
    }),
    salesReport: useQuery({
      queryKey: adminKeys.salesReport(id),
      queryFn: () => adminApi.salesReport(token, id),
      enabled: Boolean(id),
    }),
    admissionReport: useQuery({
      queryKey: adminKeys.admissionReport(id),
      queryFn: () => adminApi.admissionReport(token, id),
      enabled: Boolean(id),
    }),
    audit: useQuery({
      queryKey: adminKeys.audit(id),
      queryFn: () => adminApi.audit(token, id),
      enabled: Boolean(id),
    }),
  };
}

export function useEventReports(id: string) {
  const token = useToken();
  return {
    inventory: useQuery({
      queryKey: adminKeys.inventoryReport(id),
      queryFn: () => adminApi.inventoryReport(token, id),
      enabled: Boolean(id),
    }),
    sales: useQuery({
      queryKey: adminKeys.salesReport(id),
      queryFn: () => adminApi.salesReport(token, id),
      enabled: Boolean(id),
    }),
    admissions: useQuery({
      queryKey: adminKeys.admissionReport(id),
      queryFn: () => adminApi.admissionReport(token, id),
      enabled: Boolean(id),
    }),
  };
}

export function useVenues() {
  const token = useToken();
  return useQuery({ queryKey: adminKeys.venues, queryFn: () => adminApi.venues(token) });
}

export function useVenue(id: string) {
  const token = useToken();
  return {
    venue: useQuery({
      queryKey: adminKeys.venue(id),
      queryFn: () => adminApi.venue(token, id),
      enabled: Boolean(id),
    }),
    layouts: useQuery({
      queryKey: adminKeys.layouts(id),
      queryFn: () => adminApi.layouts(token, id),
      enabled: Boolean(id),
    }),
  };
}

export function usePartners(query = '', state = '') {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.partners(query, state),
    queryFn: () => adminApi.partners(token, query, state),
  });
}

export function usePartner(id: string) {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.partner(id),
    queryFn: () => adminApi.partner(token, id),
    enabled: Boolean(id),
  });
}

export function useTickets(query = '', eventId = '', state = '') {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.tickets(query, eventId, state),
    queryFn: () => adminApi.tickets(token, query, eventId, state),
  });
}

export function useAdmissions(eventId = '') {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.admissions(eventId),
    queryFn: () => adminApi.admissions(token, eventId),
  });
}

export function useAdmissionReport(eventId = '') {
  const token = useToken();
  return useQuery({
    queryKey: adminKeys.admissionReport(eventId),
    queryFn: () => adminApi.admissionReport(token, eventId),
    enabled: Boolean(eventId),
  });
}

export function useWebhooks() {
  const token = useToken();
  return useQuery({ queryKey: adminKeys.webhooks, queryFn: () => adminApi.webhooks(token) });
}

export function useIntentMutation<TVariables, TResult>(options: {
  intent: (variables: TVariables) => string;
  mutationFn: (token: string, idempotencyKey: string, variables: TVariables) => Promise<TResult>;
  invalidate?: QueryKey[];
}) {
  const token = useToken();
  const queryClient = useQueryClient();
  const keys = useRef(new Map<string, string>());
  return useMutation({
    retry: false,
    mutationFn: async (variables: TVariables) => {
      const intent = options.intent(variables);
      const key = keys.current.get(intent) ?? crypto.randomUUID();
      keys.current.set(intent, key);
      try {
        const data = await options.mutationFn(token, key, variables);
        keys.current.delete(intent);
        return data;
      } catch (error) {
        if (!(error instanceof AdminApiError) || !error.ambiguous) keys.current.delete(intent);
        throw error;
      }
    },
    onSuccess: async () => {
      await Promise.all(
        (options.invalidate ?? []).map((key) => queryClient.invalidateQueries({ queryKey: key })),
      );
    },
  });
}
