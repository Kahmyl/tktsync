import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createTktSyncClient } from '@tktsync/api-client';
import type { ScanResult } from './types';

type Detector = { detect(source: CanvasImageSource): Promise<Array<{ rawValue: string }>> };
type DetectorConstructor = new (options: { formats: string[] }) => Detector;

export function useScanner() {
  const [token, setToken] = useState(() => sessionStorage.getItem('tktsync.scanner.token') ?? '');
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
  const [busy, setBusy] = useState(false);
  const [camera, setCamera] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  const submit = useCallback(
    async (qr: string) => {
      if (!qr.trim() || busy) return;
      setBusy(true);
      setError('');
      sessionStorage.setItem('tktsync.scanner.token', token);
      sessionStorage.setItem('tktsync.scanner.event', eventID);
      try {
        const response = await client.POST('/api/v1/admission/scans', {
          params: {
            header: {
              'Idempotency-Key': crypto.randomUUID(),
              'X-Request-ID': crypto.randomUUID(),
            },
          },
          headers: {
            Authorization: `Bearer ${token}`,
          },
          body: { event_id: eventID, credential: qr.trim(), gate_reference: deviceID },
        });
        if (response.error) {
          const failure = response.error as { error?: { code?: string; message?: string } };
          setResult(undefined);
          setError(
            failure.error?.code === 'AUTHORITY_TEMPORARILY_UNAVAILABLE'
              ? 'Central authority could not be reached. Do not admit.'
              : (failure.error?.message ?? 'Scan was rejected.'),
          );
          return;
        }
        setResult(response.data as unknown as ScanResult);
        setManual('');
      } catch {
        setResult(undefined);
        setError('Central authority could not be reached. Do not admit.');
      } finally {
        setBusy(false);
      }
    },
    [busy, client, deviceID, eventID, token],
  );
  const startCamera = async () => {
    try {
      stream.current = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' } },
        audio: false,
      });
      if (video.current) video.current.srcObject = stream.current;
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
    const detector = new Detector({ formats: ['qr_code'] });
    let active = true;
    const scan = async () => {
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
      if (active) setTimeout(() => void scan(), 250);
    };
    void scan();
    return () => {
      active = false;
    };
  }, [camera, submit]);
  useEffect(() => () => stream.current?.getTracks().forEach((track) => track.stop()), []);
  const reset = () => {
    setResult(undefined);
    setError('');
    setManual('');
  };
  return {
    token,
    setToken,
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
