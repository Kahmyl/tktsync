import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createTktSyncClient } from '@tktsync/api-client';
import { consumeCapability } from './capability';
import { clearIntentKey, getIntentKey } from './idempotency';
import { serverOffset } from './presentation';
import type { Availability, EventView, Hold, SelectableOffer, Session } from './types';

export function useSelectionSession() {
  const [capability] = useState(() => {
    return consumeCapability(location, history);
  });
  const [selected, setSelected] = useState<SelectableOffer>();
  const [quantity, setQuantity] = useState(1);
  const [hold, setHold] = useState<Hold>();
  const [mutationError, setMutationError] = useState('');
  const intentKeys = useRef(new Map<string, string>());
  const [, tick] = useState(0);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const headers = useMemo(() => ({ Authorization: `Bearer ${capability}` }), [capability]);
  const queryClient = useQueryClient();
  const bootstrapKey = useMemo(() => ['selection', capability, 'bootstrap'] as const, [capability]);
  const bootstrap = useQuery({
    queryKey: bootstrapKey,
    enabled: Boolean(capability),
    queryFn: async ({ signal }) => {
      const [sessionResponse, eventResponse, availabilityResponse] = await Promise.all([
        client.GET('/api/v1/selection/session', { headers, signal }),
        client.GET('/api/v1/selection/event', { headers, signal }),
        client.GET('/api/v1/selection/availability', { headers, signal }),
      ]);
      if (sessionResponse.error || eventResponse.error || availabilityResponse.error) {
        throw new Error('selection bootstrap rejected');
      }
      return {
        session: sessionResponse.data as Session,
        event: eventResponse.data as EventView,
        availability: availabilityResponse.data as Availability,
      };
    },
  });
  const session = bootstrap.data?.session;
  const event = bootstrap.data?.event;
  const availability = bootstrap.data?.availability;
  const serverOffsetMs = availability ? serverOffset(availability.server_time) : 0;
  const refresh = async () => {
    await bootstrap.refetch();
  };
  const command = useMutation({ mutationFn: async (execute: () => Promise<unknown>) => execute() });
  useEffect(() => {
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  const reserve = async () => {
    if (!selected) return;

    const intent = `reserve:${selected.offer_id}:${quantity}`;
    const key = getIntentKey(intentKeys.current, intent);

    setMutationError('');

    try {
      const response = (await command.mutateAsync(() =>
        client.POST('/api/v1/selection/reservations', {
          params: {
            header: {
              'Idempotency-Key': key,
              'X-Request-ID': crypto.randomUUID(),
            },
          },
          headers,
          body: {
            items: [
              {
                offer_id: selected.offer_id,
                quantity,
              },
            ],
          },
        }),
      )) as Awaited<ReturnType<typeof client.POST>>;

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setMutationError('That inventory could not be held. Availability has been refreshed.');
        await refresh();
        return;
      }

      clearIntentKey(intentKeys.current, intent);
      setHold(response.data as Hold);
    } catch {
      setMutationError(
        'The result could not be confirmed. Retry the same selection; TktSync will reuse the same request identity.',
      );
    }
  };

  const add = async () => {
    if (!hold || !selected) return;

    const intent = `modify:${hold.id}:${selected.offer_id}:${quantity}`;
    const key = getIntentKey(intentKeys.current, intent);

    setMutationError('');

    try {
      const response = (await command.mutateAsync(() =>
        client.PATCH('/api/v1/selection/reservations/{reservation_id}', {
          params: {
            path: {
              reservation_id: hold.id,
            },
            header: {
              'X-TktSync-Reservation-Token': hold.reservation_token,
              'Idempotency-Key': key,
            },
          },
          headers,
          body: {
            add_items: [
              {
                offer_id: selected.offer_id,
                quantity,
              },
            ],
          },
        }),
      )) as Awaited<ReturnType<typeof client.PATCH>>;

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setMutationError('The hold could not be changed.');
        return;
      }

      clearIntentKey(intentKeys.current, intent);

      setHold({
        ...(response.data as Hold),
        reservation_token: hold.reservation_token,
      });
    } catch {
      setMutationError(
        'The change result could not be confirmed. Retry the same change; TktSync will reuse the same request identity.',
      );
    }
  };

  const release = async () => {
    if (!hold) return;

    const intent = `release:${hold.id}`;
    const key = getIntentKey(intentKeys.current, intent);

    setMutationError('');

    try {
      const response = (await command.mutateAsync(() =>
        client.POST('/api/v1/selection/reservations/{reservation_id}/release', {
          params: {
            path: {
              reservation_id: hold.id,
            },
            header: {
              'X-TktSync-Reservation-Token': hold.reservation_token,
              'Idempotency-Key': key,
            },
          },
          headers,
          body: {},
        }),
      )) as Awaited<ReturnType<typeof client.POST>>;

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setMutationError('The hold could not be released.');
        return;
      }

      clearIntentKey(intentKeys.current, intent);

      setHold(undefined);
      setSelected(undefined);
      await queryClient.invalidateQueries({ queryKey: bootstrapKey });
    } catch {
      setMutationError(
        'The release result could not be confirmed. Retry release; TktSync will reuse the same request identity.',
      );
    }
  };

  const handoff = () => {
    if (!hold || !session) return;
    const form = document.createElement('form');
    form.method = 'POST';
    form.action = session.return_url;
    form.style.display = 'none';
    for (const [name, value] of Object.entries({
      reservation_id: hold.id,
      reservation_token: hold.reservation_token,
    })) {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.appendChild(input);
    }
    document.body.appendChild(form);
    form.submit();
  };
  const offers: SelectableOffer[] = [
    ...(availability?.reserved_units.flatMap((item) =>
      item.offer ? [{ ...item.offer, label: `${item.row} · Seat ${item.seat}` }] : [],
    ) ?? []),
    ...(availability?.ga_pools.flatMap((pool) =>
      pool.offers.map((offer) => ({ ...offer, label: pool.name })),
    ) ?? []),
  ];
  return {
    event,
    selected,
    setSelected,
    quantity,
    setQuantity,
    hold,
    error: !capability
      ? 'Open the secure selection link supplied by the ticket partner.'
      : bootstrap.isError
        ? 'This selection link is invalid, expired, or the event is not available.'
        : mutationError,
    busy: command.isPending,
    loading: bootstrap.isPending,
    refreshing: bootstrap.isFetching && !bootstrap.isPending,
    retry: refresh,
    serverOffsetMs,
    offers,
    reserve,
    add,
    release,
    handoff,
  };
}
