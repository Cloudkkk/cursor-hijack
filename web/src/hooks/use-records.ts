'use client';

import { useState, useCallback, useMemo, useRef } from 'react';
import { Record, SessionInfo } from '@/lib/types';

const API_BASE = '';
const MAX_RECORDS = 10000;

function recordKey(r: Record): string {
  return `${r.session}-${r.index}`;
}

export function useRecords() {
  const [records, setRecords] = useState<Record[]>([]);
  const [selectedSession, setSelectedSession] = useState<string | null>(null);
  const [selectedRecordKey, setSelectedRecordKey] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const [isPaused, setIsPaused] = useState(false);
  const [searchQuery, setSearchQuery] = useState('');
  const [initialized, setInitialized] = useState(false);
  const [selectedServices, setSelectedServices] = useState<Set<string>>(new Set());
  const [selectedMethods, setSelectedMethods] = useState<Set<string>>(new Set());
  
  const recordsMap = useRef(new Map<string, Record>());

  // Trim to MAX_RECORDS and keep recordsMap in sync
  const trimRecords = useCallback((recs: Record[]): Record[] => {
    if (recs.length <= MAX_RECORDS) return recs;
    const removed = recs.slice(0, recs.length - MAX_RECORDS);
    for (const r of removed) {
      recordsMap.current.delete(recordKey(r));
    }
    return recs.slice(-MAX_RECORDS);
  }, []);

  // Batch add records from WebSocket (O(1) dedup per record via Map)
  const addRecords = useCallback((batch: Record[]) => {
    if (isPaused || batch.length === 0) return;

    const newOnes: Record[] = [];
    for (const r of batch) {
      const key = recordKey(r);
      if (!recordsMap.current.has(key)) {
        recordsMap.current.set(key, r);
        newOnes.push(r);
      }
    }

    if (newOnes.length === 0) return;

    setRecords((prev) => trimRecords([...prev, ...newOnes]));
  }, [isPaused, trimRecords]);

  const selectedRecord = useMemo(() => {
    if (!selectedRecordKey) return null;
    return recordsMap.current.get(selectedRecordKey) || null;
  }, [selectedRecordKey]);

  const setSelectedRecord = useCallback((record: Record | null) => {
    if (!record) {
      setSelectedRecordKey(null);
    } else {
      const key = recordKey(record);
      recordsMap.current.set(key, record);
      setSelectedRecordKey(key);
    }
  }, []);

  const sessions = useMemo(() => {
    const sessionMap = new Map<string, SessionInfo>();
    
    for (const record of records) {
      const existing = sessionMap.get(record.session);
      if (existing) {
        existing.record_count++;
        if (record.ts > existing.last_ts) {
          existing.last_ts = record.ts;
        }
        if (record.ts < existing.first_ts) {
          existing.first_ts = record.ts;
        }
        if (record.grpc_service && !existing.grpc_service) {
          existing.grpc_service = record.grpc_service;
          existing.grpc_method = record.grpc_method;
        }
        if (record.type === 'request' && record.url && !existing.url) {
          existing.url = record.url;
        }
        if (record.direction === 'C2S' && record.size) {
          existing.request_size += record.size;
        }
        if (record.direction === 'S2C' && record.size) {
          existing.response_size += record.size;
        }
        if (record.type === 'grpc' && record.direction === 'C2S' && record.grpc_data && !existing.grpc_preview) {
          existing.grpc_preview = record.grpc_data;
        }
      } else {
        sessionMap.set(record.session, {
          id: record.session,
          seq: record.seq,
          host: record.host || '',
          record_count: 1,
          first_ts: record.ts,
          last_ts: record.ts,
          grpc_service: record.grpc_service,
          grpc_method: record.grpc_method,
          url: record.type === 'request' ? record.url : undefined,
          request_size: record.direction === 'C2S' ? (record.size || 0) : 0,
          response_size: record.direction === 'S2C' ? (record.size || 0) : 0,
          grpc_preview: record.type === 'grpc' && record.direction === 'C2S' ? record.grpc_data : undefined,
        });
      }
    }

    return Array.from(sessionMap.values()).sort((a, b) => b.seq - a.seq);
  }, [records]);

  const availableFilters = useMemo(() => {
    const services = new Map<string, Set<string>>();
    
    for (const session of sessions) {
      if (session.grpc_service) {
        if (!services.has(session.grpc_service)) {
          services.set(session.grpc_service, new Set());
        }
        if (session.grpc_method) {
          services.get(session.grpc_service)!.add(session.grpc_method);
        }
      }
    }
    
    return services;
  }, [sessions]);

  const methodCounts = useMemo(() => {
    const counts = new Map<string, number>();
    
    for (const session of sessions) {
      if (session.grpc_service && session.grpc_method) {
        const key = `${session.grpc_service}.${session.grpc_method}`;
        counts.set(key, (counts.get(key) || 0) + 1);
      }
    }
    
    return counts;
  }, [sessions]);

  const filteredSessions = useMemo(() => {
    if (selectedServices.size === 0 && selectedMethods.size === 0) {
      return sessions;
    }
    
    return sessions.filter((s) => {
      if (!s.grpc_service) return false;
      
      if (selectedMethods.size > 0) {
        const fullMethod = `${s.grpc_service}.${s.grpc_method}`;
        return selectedMethods.has(fullMethod);
      }
      
      if (selectedServices.size > 0) {
        return selectedServices.has(s.grpc_service);
      }
      
      return true;
    });
  }, [sessions, selectedServices, selectedMethods]);

  const filteredRecords = useMemo(() => {
    let result = records;
    
    if (selectedSession) {
      result = result.filter((r) => r.session === selectedSession);
    }
    
    if (searchQuery) {
      const query = searchQuery.toLowerCase();
      result = result.filter((r) => {
        return (
          r.url?.toLowerCase().includes(query) ||
          r.grpc_service?.toLowerCase().includes(query) ||
          r.grpc_method?.toLowerCase().includes(query) ||
          r.grpc_data?.toLowerCase().includes(query) ||
          r.body?.toLowerCase().includes(query) ||
          r.host?.toLowerCase().includes(query)
        );
      });
    }
    
    return result;
  }, [records, selectedSession, searchQuery]);

  // Fetch and merge records from API (with dedup + cap)
  const fetchAndMergeRecords = useCallback(async () => {
    try {
      const res = await fetch(`${API_BASE}/api/records?limit=100`);
      const data = await res.json();
      if (Array.isArray(data) && data.length > 0) {
        const incoming = data as Record[];
        const newOnes: Record[] = [];

        for (const r of incoming) {
          const key = recordKey(r);
          if (!recordsMap.current.has(key)) {
            recordsMap.current.set(key, r);
            newOnes.push(r);
          }
        }

        if (newOnes.length === 0) return;

        setRecords((prev) => {
          const merged = [...prev, ...newOnes].sort((a, b) => {
            if (a.seq !== b.seq) return a.seq - b.seq;
            return a.index - b.index;
          });
          return trimRecords(merged);
        });
      }
    } catch (e) {
      console.error('Failed to fetch records:', e);
    }
  }, [trimRecords]);

  const fetchInitialRecords = useCallback(async () => {
    if (initialized) return;
    await fetchAndMergeRecords();
    setInitialized(true);
  }, [initialized, fetchAndMergeRecords]);

  const recoverData = useCallback(async () => {
    console.log('Recovering data after reconnect...');
    await fetchAndMergeRecords();
  }, [fetchAndMergeRecords]);

  const clearRecords = useCallback(() => {
    setRecords([]);
    setSelectedSession(null);
    setSelectedRecordKey(null);
    recordsMap.current.clear();
  }, []);

  return {
    records: filteredRecords,
    allRecords: records,
    sessions: filteredSessions,
    allSessions: sessions,
    availableFilters,
    methodCounts,
    selectedSession,
    selectedRecord,
    selectedServices,
    selectedMethods,
    isConnected,
    isPaused,
    searchQuery,
    setSelectedSession,
    setSelectedRecord,
    setSelectedServices,
    setSelectedMethods,
    setIsConnected,
    setIsPaused,
    setSearchQuery,
    addRecords,
    fetchInitialRecords,
    recoverData,
    clearRecords,
  };
}
