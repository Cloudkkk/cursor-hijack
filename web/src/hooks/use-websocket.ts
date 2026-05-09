'use client';

import { useEffect, useRef } from 'react';
import { WSClient } from '@/lib/ws-client';
import { Record } from '@/lib/types';

const WS_URL = process.env.NEXT_PUBLIC_WS_URL || 'ws://localhost:9090/ws/records';

export function useWebSocket(
  onBatchRecords: (records: Record[]) => void,
  onStatus: (connected: boolean) => void,
  onReconnect?: () => void
) {
  const onBatchRef = useRef(onBatchRecords);
  const onStatusRef = useRef(onStatus);
  const onReconnectRef = useRef(onReconnect);
  const clientRef = useRef<WSClient | null>(null);

  onBatchRef.current = onBatchRecords;
  onStatusRef.current = onStatus;
  onReconnectRef.current = onReconnect;

  useEffect(() => {
    const client = new WSClient(
      WS_URL,
      (records) => onBatchRef.current(records),
      (connected) => onStatusRef.current(connected),
      () => onReconnectRef.current?.()
    );
    clientRef.current = client;
    client.connect();

    return () => {
      client.disconnect();
    };
  }, []);

  return clientRef.current;
}
