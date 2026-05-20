'use client';

import { useState, memo } from 'react';
import { Record, formatTimestamp } from '@/lib/types';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Separator } from '@/components/ui/separator';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { JsonViewer } from './json-viewer';
import { HeaderTable } from './header-table';
import { Copy, Check } from 'lucide-react';

interface DetailPanelProps {
  record: Record | null;
}

// Copy button component
function CopyButton({ text, className }: { text: string; className?: string }) {
  const [copied, setCopied] = useState(false);
  
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy:', err);
    }
  };

  return (
    <Button
      variant="ghost"
      size="sm"
      className={className}
      onClick={handleCopy}
      title="Copy to clipboard"
    >
      {copied ? (
        <Check className="w-3.5 h-3.5 text-green-500" />
      ) : (
        <Copy className="w-3.5 h-3.5" />
      )}
    </Button>
  );
}

// Memoized to prevent re-renders when parent updates
function DetailPanelInner({ record }: DetailPanelProps) {
  if (!record) {
    return (
      <div className="flex items-center justify-center h-full text-muted-foreground">
        Select a record to view details
      </div>
    );
  }

  return (
    <ScrollArea className="h-full w-full">
      <div className="p-4 space-y-4 w-full max-w-full overflow-hidden">
        {/* Header */}
        <div className="space-y-2">
          <div className="flex items-center gap-2 flex-wrap">
            <Badge>{record.type}</Badge>
            {record.direction && (
              <Badge variant={record.direction === 'C2S' ? 'default' : 'secondary'}>
                {record.direction}
              </Badge>
            )}
            {record.grpc_streaming && <Badge variant="outline">streaming</Badge>}
            {record.grpc_compressed && <Badge variant="outline">compressed</Badge>}
          </div>
          <div className="text-sm text-muted-foreground">
            {formatTimestamp(record.ts)} · Session: {record.session} · Index: {record.index}
          </div>
        </div>

        <Separator />

        {/* Request/Response specific */}
        {record.type === 'request' && (
          <div className="space-y-2 w-full min-w-0">
            <div className="font-semibold text-sm">Request</div>
            <div className="font-mono text-sm bg-muted p-2 rounded overflow-x-auto">
              <span className="whitespace-nowrap">{record.method} {record.url}</span>
            </div>
            <div className="text-sm">Host: {record.host}</div>
          </div>
        )}

        {record.type === 'response' && (
          <div className="space-y-2">
            <div className="font-semibold text-sm">Response</div>
            <div className="font-mono text-sm bg-muted p-2 rounded">
              {record.status} {record.status_text}
            </div>
          </div>
        )}

        {/* gRPC specific */}
        {record.type === 'grpc' && (
          <div className="space-y-2">
            <div className="font-semibold text-sm">gRPC</div>
            <div className="grid grid-cols-2 gap-2 text-sm">
              <div className="min-w-0">
                <span className="text-muted-foreground">Service:</span>
                <div className="font-mono text-xs truncate" title={record.grpc_service}>{record.grpc_service}</div>
              </div>
              <div className="min-w-0">
                <span className="text-muted-foreground">Method:</span>
                <div className="font-mono text-xs truncate" title={record.grpc_method}>{record.grpc_method}</div>
              </div>
              {record.grpc_frame_index !== undefined && (
                <div>
                  <span className="text-muted-foreground">Frame Index:</span>
                  <div>{record.grpc_frame_index}</div>
                </div>
              )}
              {record.size !== undefined && (
                <div>
                  <span className="text-muted-foreground">Size:</span>
                  <div>{record.size} bytes</div>
                </div>
              )}
            </div>
          </div>
        )}

        {/* SSE specific */}
        {record.type === 'sse' && (
          <div className="space-y-2">
            <div className="font-semibold text-sm">SSE Event</div>
            <div className="grid grid-cols-2 gap-2 text-sm">
              <div>
                <span className="text-muted-foreground">Event Type:</span>
                <div>{record.event_type || 'message'}</div>
              </div>
              {record.event_id && (
                <div>
                  <span className="text-muted-foreground">Event ID:</span>
                  <div>{record.event_id}</div>
                </div>
              )}
            </div>
            {record.event_data && (
              <div>
                <span className="text-muted-foreground text-sm">Data:</span>
                <pre className="mt-1 p-2 bg-muted rounded text-xs overflow-auto">
                  {record.event_data}
                </pre>
              </div>
            )}
          </div>
        )}

        <Separator />

        {/* Headers */}
        {record.headers && Object.keys(record.headers).length > 0 && (
          <div className="space-y-2">
            <div className="font-semibold text-sm">Headers</div>
            <HeaderTable headers={record.headers} />
          </div>
        )}

        {/* gRPC Data */}
        {record.grpc_data && (
          <div className="space-y-2 w-full min-w-0 overflow-hidden">
            <div className="font-semibold text-sm flex items-center justify-between">
              <span>gRPC Data</span>
              <CopyButton text={record.grpc_data} className="h-6 w-6 p-0" />
            </div>
            <JsonViewer data={record.grpc_data} />
          </div>
        )}

        {/* Body */}
        {record.body && (
          <div className="space-y-2 w-full min-w-0 overflow-hidden">
            <div className="font-semibold text-sm flex items-center justify-between">
              <span>Body</span>
              <CopyButton text={record.body} className="h-6 w-6 p-0" />
            </div>
            {record.content_type?.includes('json') ? (
              <JsonViewer data={record.body} />
            ) : (
              <div className="w-full overflow-x-auto">
                <pre className="p-2 bg-muted rounded text-xs whitespace-pre max-h-96" style={{ minWidth: 'min-content' }}>
                  {record.body}
                </pre>
              </div>
            )}
          </div>
        )}

        {/* Base64 Body */}
        {record.body_base64 && (
          <div className="space-y-2 w-full min-w-0 overflow-hidden">
            <div className="font-semibold text-sm flex items-center justify-between">
              <span>Body (Base64)</span>
              <CopyButton text={record.body_base64} className="h-6 w-6 p-0" />
            </div>
            <div className="w-full overflow-x-auto">
              <pre className="p-2 bg-muted rounded text-xs max-h-96 break-all whitespace-pre-wrap">
                {record.body_base64}
              </pre>
            </div>
          </div>
        )}

        {/* Raw gRPC data (on error) */}
        {record.grpc_raw && (
          <div className="space-y-2 w-full min-w-0 overflow-hidden">
            <div className="font-semibold text-sm text-red-600 flex items-center justify-between">
              <span>Raw Data (Debug)</span>
              <CopyButton text={record.grpc_raw} className="h-6 w-6 p-0" />
            </div>
            <div className="w-full overflow-x-auto">
              <pre className="p-2 bg-red-50 dark:bg-red-900/20 rounded text-xs max-h-96 break-all whitespace-pre-wrap">
                {record.grpc_raw}
              </pre>
            </div>
          </div>
        )}

        {/* Error */}
        {record.error && (
          <div className="space-y-2">
            <div className="font-semibold text-sm text-red-600">Error</div>
            <div className="p-2 bg-red-50 text-red-800 rounded text-sm">
              {record.error}
            </div>
          </div>
        )}
      </div>
    </ScrollArea>
  );
}

// Use memo with custom comparison to prevent re-renders
// Only re-render when the actual record content changes
export const DetailPanel = memo(DetailPanelInner, (prevProps, nextProps) => {
  // If both null, no change
  if (!prevProps.record && !nextProps.record) return true;
  // If one is null and other isn't, re-render
  if (!prevProps.record || !nextProps.record) return false;
  // Compare by unique key (session + index)
  return (
    prevProps.record.session === nextProps.record.session &&
    prevProps.record.index === nextProps.record.index
  );
});
