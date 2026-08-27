import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useMutation, useQuery } from '@tanstack/react-query';
import { createTktSyncClient } from '@tktsync/api-client';
import { isPhoneDevice, scannerGateLabel } from './device';
import { humanLabel, outcomePresentation, ticketLocation } from './outcome';
import type { RecentScan, ScannerEvent, ScanResult } from './types';

type Detector = { detect(source: CanvasImageSource): Promise<Array<{ rawValue: string }>> };
type DetectorConstructor = new (options: { formats: string[] }) => Detector;
type TorchCapabilities = MediaTrackCapabilities & { torch?: boolean };

export function useAuthorizedEvents(token: string) {
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);
  return useQuery({
    queryKey: ['scanner', 'authorized-events'],
    enabled: Boolean(token),
    retry: false,
    queryFn: async ({ signal }) => {
      const response = await client.GET('/api/v1/admission/events', {
        headers: { Authorization: `Bearer ${token}` },
        signal,
      });
      if (response.error) throw new Error('events unavailable');
      return (response.data as { items: ScannerEvent[] }).items;
    },
  });
}

export function useScanner(token: string, selectedEvent?: ScannerEvent) {
  const [phoneDevice] = useState(() => isPhoneDevice(navigator));
  const gateReference = scannerGateLabel(phoneDevice);
  const [manual, setManual] = useState('');
  const [result, setResult] = useState<ScanResult>();
  const [cannotVerify, setCannotVerify] = useState(false);
  const [cameraState, setCameraState] = useState<
    | 'idle'
    | 'requesting'
    | 'active'
    | 'denied'
    | 'unsupported'
    | 'phone-required'
    | 'rear-camera-missing'
  >(phoneDevice ? 'idle' : 'phone-required');
  const [cameraMessage, setCameraMessage] = useState('');
  const [recentScans, setRecentScans] = useState<RecentScan[]>([]);
  const [soundEnabled, setSoundEnabled] = useState(
    () => localStorage.getItem('tktsync.scanner.sound') !== 'off',
  );
  const [vibrationEnabled, setVibrationEnabled] = useState(
    () => localStorage.getItem('tktsync.scanner.vibration') !== 'off',
  );
  const [torchSupported, setTorchSupported] = useState(false);
  const [torchEnabled, setTorchEnabled] = useState(false);
  const video = useRef<HTMLVideoElement>(null);
  const stream = useRef<MediaStream | null>(null);
  const intentKeys = useRef(new Map<string, string>());
  const processing = useRef(false);
  const client = useMemo(() => createTktSyncClient(import.meta.env.VITE_API_BASE_URL ?? ''), []);

  const scan = useMutation({
    retry: false,
    mutationFn: ({ qr, key }: { qr: string; key: string }) =>
      client.POST('/api/v1/admission/scans', {
        params: {
          header: { 'Idempotency-Key': key, 'X-Request-ID': crypto.randomUUID() },
        },
        headers: { Authorization: `Bearer ${token}` },
        body: {
          event_id: selectedEvent?.id ?? '',
          credential: qr,
          gate_reference: gateReference,
        },
      }),
  });

  const feedback = useCallback(
    (next: ScanResult) => {
      const admitted = next.result === 'ADMITTED' || next.result === 'MANUAL_OVERRIDE_ADMITTED';
      if (vibrationEnabled && navigator.vibrate) navigator.vibrate(admitted ? 120 : [80, 60, 80]);
      if (!soundEnabled) return;
      try {
        const AudioContextClass = window.AudioContext;
        const context = new AudioContextClass();
        const oscillator = context.createOscillator();
        const gain = context.createGain();
        oscillator.frequency.value = admitted ? 880 : 220;
        gain.gain.value = 0.045;
        oscillator.connect(gain);
        gain.connect(context.destination);
        oscillator.start();
        oscillator.stop(context.currentTime + (admitted ? 0.12 : 0.2));
        oscillator.addEventListener('ended', () => void context.close());
      } catch {
        // Browser audio feedback is optional; the visual decision remains authoritative.
      }
    },
    [soundEnabled, vibrationEnabled],
  );

  const appendRecent = useCallback(
    (next: ScanResult) => {
      const presentation = outcomePresentation(
        next,
        false,
        humanLabel(selectedEvent?.name, 'this event'),
      );
      const tone: RecentScan['tone'] =
        presentation.tone === 'success'
          ? 'success'
          : presentation.tone === 'warning'
            ? 'warning'
            : 'danger';
      setRecentScans((current) =>
        [
          {
            id: crypto.randomUUID(),
            title: presentation.title,
            detail: ticketLocation(next) || presentation.description,
            tone,
            time: new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }),
          },
          ...current,
        ].slice(0, 10),
      );
    },
    [selectedEvent?.name],
  );

  const submit = useCallback(
    async (qr: string) => {
      const value = qr.trim();
      if (!token || !selectedEvent || !value || processing.current) return;
      processing.current = true;
      setCannotVerify(false);
      const intent = `${selectedEvent.id}:${value}`;
      const key = intentKeys.current.get(intent) ?? crypto.randomUUID();
      intentKeys.current.set(intent, key);
      try {
        const response = await scan.mutateAsync({ qr: value, key });
        if (response.error) {
          intentKeys.current.delete(intent);
          setResult(undefined);
          setCannotVerify(true);
          setRecentScans((current) =>
            [
              {
                id: crypto.randomUUID(),
                title: "Can't verify ticket",
                detail: 'No admission decision was made.',
                tone: 'danger' as const,
                time: new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }),
              },
              ...current,
            ].slice(0, 10),
          );
          return;
        }
        intentKeys.current.delete(intent);
        const next = response.data as unknown as ScanResult;
        setResult(next);
        setManual('');
        appendRecent(next);
        feedback(next);
      } catch {
        setResult(undefined);
        setCannotVerify(true);
        setRecentScans((current) =>
          [
            {
              id: crypto.randomUUID(),
              title: "Can't verify ticket",
              detail: 'No admission decision was made.',
              tone: 'danger' as const,
              time: new Date().toLocaleTimeString([], { hour: 'numeric', minute: '2-digit' }),
            },
            ...current,
          ].slice(0, 10),
        );
      } finally {
        processing.current = false;
      }
    },
    [appendRecent, feedback, scan, selectedEvent, token],
  );

  const startCamera = useCallback(async () => {
    if (!phoneDevice) {
      setCameraState('phone-required');
      setCameraMessage(
        'Open TktSync Scanner on a phone with a rear camera. You can enter a manual admission code on this device.',
      );
      return;
    }
    if (stream.current) {
      if (video.current) video.current.srcObject = stream.current;
      setCameraState('active');
      return;
    }
    if (!navigator.mediaDevices?.getUserMedia) {
      setCameraState('unsupported');
      setCameraMessage('Camera scanning is not available in this browser.');
      return;
    }
    setCameraState('requesting');
    setCameraMessage('');
    try {
      const nextStream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: { ideal: 'environment' } },
        audio: false,
      });
      const track = nextStream.getVideoTracks()[0];
      if (track?.getSettings?.().facingMode === 'user') {
        nextStream.getTracks().forEach((streamTrack) => streamTrack.stop());
        setCameraState('rear-camera-missing');
        setCameraMessage(
          'A rear camera was not found. Use a phone with a rear camera or enter the manual admission code.',
        );
        return;
      }
      stream.current = nextStream;
      if (video.current) video.current.srcObject = nextStream;
      const capabilities = track?.getCapabilities() as TorchCapabilities;
      setTorchSupported(Boolean(capabilities?.torch));
      setCameraState('active');
    } catch {
      setCameraState('denied');
      setCameraMessage('Camera access is off. Allow access or enter the manual admission code.');
    }
  }, [phoneDevice]);

  const stopCamera = useCallback(() => {
    stream.current?.getTracks().forEach((track) => track.stop());
    stream.current = null;
    if (video.current) video.current.srcObject = null;
    setCameraState(phoneDevice ? 'idle' : 'phone-required');
    setTorchEnabled(false);
    setTorchSupported(false);
  }, [phoneDevice]);

  const toggleTorch = useCallback(async () => {
    const track = stream.current?.getVideoTracks()[0];
    if (!track || !torchSupported) return;
    const next = !torchEnabled;
    try {
      await track.applyConstraints({ advanced: [{ torch: next } as MediaTrackConstraintSet] });
      setTorchEnabled(next);
    } catch {
      setTorchSupported(false);
      setTorchEnabled(false);
    }
  }, [torchEnabled, torchSupported]);

  useEffect(() => {
    if (cameraState !== 'active' || result || cannotVerify || scan.isPending) return;
    const Detector = (window as unknown as { BarcodeDetector?: DetectorConstructor })
      .BarcodeDetector;
    if (!Detector) {
      setCameraState('unsupported');
      setCameraMessage(
        'Automatic QR scanning is not available here. Enter the manual admission code.',
      );
      return;
    }
    const detector = new Detector({ formats: ['qr_code'] });
    let active = true;
    const detect = async () => {
      if (!active || !video.current || processing.current) return;
      try {
        const codes = await detector.detect(video.current);
        const raw = codes[0]?.rawValue;
        if (raw) {
          await submit(raw);
          return;
        }
      } catch {
        setCameraMessage(
          'The camera image could not be read. Try again or enter the code manually.',
        );
      }
      if (active) window.setTimeout(() => void detect(), 220);
    };
    void detect();
    return () => {
      active = false;
    };
  }, [cameraState, cannotVerify, result, scan.isPending, submit]);

  useEffect(() => () => stopCamera(), [stopCamera]);
  useEffect(() => {
    localStorage.setItem('tktsync.scanner.sound', soundEnabled ? 'on' : 'off');
  }, [soundEnabled]);
  useEffect(() => {
    localStorage.setItem('tktsync.scanner.vibration', vibrationEnabled ? 'on' : 'off');
  }, [vibrationEnabled]);

  const reset = () => {
    setResult(undefined);
    setCannotVerify(false);
    setManual('');
  };

  return {
    manual,
    setManual,
    result,
    cannotVerify,
    busy: scan.isPending,
    cameraState,
    cameraMessage,
    phoneDevice,
    video,
    recentScans,
    soundEnabled,
    setSoundEnabled,
    vibrationEnabled,
    setVibrationEnabled,
    torchSupported,
    torchEnabled,
    toggleTorch,
    submit,
    startCamera,
    stopCamera,
    reset,
  };
}
