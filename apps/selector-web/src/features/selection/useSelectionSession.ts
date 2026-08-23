import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createTktSyncClient } from '@tktsync/api-client';
import { consumeCapability } from './capability';
import { clearIntentKey, getIntentKey } from './idempotency';
import { serverOffset } from './presentation';
import { runSelectionRealtime } from './realtime';
import type { Availability, EventView, Hold, Layout, SelectableOffer, Session } from './types';

export function useSelectionSession() {
  const [capability] = useState(() => consumeCapability(location, history));
  const [cacheInstance] = useState(() => crypto.randomUUID());
  const [selected, setSelected] = useState<SelectableOffer>();
  const [quantity, setQuantity] = useState(1);
  const [hold, setHold] = useState<Hold>();
  const [mutationError, setMutationError] = useState('');
  const intentKeys = useRef(new Map<string, string>());
  const [, tick] = useState(0);

  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);

  const headers = useMemo(
    () => ({
      Authorization: `Bearer ${capability}`,
    }),
    [capability],
  );

  const queryClient = useQueryClient();

  const bootstrapKey = useMemo(
    () => ['selection', 'bootstrap', cacheInstance] as const,
    [cacheInstance],
  );

  const bootstrap = useQuery({
    queryKey: bootstrapKey,
    enabled: Boolean(capability),
    refetchInterval: 30_000,
    queryFn: async ({ signal }) => {
      const [sessionResponse, eventResponse, layoutResponse, availabilityResponse] =
        await Promise.all([
          client.GET('/api/v1/selection/session', {
            headers,
            signal,
          }),
          client.GET('/api/v1/selection/event', {
            headers,
            signal,
          }),
          client.GET('/api/v1/selection/layout', {
            headers,
            signal,
          }),
          client.GET('/api/v1/selection/availability', {
            headers,
            signal,
          }),
        ]);

      if (
        sessionResponse.error ||
        eventResponse.error ||
        layoutResponse.error ||
        availabilityResponse.error
      ) {
        throw new Error('selection bootstrap rejected');
      }

      return {
        session: sessionResponse.data as Session,
        event: eventResponse.data as EventView,
        layout: layoutResponse.data as unknown as Layout,
        availability: availabilityResponse.data as Availability,
      };
    },
  });

  const session = bootstrap.data?.session;
  const event = bootstrap.data?.event;
  const layout = bootstrap.data?.layout;
  const availability = bootstrap.data?.availability;

  const serverOffsetMs = availability ? serverOffset(availability.server_time) : 0;

  const offers = useMemo<SelectableOffer[]>(() => {
    const reserved =
      availability?.reserved_units.flatMap((item) =>
        item.offer
          ? [
              {
                ...item.offer,
                kind: 'reserved' as const,
                inventory_id: item.inventory_id,
                section_id: item.section_id,
                row: item.row,
                seat: item.seat,
                label: `${item.row} · Seat ${item.seat}`,
              },
            ]
          : [],
      ) ?? [];

    const ga =
      availability?.ga_pools.flatMap((pool) =>
        pool.offers.map((offer) => ({
          ...offer,
          kind: 'ga' as const,
          inventory_id: pool.inventory_id,
          section_id: pool.section_id,
          label: pool.name,
        })),
      ) ?? [];

    return [...reserved, ...ga];
  }, [availability]);

  const refresh = async () => {
    await bootstrap.refetch();
  };

  const command = useMutation({
    mutationFn: async (execute: () => Promise<unknown>) => execute(),
  });

  useEffect(() => {
    const timer = window.setInterval(() => tick((value) => value + 1), 1000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    if (!capability || !session?.event_id) return;

    const controller = new AbortController();

    void runSelectionRealtime({
      baseUrl: import.meta.env.VITE_API_BASE_URL ?? '',
      eventID: session.event_id,
      capability,
      signal: controller.signal,
      onInvalidate: () => {
        void queryClient.invalidateQueries({
          queryKey: bootstrapKey,
        });
      },
    });

    return () => controller.abort();
  }, [bootstrapKey, capability, queryClient, session?.event_id]);

  useEffect(() => {
    if (!selected) return;

    const current = offers.find((offer) => offer.offer_id === selected.offer_id);

    if (!current) {
      setSelected(undefined);
      setQuantity(1);
      return;
    }

    setSelected(current);

    if (current.kind === 'reserved') {
      setQuantity(1);
      return;
    }

    const maximum = Math.max(1, current.available_quantity ?? 1);
    setQuantity((value) => Math.min(Math.max(1, value), maximum));
  }, [offers, selected]);

  const choose = (offer: SelectableOffer) => {
    setSelected(offer);
    setQuantity(1);
  };

  const reserve = async () => {
    if (!selected) return;

    const requestedQuantity = selected.kind === 'reserved' ? 1 : quantity;

    const intent = `reserve:${selected.offer_id}:${requestedQuantity}`;
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
                quantity: requestedQuantity,
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
      await queryClient.invalidateQueries({
        queryKey: bootstrapKey,
      });
    } catch {
      setMutationError(
        'The result could not be confirmed. Retry the same selection; TktSync will reuse the same request identity.',
      );
    }
  };

  const add = async () => {
    if (!hold || !selected) return;

    const requestedQuantity = selected.kind === 'reserved' ? 1 : quantity;

    const intent = `modify:${hold.id}:${selected.offer_id}:${requestedQuantity}`;

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
                quantity: requestedQuantity,
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

      await queryClient.invalidateQueries({
        queryKey: bootstrapKey,
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
      setQuantity(1);

      await queryClient.invalidateQueries({
        queryKey: bootstrapKey,
      });
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

  return {
    event,
    layout,
    availability,
    selected,
    setSelected: choose,
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
