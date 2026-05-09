import { Record } from './types';

export type WSBatchCallback = (records: Record[]) => void;
export type WSStatusCallback = (connected: boolean) => void;
export type WSReconnectCallback = () => void;

const BATCH_INTERVAL_MS = 200;

export class WSClient {
  private ws: WebSocket | null = null;
  private url: string;
  private onBatch: WSBatchCallback;
  private onStatus: WSStatusCallback;
  private onReconnect?: WSReconnectCallback;
  private reconnectTimer: NodeJS.Timeout | null = null;
  private reconnectDelay = 1000;
  private maxReconnectDelay = 30000;
  private wasConnected = false;
  private reconnectAttempts = 0;

  private buffer: Record[] = [];
  private flushTimer: number | null = null;

  constructor(
    url: string,
    onBatch: WSBatchCallback,
    onStatus: WSStatusCallback,
    onReconnect?: WSReconnectCallback
  ) {
    this.url = url;
    this.onBatch = onBatch;
    this.onStatus = onStatus;
    this.onReconnect = onReconnect;
  }

  connect() {
    if (this.ws?.readyState === WebSocket.OPEN) {
      return;
    }

    try {
      this.ws = new WebSocket(this.url);

      this.ws.onopen = () => {
        console.log('WebSocket connected');
        this.onStatus(true);

        if (this.wasConnected && this.onReconnect) {
          console.log('Reconnected - recovering data...');
          this.onReconnect();
        }

        this.wasConnected = true;
        this.reconnectDelay = 1000;
        this.reconnectAttempts = 0;
        this.startFlushLoop();
      };

      this.ws.onmessage = (event) => {
        try {
          const record = JSON.parse(event.data) as Record;
          this.buffer.push(record);
        } catch (e) {
          console.error('Failed to parse WebSocket message:', e);
        }
      };

      this.ws.onclose = () => {
        console.log('WebSocket disconnected');
        this.onStatus(false);
        this.stopFlushLoop();
        this.flush();
        this.scheduleReconnect();
      };

      this.ws.onerror = (error) => {
        console.error('WebSocket error:', error);
      };
    } catch (e) {
      console.error('Failed to create WebSocket:', e);
      this.scheduleReconnect();
    }
  }

  private startFlushLoop() {
    this.stopFlushLoop();
    const tick = () => {
      this.flush();
      this.flushTimer = window.setTimeout(tick, BATCH_INTERVAL_MS);
    };
    this.flushTimer = window.setTimeout(tick, BATCH_INTERVAL_MS);
  }

  private stopFlushLoop() {
    if (this.flushTimer !== null) {
      clearTimeout(this.flushTimer);
      this.flushTimer = null;
    }
  }

  private flush() {
    if (this.buffer.length === 0) return;
    const batch = this.buffer;
    this.buffer = [];
    this.onBatch(batch);
  }

  private scheduleReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
    }

    this.reconnectAttempts++;
    this.reconnectTimer = setTimeout(() => {
      console.log(`Reconnecting (attempt ${this.reconnectAttempts})...`);
      this.connect();
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
    }, this.reconnectDelay);
  }

  getReconnectAttempts(): number {
    return this.reconnectAttempts;
  }

  disconnect() {
    this.stopFlushLoop();
    this.flush();
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }
}
