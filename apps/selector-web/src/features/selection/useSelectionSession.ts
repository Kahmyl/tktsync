import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createTktSyncClient } from '@tktsync/api-client';
import { consumeCapability } from './capability';
import { clearIntentKey, getIntentKey } from './idempotency';
import { serverOffset } from './presentation';
import type { Availability, EventView, Hold, SelectableOffer, Session } from './types';

export function useSelectionSession() {
  const [capability] = useState(() => {
    return consumeCapability(location, history);
  });
  const [session, setSession] = useState<Session>();
  const [event, setEvent] = useState<EventView>();
  const [availability, setAvailability] = useState<Availability>();
  const [selected, setSelected] = useState<SelectableOffer>();
  const [quantity, setQuantity] = useState(1);
  const [hold, setHold] = useState<Hold>();
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [serverOffsetMs, setServerOffsetMs] = useState(0);
  const intentKeys = useRef(new Map<string, string>());
  const [, tick] = useState(0);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const headers = useMemo(() => ({ Authorization: `Bearer ${capability}` }), [capability]);
  const refresh = useCallback(async () => {
    setError('');
    const [s, e, a] = await Promise.all([
      client.GET('/api/v1/selection/session', { headers }),
      client.GET('/api/v1/selection/event', { headers }),
      client.GET('/api/v1/selection/availability', { headers }),
    ]);
    if (s.error || e.error || a.error) {
      setError('This selection link is invalid, expired, or the event is not available.');
      return;
    }
    const nextAvailability = a.data as Availability;

    setSession(s.data as Session);
    setEvent(e.data as EventView);
    setAvailability(nextAvailability);
    setServerOffsetMs(serverOffset(nextAvailability.server_time));
  }, [client, headers]);
  useEffect(() => {
    if (capability) void refresh();
    else setError('Open the secure selection link supplied by the ticket partner.');
  }, [capability, refresh]);
  useEffect(() => {
    const timer = setInterval(() => tick((v) => v + 1), 1000);
    return () => clearInterval(timer);
  }, []);
  const reserve = async () => {
    if (!selected) return;

    const intent = `reserve:${selected.offer_id}:${quantity}`;
    const key = getIntentKey(intentKeys.current, intent);

    setBusy(true);
    setError('');

    try {
      const response = await client.POST('/api/v1/selection/reservations', {
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
      });

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setError('That inventory could not be held. Availability has been refreshed.');
        await refresh();
        return;
      }

      clearIntentKey(intentKeys.current, intent);
      setHold(response.data as Hold);
    } catch {
      setError(
        'The result could not be confirmed. Retry the same selection; TktSync will reuse the same request identity.',
      );
    } finally {
      setBusy(false);
    }
  };

  const add = async () => {
    if (!hold || !selected) return;

    const intent = `modify:${hold.id}:${selected.offer_id}:${quantity}`;
    const key = getIntentKey(intentKeys.current, intent);

    setBusy(true);
    setError('');

    try {
      const response = await client.PATCH('/api/v1/selection/reservations/{reservation_id}', {
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
      });

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setError('The hold could not be changed.');
        return;
      }

      clearIntentKey(intentKeys.current, intent);

      setHold({
        ...(response.data as Hold),
        reservation_token: hold.reservation_token,
      });
    } catch {
      setError(
        'The change result could not be confirmed. Retry the same change; TktSync will reuse the same request identity.',
      );
    } finally {
      setBusy(false);
    }
  };

  const release = async () => {
    if (!hold) return;

    const intent = `release:${hold.id}`;
    const key = getIntentKey(intentKeys.current, intent);

    setBusy(true);
    setError('');

    try {
      const response = await client.POST(
        '/api/v1/selection/reservations/{reservation_id}/release',
        {
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
        },
      );

      if (response.error) {
        clearIntentKey(intentKeys.current, intent);
        setError('The hold could not be released.');
        return;
      }

      clearIntentKey(intentKeys.current, intent);

      setHold(undefined);
      setSelected(undefined);
      await refresh();
    } catch {
      setError(
        'The release result could not be confirmed. Retry release; TktSync will reuse the same request identity.',
      );
    } finally {
      setBusy(false);
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
    error,
    busy,
    serverOffsetMs,
    offers,
    reserve,
    add,
    release,
    handoff,
  };
}
