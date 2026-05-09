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
  const clientRef = useRef<WSClient | null>(null);

  useEffect(() => {
    const client = new WSClient(WS_URL, onBatchRecords, onStatus, onReconnect);
    clientRef.current = client;
    client.connect();

    return () => {
      client.disconnect();
    };
  }, [onBatchRecords, onStatus, onReconnect]);

  return clientRef.current;
}
