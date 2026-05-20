'use client';

import { useCallback, useEffect } from 'react';
import { useRecords } from '@/hooks/use-records';
import { useWebSocket } from '@/hooks/use-websocket';
import { FilterSidebar } from '@/components/filter-sidebar';
import { SessionList } from '@/components/session-list';
import { RecordList } from '@/components/record-list';
import { DetailPanel } from '@/components/detail-panel';
import { FilterBar } from '@/components/filter-bar';
import { Badge } from '@/components/ui/badge';
import { ResizablePanels } from '@/components/resizable-panels';

export default function Home() {
  const {
    records,
    allRecords,
    sessions,
    allSessions,
    availableFilters,
    methodCounts,
    selectedSession,
    selectedRecord,
    selectedServices,
    selectedMethods,
    isConnected,
    isPaused,
    setSelectedSession,
    setSelectedRecord,
    setSelectedServices,
    setSelectedMethods,
    setIsConnected,
    setIsPaused,
    addRecord,
    fetchInitialRecords,
    recoverData,
    clearRecords,
  } = useRecords();

  // WebSocket connection with reconnect recovery
  const onRecord = useCallback(addRecord, [addRecord]);
  const onStatus = useCallback(setIsConnected, [setIsConnected]);
  const onReconnect = useCallback(recoverData, [recoverData]);
  useWebSocket(onRecord, onStatus, onReconnect);

  // Fetch initial records (recent 100)
  useEffect(() => {
    fetchInitialRecords();
  }, [fetchInitialRecords]);

  // Toggle service selection
  const handleServiceToggle = useCallback((service: string) => {
    setSelectedServices((prev) => {
      const next = new Set(prev);
      if (next.has(service)) {
        next.delete(service);
      } else {
        next.add(service);
      }
      return next;
    });
    // Clear method selection when toggling service
    setSelectedMethods(new Set());
  }, [setSelectedServices, setSelectedMethods]);

  // Toggle method selection
  const handleMethodToggle = useCallback((fullMethod: string) => {
    setSelectedMethods((prev) => {
      const next = new Set(prev);
      if (next.has(fullMethod)) {
        next.delete(fullMethod);
      } else {
        next.add(fullMethod);
      }
      return next;
    });
    // Clear service selection when selecting specific methods
    setSelectedServices(new Set());
  }, [setSelectedMethods, setSelectedServices]);

  // Clear all filters
  const handleClearFilters = useCallback(() => {
    setSelectedServices(new Set());
    setSelectedMethods(new Set());
  }, [setSelectedServices, setSelectedMethods]);

  // Handle session selection - auto select first gRPC record
  const handleSelectSession = useCallback((sessionId: string | null) => {
    setSelectedSession(sessionId);
    
    if (sessionId) {
      // Find records for this session
      const sessionRecords = allRecords.filter((r) => r.session === sessionId);
      
      // Find first gRPC record, or first record if no gRPC
      const firstGrpc = sessionRecords.find((r) => r.type === 'grpc');
      const firstRecord = firstGrpc || sessionRecords[0];
      
      if (firstRecord) {
        setSelectedRecord(firstRecord);
      }
    } else {
      // "All Calls" selected - clear record selection
      setSelectedRecord(null);
    }
  }, [allRecords, setSelectedSession, setSelectedRecord]);

  return (
    <div className="h-screen flex flex-col bg-background overflow-hidden">
      {/* Header */}
      <header className="border-b px-4 py-2 flex items-center justify-between bg-muted/30 flex-shrink-0">
        <div className="flex items-center gap-3">
          <h1 className="font-bold text-lg">gRPC Inspector</h1>
          <Badge 
            variant={isConnected ? 'default' : 'destructive'}
            className={!isConnected ? 'animate-pulse' : ''}
          >
            {isConnected ? 'Connected' : 'Reconnecting...'}
          </Badge>
          <span className="text-xs text-gray-600 dark:text-gray-400">
            {allSessions.length} calls
          </span>
        </div>
        <FilterBar
          isPaused={isPaused}
          onTogglePause={() => setIsPaused(!isPaused)}
          onClear={clearRecords}
        />
      </header>

      {/* Main content */}
      <ResizablePanels
        defaultSizes={[15, 22, 22, 41]}
        minSizes={[150, 200, 200, 300]}
        className="flex-1 min-h-0"
      >
        {/* Filter Sidebar - Services */}
        <FilterSidebar
          availableFilters={availableFilters}
          methodCounts={methodCounts}
          selectedServices={selectedServices}
          selectedMethods={selectedMethods}
          onServiceToggle={handleServiceToggle}
          onMethodToggle={handleMethodToggle}
          onClearFilters={handleClearFilters}
        />

        {/* RPC Calls List */}
        <SessionList
          sessions={sessions}
          selectedSession={selectedSession}
          onSelectSession={handleSelectSession}
        />

        {/* Record (frames) List */}
        <RecordList
          records={records}
          selectedRecord={selectedRecord}
          onSelectRecord={setSelectedRecord}
        />

        {/* Detail Panel */}
        <DetailPanel record={selectedRecord} />
      </ResizablePanels>
    </div>
  );
}
