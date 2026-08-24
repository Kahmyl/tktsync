type RealtimeOptions = {
  baseUrl: string;
  eventID: string;
  capability: string;
  signal: AbortSignal;
  onInvalidate: (event: string, data: unknown) => void;
};

const wait = (milliseconds: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    if (signal.aborted) {
      resolve();
      return;
    }

    const timer = window.setTimeout(resolve, milliseconds);

    signal.addEventListener(
      'abort',
      () => {
        window.clearTimeout(timer);
        resolve();
      },
      { once: true },
    );
  });

async function consume(
  response: Response,
  signal: AbortSignal,
  onInvalidate: RealtimeOptions['onInvalidate'],
) {
  if (!response.body) {
    throw new Error('Realtime response has no readable body');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();

  let buffer = '';
  let eventName = '';
  let dataLines: string[] = [];

  const dispatch = () => {
    if (eventName !== 'invalidate' && eventName !== 'resync') {
      eventName = '';
      dataLines = [];
      return;
    }

    const raw = dataLines.join('\n');
    let data: unknown;

    try {
      data = raw ? JSON.parse(raw) : undefined;
    } catch {
      data = raw;
    }

    onInvalidate(eventName, data);

    eventName = '';
    dataLines = [];
  };

  while (!signal.aborted) {
    const { done, value } = await reader.read();

    if (done) break;

    buffer += decoder.decode(value, { stream: true });

    for (;;) {
      const newline = buffer.indexOf('\n');

      if (newline < 0) break;

      let line = buffer.slice(0, newline);
      buffer = buffer.slice(newline + 1);

      if (line.endsWith('\r')) {
        line = line.slice(0, -1);
      }

      if (!line) {
        dispatch();
        continue;
      }

      if (line.startsWith(':')) continue;

      if (line.startsWith('event:')) {
        eventName = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart());
      }
    }
  }
}

export async function runSelectionRealtime({
  baseUrl,
  eventID,
  capability,
  signal,
  onInvalidate,
}: RealtimeOptions) {
  let retryDelay = 500;

  while (!signal.aborted) {
    try {
      const url = new URL('/api/v1/realtime/stream', baseUrl || window.location.origin);

      url.searchParams.set('audience', 'selection');
      url.searchParams.set('event_id', eventID);

      const response = await fetch(url, {
        method: 'GET',
        headers: {
          Accept: 'text/event-stream',
          Authorization: `Bearer ${capability}`,
          'X-Request-ID': crypto.randomUUID(),
        },
        cache: 'no-store',
        signal,
      });

      if (response.status === 401 || response.status === 403) {
        return;
      }

      if (!response.ok) {
        throw new Error(`Realtime rejected with HTTP ${response.status}`);
      }

      retryDelay = 500;

      await consume(response, signal, onInvalidate);
    } catch {
      if (signal.aborted) return;
    }

    await wait(retryDelay, signal);
    retryDelay = Math.min(retryDelay * 2, 10_000);
  }
}
