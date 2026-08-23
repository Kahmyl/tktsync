import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation } from '@tanstack/react-query';
import { createTktSyncClient } from '@tktsync/api-client';
import type { ScanResult } from './types';

type Detector = {
  detect(source: CanvasImageSource): Promise<Array<{ rawValue: string }>>;
};

type DetectorConstructor = new (options: { formats: string[] }) => Detector;

export function useScanner(token: string) {
  const [eventID, setEventID] = useState(
    () => sessionStorage.getItem('tktsync.scanner.event') ?? '',
  );

  const [deviceID] = useState(() => {
    const existing = sessionStorage.getItem('tktsync.scanner.device');
    const id = existing ?? crypto.randomUUID();
    sessionStorage.setItem('tktsync.scanner.device', id);
    return id;
  });

  const [manual, setManual] = useState('');
  const [result, setResult] = useState<ScanResult>();
  const [error, setError] = useState('');
  const [camera, setCamera] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const intentKeys = useRef(new Map<string, string>());

  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);

  const scan = useMutation({
    retry: false,
    mutationFn: ({ qr, key }: { qr: string; key: string }) =>
      client.POST('/api/v1/admission/scans', {
        params: {
          header: {
            'Idempotency-Key': key,
            'X-Request-ID': crypto.randomUUID(),
          },
        },
        headers: {
          Authorization: `Bearer ${token}`,
        },
        body: {
          event_id: eventID,
          credential: qr,
          gate_reference: deviceID,
        },
      }),
  });

  const busy = scan.isPending;

  const submit = useCallback(
    async (qr: string) => {
      if (!token || !eventID || !qr.trim() || busy) return;

      setError('');
      sessionStorage.setItem('tktsync.scanner.event', eventID);

      const intent = `${eventID}:${qr.trim()}`;
      const key = intentKeys.current.get(intent) ?? crypto.randomUUID();
      intentKeys.current.set(intent, key);

      try {
        const response = await scan.mutateAsync({
          qr: qr.trim(),
          key,
        });

        if (response.error) {
          intentKeys.current.delete(intent);

          const failure = response.error as {
            error?: {
              code?: string;
              message?: string;
            };
          };

          setResult(undefined);
          setError(
            failure.error?.code === 'AUTHORITY_TEMPORARILY_UNAVAILABLE'
              ? 'Central authority could not be reached. Do not admit.'
              : (failure.error?.message ?? 'Scan was rejected.'),
          );
          return;
        }

        intentKeys.current.delete(intent);
        setResult(response.data as unknown as ScanResult);
        setManual('');
      } catch {
        setResult(undefined);
        setError(
          'Central authority could not be reached. Do not admit. Retry uses the same protected intent.',
        );
      }
    },
    [busy, eventID, scan, token],
  );

  const startCamera = async () => {
    try {
      stream.current = await navigator.mediaDevices.getUserMedia({
        video: {
          facingMode: {
            ideal: 'environment',
          },
        },
        audio: false,
      });

      if (video.current) {
        video.current.srcObject = stream.current;
      }

      setCamera(true);
    } catch {
      setError('Camera access was denied. Use manual scan entry.');
    }
  };

  useEffect(() => {
    if (!camera) return;

    const Detector = (window as unknown as { BarcodeDetector?: DetectorConstructor })
      .BarcodeDetector;

    if (!Detector) {
      setError('Barcode scanning is unavailable in this browser. Use manual scan entry.');
      return;
    }

    const detector = new Detector({
      formats: ['qr_code'],
    });

    let active = true;

    const detect = async () => {
      if (!active || !video.current) return;

      try {
        const codes = await detector.detect(video.current);
        const raw = codes[0]?.rawValue;

        if (raw) {
          await submit(raw);
          setCamera(false);
          stream.current?.getTracks().forEach((track) => track.stop());
          return;
        }
      } catch {
        setError('The camera image could not be read.');
      }

      if (active) {
        window.setTimeout(() => void detect(), 250);
      }
    };

    void detect();

    return () => {
      active = false;
    };
  }, [camera, submit]);

  useEffect(
    () => () => {
      stream.current?.getTracks().forEach((track) => track.stop());
    },
    [],
  );

  const reset = () => {
    setResult(undefined);
    setError('');
    setManual('');
  };

  return {
    eventID,
    setEventID,
    deviceID,
    manual,
    setManual,
    result,
    error,
    busy,
    camera,
    video,
    submit,
    startCamera,
    reset,
  };
}
