import { useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { createTktSyncClient } from '@tktsync/api-client';
import { consumeCapability } from './capability';
import { clearIntentKey, getIntentKey } from './idempotency';
import { serverOffset } from './presentation';
import { runSelectionRealtime } from './realtime';
import type {
  Availability,
  EventView,
  Hold,
  Layout,
  SelectableOffer,
  SelectionLine,
  Session,
} from './types';

export function useSelectionSession() {
  const [capability] = useState(() => consumeCapability(location, history));
  const [cacheInstance] = useState(() => crypto.randomUUID());
  const [selectedOffers, setSelectedOffers] = useState<SelectableOffer[]>([]);
  const [gaQuantities, setGaQuantities] = useState<Record<string, number>>({});
  const [hold, setHold] = useState<Hold>();
  const [heldLines, setHeldLines] = useState<SelectionLine[]>([]);
  const [mutationError, setMutationError] = useState('');
  const [availabilityNotice, setAvailabilityNotice] = useState('');
  const [handoffPending, setHandoffPending] = useState(false);
  const [now, setNow] = useState(Date.now());
  const intentKeys = useRef(new Map<string, string>());

  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const headers = useMemo(() => ({ Authorization: `Bearer ${capability}` }), [capability]);
  const queryClient = useQueryClient();
  const bootstrapKey = useMemo(
    () => ['selection', 'bootstrap', cacheInstance] as const,
    [cacheInstance],
  );

  const bootstrap = useQuery({
    queryKey: bootstrapKey,
    enabled: Boolean(capability),
    refetchInterval: 30_000,
    retry: false,
    queryFn: async ({ signal }) => {
      const [sessionResponse, eventResponse, layoutResponse, availabilityResponse] =
        await Promise.all([
          client.GET('/api/v1/selection/session', { headers, signal }),
          client.GET('/api/v1/selection/event', { headers, signal }),
          client.GET('/api/v1/selection/layout', { headers, signal }),
          client.GET('/api/v1/selection/availability', { headers, signal }),
        ]).catch(() => {
          throw new Error('network');
        });

      if (
        sessionResponse.error ||
        eventResponse.error ||
        layoutResponse.error ||
        availabilityResponse.error
      ) {
        const responses = [
          sessionResponse.response,
          eventResponse.response,
          layoutResponse.response,
          availabilityResponse.response,
        ];
        const invalid = responses.some((response) =>
          response ? [401, 403, 404, 410].includes(response.status) : false,
        );
        throw new Error(invalid ? 'invalid link' : 'network');
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

  const layoutByInventory = useMemo(
    () =>
      new Map(
        [...(layout?.reserved_units ?? []), ...(layout?.ga_pools ?? [])].map((item) => [
          item.inventory_id,
          item,
        ]),
      ),
    [layout],
  );

  const offers = useMemo<SelectableOffer[]>(() => {
    const reserved =
      availability?.reserved_units.flatMap((item) => {
        if (!item.offer) return [];
        const layoutItem = layoutByInventory.get(item.inventory_id);
        return [
          {
            ...item.offer,
            kind: 'reserved' as const,
            inventory_id: item.inventory_id,
            section_id: item.section_id,
            section_name: layoutItem?.section_name,
            row: item.row,
            seat: item.seat,
            label: layoutItem?.section_name ?? 'Reserved seating',
          },
        ];
      }) ?? [];

    const generalAdmission =
      availability?.ga_pools.flatMap((pool) => {
        const layoutItem = layoutByInventory.get(pool.inventory_id);
        return pool.offers
          .filter((offer) => (offer.available_quantity ?? 0) > 0)
          .map((offer) => ({
            ...offer,
            kind: 'ga' as const,
            inventory_id: pool.inventory_id,
            section_id: pool.section_id,
            section_name: layoutItem?.section_name,
            label: pool.name,
          }));
      }) ?? [];

    return [...reserved, ...generalAdmission];
  }, [availability, layoutByInventory]);

  const selectedLines = useMemo<SelectionLine[]>(
    () =>
      selectedOffers.flatMap((offer) => {
        const quantity = offer.kind === 'reserved' ? 1 : (gaQuantities[offer.offer_id] ?? 0);
        return quantity > 0 ? [{ offer, quantity }] : [];
      }),
    [gaQuantities, selectedOffers],
  );

  const refresh = async () => {
    await bootstrap.refetch();
  };

  const command = useMutation({
    retry: false,
    mutationFn: async (execute: () => Promise<unknown>) => execute(),
  });

  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
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
      onInvalidate: () => void queryClient.invalidateQueries({ queryKey: bootstrapKey }),
    });
    return () => controller.abort();
  }, [bootstrapKey, capability, queryClient, session?.event_id]);

  useEffect(() => {
    setSelectedOffers((selected) => {
      if (selected.length === 0) return selected;
      const currentByID = new Map(offers.map((offer) => [offer.offer_id, offer]));
      const next = selected.flatMap((offer) => {
        const current = currentByID.get(offer.offer_id);
        if (!current) {
          if (!hold) {
            setAvailabilityNotice(
              'That seat is no longer available. Choose another seat to continue.',
            );
          }
          return [];
        }
        return [current];
      });
      setGaQuantities((previous) => {
        const updated = { ...previous };
        for (const offer of next) {
          if (offer.kind !== 'ga') continue;
          updated[offer.offer_id] = Math.min(
            updated[offer.offer_id] ?? 1,
            offer.available_quantity ?? 0,
          );
        }
        return updated;
      });
      return next;
    });
  }, [hold, offers]);

  const toggleReserved = (offer: SelectableOffer) => {
    if (offer.kind !== 'reserved' || hold) return;
    setAvailabilityNotice('');
    setSelectedOffers((current) =>
      current.some((item) => item.offer_id === offer.offer_id)
        ? current.filter((item) => item.offer_id !== offer.offer_id)
        : [...current, offer],
    );
  };

  const setGAQuantity = (offer: SelectableOffer, requested: number) => {
    if (offer.kind !== 'ga' || hold) return;
    const next = Math.min(Math.max(0, Math.floor(requested)), offer.available_quantity ?? 0);
    setAvailabilityNotice('');
    setGaQuantities((current) => ({ ...current, [offer.offer_id]: next }));
    setSelectedOffers((current) => {
      const without = current.filter((item) => item.offer_id !== offer.offer_id);
      return next > 0 ? [...without, offer] : without;
    });
  };

  const reserve = async () => {
    if (selectedLines.length === 0) return;
    const items = selectedLines
      .map(({ offer, quantity }) => ({ offer_id: offer.offer_id, quantity }))
      .sort((a, b) => a.offer_id.localeCompare(b.offer_id));
    const intent = `reserve:${items.map((item) => `${item.offer_id}:${item.quantity}`).join('|')}`;
    const key = getIntentKey(intentKeys.current, intent);
    setMutationError('');

    try {
      const response = (await command.mutateAsync(() =>
        client.POST('/api/v1/selection/reservations', {
          params: {
            header: { 'Idempotency-Key': key, 'X-Request-ID': crypto.randomUUID() },
          },
          headers,
          body: { items },
        }),
      )) as Awaited<ReturnType<typeof client.POST>>;

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setMutationError('Those tickets could not be held. Please review the latest availability.');
        await refresh();
        return;
      }

      clearIntentKey(intentKeys.current, intent);
      setHeldLines(selectedLines);
      setHold(response.data as Hold);
      await queryClient.invalidateQueries({ queryKey: bootstrapKey });
    } catch {
      setMutationError("We couldn't confirm your reservation. Try again to check its status.");
    }
  };

  const holdExpiresAt = hold ? new Date(hold.hold_expires_at).getTime() : 0;
  const holdExpired = Boolean(hold && holdExpiresAt <= now + serverOffsetMs);
  const holdNearExpiry = Boolean(
    hold && !holdExpired && holdExpiresAt - (now + serverOffsetMs) <= 60_000,
  );

  const clearSelection = () => {
    setHold(undefined);
    setHeldLines([]);
    setSelectedOffers([]);
    setGaQuantities({});
    setMutationError('');
    setAvailabilityNotice('');
  };

  const release = async () => {
    if (!hold) return;
    if (holdExpired) {
      clearSelection();
      await refresh();
      return;
    }
    const intent = `release:${hold.id}`;
    const key = getIntentKey(intentKeys.current, intent);
    setMutationError('');
    try {
      const response = (await command.mutateAsync(() =>
        client.POST('/api/v1/selection/reservations/{reservation_id}/release', {
          params: {
            path: { reservation_id: hold.id },
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
        setMutationError("We couldn't change this reservation. Please try again.");
        return;
      }
      clearIntentKey(intentKeys.current, intent);
      clearSelection();
      await queryClient.invalidateQueries({ queryKey: bootstrapKey });
    } catch {
      setMutationError("We couldn't confirm the change. Try again to check its status.");
    }
  };

  const handoff = () => {
    if (!hold || !session || holdExpired) return;
    setHandoffPending(true);
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

  const loadError = bootstrap.error instanceof Error ? bootstrap.error.message : '';
  const invalidLink = !capability || loadError === 'invalid link';
  const networkError = loadError === 'network';
  const error = invalidLink ? 'This ticket selection link is no longer available.' : mutationError;

  return {
    event,
    layout,
    availability,
    offers,
    selectedLines,
    hold,
    heldLines,
    error,
    availabilityNotice,
    busy: command.isPending,
    loading: Boolean(capability) && bootstrap.isPending,
    refreshing: bootstrap.isFetching && !bootstrap.isPending,
    invalidLink,
    networkError,
    eventUnavailable: Boolean(event && event.state !== 'ON_SALE'),
    serverOffsetMs,
    now,
    holdExpired,
    holdNearExpiry,
    handoffPending,
    retry: refresh,
    toggleReserved,
    setGAQuantity,
    reserve,
    release,
    handoff,
  };
}
