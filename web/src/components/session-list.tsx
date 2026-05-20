'use client';

import { memo } from 'react';
import { SessionInfo, formatTimestamp } from '@/lib/types';
import { ScrollArea } from '@/components/ui/scroll-area';
import { cn } from '@/lib/utils';
import { ArrowUp, ArrowDown } from 'lucide-react';

interface SessionListProps {
  sessions: SessionInfo[];
  selectedSession: string | null;
  onSelectSession: (sessionId: string | null) => void;
}

// Format bytes to human readable
function formatBytes(bytes: number): string {
  if (bytes === 0) return '0';
  if (bytes < 1024) return `${bytes}B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)}K`;
  return `${(bytes / 1024 / 1024).toFixed(1)}M`;
}

// Extract preview text from gRPC JSON data
function getPreviewText(grpcData: string | undefined, maxLen: number = 80): string | null {
  if (!grpcData) return null;
  
  try {
    const data = JSON.parse(grpcData);
    // Try to find meaningful fields for preview
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
      
      // Stop if we have enough
      if (previewFields.join(', ').length > maxLen) break;
    }
    
    const preview = previewFields.join(', ');
    return preview.length > maxLen ? preview.substring(0, maxLen) + '...' : preview;
  } catch {
    // If not valid JSON, just truncate
    return grpcData.length > maxLen ? grpcData.substring(0, maxLen) + '...' : grpcData;
  }
}

export const SessionList = memo(function SessionList({
  sessions,
  selectedSession,
  onSelectSession,
}: SessionListProps) {
  return (
    <div className="flex flex-col h-full overflow-hidden border-r">
      <div className="p-3 border-b font-semibold text-sm bg-muted/50 flex-shrink-0">
        RPC Calls ({sessions.length})
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="p-2 space-y-1">
          {/* All calls option */}
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

          {/* Individual RPC calls */}
          {sessions.map((session) => {
            const isSelected = selectedSession === session.id;
            // Display method name or URL path
            const methodDisplay = session.grpc_method 
              || (session.url ? session.url.split('/').pop() : null)
              || 'unknown';
            const serviceDisplay = session.grpc_service 
              || (session.url ? session.url.split('/').slice(-2, -1)[0] : null)
              || session.host;

            return (
              <button
                key={session.id}
                onClick={() => onSelectSession(session.id)}
                className={cn(
                  'w-full text-left px-3 py-2.5 rounded-md text-sm transition-colors border',
                  isSelected
                    ? 'bg-primary text-primary-foreground border-primary'
                    : 'hover:bg-gray-50 dark:hover:bg-gray-800 border-gray-200 dark:border-gray-700'
                )}
              >
                {/* Main: method (primary) */}
                <div className="font-semibold truncate text-sm" title={methodDisplay}>
                  {methodDisplay}
                </div>

                {/* Service (secondary) */}
                <div className={cn(
                  'truncate text-xs mt-0.5',
                  isSelected ? 'opacity-80' : 'text-gray-600 dark:text-gray-400'
                )} title={serviceDisplay}>
                  {serviceDisplay}
                </div>

                {/* Record count + Time */}
                <div className={cn(
                  'mt-1.5 flex items-center gap-2 text-xs',
                  isSelected ? 'opacity-90' : 'text-gray-600 dark:text-gray-400'
                )}>
                  <span>{session.record_count} frames</span>
                  <span>·</span>
                  <span>{formatTimestamp(session.first_ts)}</span>
                </div>

                {/* Sizes row */}
                <div className={cn(
                  'mt-1 flex items-center gap-3 text-xs font-medium'
                )}>
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

                {/* Host */}
                <div className={cn(
                  'mt-1 truncate text-xs',
                  isSelected ? 'opacity-70' : 'text-gray-500 dark:text-gray-500'
                )} title={session.host}>
                  {session.host}
                </div>

                {/* gRPC Preview */}
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
              </button>
            );
          })}

          {sessions.length === 0 && (
            <div className="text-center text-gray-500 dark:text-gray-400 text-sm py-8">
              No RPC calls yet
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
});
