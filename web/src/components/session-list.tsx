'use client';

import { memo, useRef, useCallback } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { SessionInfo, formatTimestamp } from '@/lib/types';
import { cn } from '@/lib/utils';
import { ArrowUp, ArrowDown, MessageSquareText } from 'lucide-react';

interface SessionListProps {
  sessions: SessionInfo[];
  selectedSession: string | null;
  onSelectSession: (sessionId: string | null) => void;
  onViewContext?: (sessionId: string) => void;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0';
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}K`;
  return `${(bytes / 1024 / 1024).toFixed(1)}M`;
}

function getPreviewText(grpcData: string | undefined, maxLen: number = 80): string | null {
  if (!grpcData) return null;
  
  try {
    const data = JSON.parse(grpcData);
    const previewFields: string[] = [];
    
    for (const [key, value] of Object.entries(data)) {
      if (value === null || value === undefined) continue;
      
      let valueStr: string;
      if (typeof value === 'string') {
        valueStr = value.length > 30 ? value.substring(0, 30) + '...' : value;
      } else if (typeof value === 'number' || typeof value === 'boolean') {
        valueStr = String(value);
      } else {
        valueStr = '{...}';
      }
      
      previewFields.push(`${key}: ${valueStr}`);
      
      if (previewFields.join(', ').length > maxLen) break;
    }
    
    const preview = previewFields.join(', ');
    return preview.length > maxLen ? preview.substring(0, maxLen) + '...' : preview;
  } catch {
    return grpcData.length > maxLen ? grpcData.substring(0, maxLen) + '...' : grpcData;
  }
}

const SessionItem = memo(function SessionItem({
  session,
  isSelected,
  onSelect,
  onViewContext,
}: {
  session: SessionInfo;
  isSelected: boolean;
  onSelect: () => void;
  onViewContext?: (sessionId: string) => void;
}) {
  const methodDisplay = session.grpc_method
    || (session.url ? session.url.split('/').pop() : null)
    || 'unknown';
  const serviceDisplay = session.grpc_service
    || (session.url ? session.url.split('/').slice(-2, -1)[0] : null)
    || session.host;

  return (
    <div
      className={cn(
        'w-full text-left px-3 py-2.5 rounded-md text-sm transition-colors border',
        isSelected
          ? 'bg-primary text-primary-foreground border-primary'
          : 'hover:bg-gray-50 dark:hover:bg-gray-800 border-gray-200 dark:border-gray-700'
      )}
    >
      <div
        className="cursor-pointer"
        role="button"
        tabIndex={0}
        onClick={onSelect}
        onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') onSelect(); }}
      >
        <div className="font-semibold truncate text-sm" title={methodDisplay}>
          {methodDisplay}
        </div>
        <div className={cn(
          'truncate text-xs mt-0.5',
          isSelected ? 'opacity-80' : 'text-gray-600 dark:text-gray-400'
        )} title={serviceDisplay}>
          {serviceDisplay}
        </div>
        <div className={cn(
          'mt-1.5 flex items-center gap-2 text-xs',
          isSelected ? 'opacity-90' : 'text-gray-600 dark:text-gray-400'
        )}>
          <span>{session.record_count} frames</span>
          <span>·</span>
          <span>{formatTimestamp(session.first_ts)}</span>
        </div>
        <div className="mt-1 flex items-center gap-3 text-xs font-medium">
          <span className={cn(
            'flex items-center gap-0.5',
            isSelected ? 'text-blue-200' : 'text-blue-600'
          )}>
            <ArrowUp className="w-3 h-3" />
            {formatBytes(session.request_size)}
          </span>
          <span className={cn(
            'flex items-center gap-0.5',
            isSelected ? 'text-green-200' : 'text-green-600'
          )}>
            <ArrowDown className="w-3 h-3" />
            {formatBytes(session.response_size)}
          </span>
        </div>
        <div className={cn(
          'mt-1 truncate text-xs',
          isSelected ? 'opacity-70' : 'text-gray-500 dark:text-gray-500'
        )} title={session.host}>
          {session.host}
        </div>
        {session.grpc_preview && (
          <div className={cn(
            'mt-2 px-2 py-1.5 rounded text-[11px] font-mono leading-relaxed',
            isSelected
              ? 'bg-white/10 text-white/80'
              : 'bg-gray-100 dark:bg-gray-800 text-gray-600 dark:text-gray-400'
          )}>
            <div className="line-clamp-2 break-all">
              {getPreviewText(session.grpc_preview, 100)}
            </div>
          </div>
        )}
      </div>
      {onViewContext && (
        <button
          type="button"
          className={cn(
            'mt-1.5 h-6 px-2 text-xs gap-1 inline-flex items-center rounded-md hover:bg-accent transition-colors',
            isSelected ? 'text-primary-foreground/80 hover:text-primary-foreground hover:bg-white/10' : 'text-muted-foreground hover:text-foreground'
          )}
          onClick={(e) => {
            e.stopPropagation();
            onViewContext(session.id);
          }}
        >
          <MessageSquareText className="w-3 h-3" />
          View Context
        </button>
      )}
    </div>
  );
});

export const SessionList = memo(function SessionList({
  sessions,
  selectedSession,
  onSelectSession,
  onViewContext,
}: SessionListProps) {
  const scrollRef = useRef<HTMLDivElement>(null);

  // +1 for the "All Calls" button at index 0
  const virtualizer = useVirtualizer({
    count: sessions.length + 1,
    getScrollElement: () => scrollRef.current,
    estimateSize: useCallback((index: number) => {
      if (index === 0) return 60;
      const session = sessions[index - 1];
      return session?.grpc_preview ? 200 : 160;
    }, [sessions]),
    overscan: 5,
    measureElement: useCallback((el: Element) => el.getBoundingClientRect().height, []),
  });

  return (
    <div className="flex flex-col h-full overflow-hidden border-r">
      <div className="p-3 border-b font-semibold text-sm bg-muted/50 flex-shrink-0">
        RPC Calls ({sessions.length})
      </div>
      <div ref={scrollRef} className="flex-1 min-h-0 overflow-y-auto">
        <div
          className="p-2 relative"
          style={{ height: virtualizer.getTotalSize() }}
        >
          {virtualizer.getVirtualItems().map((virtualItem) => {
            if (virtualItem.index === 0) {
              return (
                <div
                  key="all-calls"
                  ref={virtualizer.measureElement}
                  data-index={virtualItem.index}
                  className="absolute left-0 right-0 px-2 pb-1"
                  style={{
                    transform: `translateY(${virtualItem.start}px)`,
                  }}
                >
                  <button
                    onClick={() => onSelectSession(null)}
                    className={cn(
                      'w-full text-left px-3 py-2 rounded-md text-sm transition-colors border',
                      selectedSession === null
                        ? 'bg-primary text-primary-foreground border-primary'
                        : 'hover:bg-gray-50 dark:hover:bg-gray-800 border-gray-200 dark:border-gray-700'
                    )}
                  >
                    <div className="font-medium">All Calls</div>
                    <div className={cn(
                      'text-xs',
                      selectedSession === null ? 'opacity-80' : 'text-gray-600 dark:text-gray-400'
                    )}>
                      {sessions.reduce((acc, s) => acc + s.record_count, 0)} frames
                    </div>
                  </button>
                </div>
              );
            }

            const session = sessions[virtualItem.index - 1];
            return (
              <div
                key={session.id}
                ref={virtualizer.measureElement}
                data-index={virtualItem.index}
                className="absolute left-0 right-0 px-2 pb-1"
                style={{
                  transform: `translateY(${virtualItem.start}px)`,
                }}
              >
                <SessionItem
                  session={session}
                  isSelected={selectedSession === session.id}
                  onSelect={() => onSelectSession(session.id)}
                  onViewContext={onViewContext}
                />
              </div>
            );
          })}

          {sessions.length === 0 && (
            <div className="text-center text-gray-500 dark:text-gray-400 text-sm py-8">
              No RPC calls yet
            </div>
          )}
        </div>
      </div>
    </div>
  );
});
