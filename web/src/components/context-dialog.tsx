'use client';

import { useMemo, useState } from 'react';
import { Record as RecordEntry } from '@/lib/types';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Copy, Check, ChevronDown, ChevronRight } from 'lucide-react';

interface ParsedMessage {
  role: string;
  content: string;
  frameIndex: number;
  blobId: string;
  timestamp: string;
  rawSize: number;
}

interface ParsedFrame {
  frameIndex: number;
  timestamp: string;
  direction: string;
  messageType: string;
  traceId?: string;
  spanId?: string;
  blobId?: string;
  blobDataSize: number;
  parsed: unknown;
  raw: string;
}

function containsBinary(s: string): boolean {
  // eslint-disable-next-line no-control-regex
  return /[\x00-\x08\x0e-\x1f]/.test(s.slice(0, 1024));
}

function sanitizeString(s: string): string {
  if (!containsBinary(s)) return s;
  // eslint-disable-next-line no-control-regex
  return s.replace(/[\x00-\x08\x0e-\x1f]/g, '\uFFFD');
}

function sanitizeValue(val: unknown): unknown {
  if (val === null || val === undefined) return val;
  if (typeof val === 'string') {
    if (containsBinary(val)) return '[binary data]';
    return val;
  }
  if (Array.isArray(val)) return val.map(sanitizeValue);
  if (typeof val === 'object') {
    const out: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(val)) {
      out[k] = sanitizeValue(v);
    }
    return out;
  }
  return val;
}

function tryDecodeBlob(blobDataBase64: string): { decoded: string; parsed: unknown } | null {
  try {
    const bytes = Uint8Array.from(atob(blobDataBase64), (c) => c.charCodeAt(0));
    const decoded = new TextDecoder('utf-8').decode(bytes);

    if (containsBinary(decoded)) {
      return null;
    }

    try {
      const parsed = JSON.parse(decoded);
      return { decoded, parsed };
    } catch {
      return { decoded, parsed: null };
    }
  } catch {
    return null;
  }
}

function extractContextFrames(records: RecordEntry[]): ParsedFrame[] {
  const frames: ParsedFrame[] = [];

  for (const record of records) {
    if (record.type !== 'grpc' || !record.grpc_data) continue;

    // Skip records where grpc_data itself contains binary
    if (containsBinary(record.grpc_data)) continue;

    let data: { [key: string]: unknown };
    try {
      data = JSON.parse(record.grpc_data);
    } catch {
      continue;
    }

    const kvMsg = data.kvServerMessage as { [key: string]: unknown } | undefined;
    if (!kvMsg) continue;

    const setBlobArgs = kvMsg.setBlobArgs as { [key: string]: unknown } | undefined;
    if (!setBlobArgs) continue;

    const blobData = setBlobArgs.blobData as string | undefined;
    const blobId = setBlobArgs.blobId as string | undefined;
    const spanContext = kvMsg.spanContext as { [key: string]: string } | undefined;

    const blobResult = blobData ? tryDecodeBlob(blobData) : null;
    const parsedContent = blobResult?.parsed ?? blobResult?.decoded ?? null;

    frames.push({
      frameIndex: record.grpc_frame_index ?? 0,
      timestamp: record.ts,
      direction: record.direction || 'S2C',
      messageType: 'SetBlobArgs',
      traceId: spanContext?.traceId,
      spanId: spanContext?.spanId,
      blobId: blobId,
      blobDataSize: blobData ? blobData.length : 0,
      parsed: parsedContent ? sanitizeValue(parsedContent) : null,
      raw: blobData || '',
    });
  }

  return frames.sort((a, b) => a.frameIndex - b.frameIndex);
}

function stringifyContent(content: unknown): string {
  if (typeof content === 'string') return sanitizeString(content);
  if (Array.isArray(content)) {
    return content
      .map((part) => {
        if (typeof part === 'string') return sanitizeString(part);
        if (typeof part === 'object' && part !== null && 'text' in part) {
          return sanitizeString(String((part as { text: string }).text));
        }
        try { return sanitizeString(JSON.stringify(part)); } catch { return '[unrenderable]'; }
      })
      .join('\n');
  }
  if (typeof content === 'object' && content !== null) {
    try { return sanitizeString(JSON.stringify(content, null, 2)); } catch { /* fall through */ }
  }
  return sanitizeString(String(content ?? ''));
}

function extractConversation(frames: ParsedFrame[]): ParsedMessage[] {
  const messages: ParsedMessage[] = [];

  for (const frame of frames) {
    if (!frame.parsed) continue;

    const parsed = frame.parsed as { [key: string]: unknown };

    if (typeof parsed === 'object' && parsed !== null && 'role' in parsed && 'content' in parsed) {
      messages.push({
        role: parsed.role as string,
        content: stringifyContent(parsed.content),
        frameIndex: frame.frameIndex,
        blobId: frame.blobId || '',
        timestamp: frame.timestamp,
        rawSize: frame.blobDataSize,
      });
    }
  }

  return messages;
}

function CopyBtn({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {}
  };

  return (
    <Button variant="ghost" size="sm" className="h-6 w-6 p-0" onClick={handleCopy} title="Copy">
      {copied ? <Check className="w-3 h-3 text-green-500" /> : <Copy className="w-3 h-3" />}
    </Button>
  );
}

function CollapsibleSection({ title, badge, children, defaultOpen = false }: {
  title: string;
  badge?: string;
  children: React.ReactNode;
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className="border rounded-md">
      <button
        onClick={() => setOpen(!open)}
        className="w-full flex items-center gap-2 px-3 py-2 text-sm font-medium hover:bg-muted/50 transition-colors"
      >
        {open ? <ChevronDown className="w-4 h-4" /> : <ChevronRight className="w-4 h-4" />}
        <span>{title}</span>
        {badge && <Badge variant="secondary" className="text-xs">{badge}</Badge>}
      </button>
      {open && <div className="px-3 pb-3">{children}</div>}
    </div>
  );
}

const ROLE_STYLES: { [key: string]: { bg: string; border: string; label: string } } = {
  system: { bg: 'bg-amber-50 dark:bg-amber-950/30', border: 'border-amber-200 dark:border-amber-800', label: 'System' },
  user: { bg: 'bg-blue-50 dark:bg-blue-950/30', border: 'border-blue-200 dark:border-blue-800', label: 'User' },
  assistant: { bg: 'bg-green-50 dark:bg-green-950/30', border: 'border-green-200 dark:border-green-800', label: 'Assistant' },
  tool: { bg: 'bg-purple-50 dark:bg-purple-950/30', border: 'border-purple-200 dark:border-purple-800', label: 'Tool' },
};

function getDefaultStyle() {
  return { bg: 'bg-gray-50 dark:bg-gray-900/30', border: 'border-gray-200 dark:border-gray-700', label: 'Unknown' };
}

function MessageCard({ message }: { message: ParsedMessage }) {
  const [expanded, setExpanded] = useState(false);
  const style = ROLE_STYLES[message.role] || getDefaultStyle();
  const isLong = message.content.length > 500;
  const displayContent = isLong && !expanded ? message.content.slice(0, 500) + '...' : message.content;

  return (
    <div className={`rounded-lg border ${style.border} ${style.bg} overflow-hidden`}>
      <div className="flex items-center justify-between px-3 py-1.5 border-b border-inherit bg-black/5 dark:bg-white/5">
        <div className="flex items-center gap-2">
          <Badge variant="outline" className="text-xs font-semibold">{style.label}</Badge>
          <span className="text-xs text-muted-foreground">Frame #{message.frameIndex}</span>
          <span className="text-xs text-muted-foreground">{message.rawSize} chars</span>
        </div>
        <CopyBtn text={message.content} />
      </div>
      <div className="px-3 py-2">
        <pre className="text-xs font-mono whitespace-pre-wrap break-words leading-relaxed max-h-[400px] overflow-y-auto">
          {displayContent}
        </pre>
        {isLong && (
          <button
            onClick={() => setExpanded(!expanded)}
            className="mt-1 text-xs text-blue-600 dark:text-blue-400 hover:underline"
          >
            {expanded ? 'Collapse' : `Show all (${message.content.length} chars)`}
          </button>
        )}
      </div>
    </div>
  );
}

function RawFrameCard({ frame }: { frame: ParsedFrame }) {
  const displayData = useMemo(() => {
    if (frame.parsed) {
      try {
        const json = JSON.stringify(frame.parsed, null, 2);
        return sanitizeString(json);
      } catch {
        return sanitizeString(String(frame.parsed));
      }
    }
    if (frame.raw) {
      if (containsBinary(frame.raw)) return `[binary base64, ${frame.raw.length} chars]`;
      return frame.raw;
    }
    return '(empty)';
  }, [frame]);

  return (
    <div className="border rounded-md p-3 space-y-2">
      <div className="flex items-center gap-2 flex-wrap">
        <Badge variant="outline" className="text-xs">Frame #{frame.frameIndex}</Badge>
        <Badge variant={frame.direction === 'C2S' ? 'default' : 'secondary'} className="text-xs">{frame.direction}</Badge>
        <span className="text-xs text-muted-foreground">{frame.blobDataSize} chars</span>
        {frame.traceId && <span className="text-xs text-muted-foreground font-mono">trace: {frame.traceId.slice(0, 12)}...</span>}
        <div className="ml-auto"><CopyBtn text={displayData} /></div>
      </div>
      {frame.blobId && (
        <div className="text-xs text-muted-foreground font-mono truncate">blobId: {frame.blobId}</div>
      )}
      <pre className="text-xs font-mono bg-muted rounded p-2 whitespace-pre-wrap break-words max-h-[300px] overflow-y-auto">
        {displayData}
      </pre>
    </div>
  );
}

interface ContextDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  records: RecordEntry[];
  sessionId: string | null;
}

export function ContextDialog({ open, onOpenChange, records, sessionId }: ContextDialogProps) {
  const sessionRecords = useMemo(() => {
    if (!sessionId) return records;
    return records.filter((r) => r.session === sessionId);
  }, [records, sessionId]);

  const frames = useMemo(() => extractContextFrames(sessionRecords), [sessionRecords]);
  const conversation = useMemo(() => extractConversation(frames), [frames]);

  const [viewMode, setViewMode] = useState<'conversation' | 'frames'>('conversation');

  const allContent = useMemo(() => {
    if (viewMode === 'conversation') {
      return conversation.map((m) => `[${m.role}]\n${m.content}`).join('\n\n---\n\n');
    }
    return frames.map((f) => {
      let data: string;
      if (f.parsed) {
        try { data = sanitizeString(JSON.stringify(f.parsed, null, 2)); }
        catch { data = '[parse error]'; }
      } else if (f.raw && containsBinary(f.raw)) {
        data = `[binary base64, ${f.raw.length} chars]`;
      } else {
        data = f.raw || '(empty)';
      }
      return `[Frame #${f.frameIndex} ${f.direction}]\n${data}`;
    }).join('\n\n---\n\n');
  }, [viewMode, conversation, frames]);

  if (!open) return null;

  const descText = `${sessionId ? 'Session: ' + sessionId : 'All Sessions'} - ${String(frames.length)} blob frames, ${String(conversation.length)} messages`;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-5xl !grid-rows-[auto_auto_1fr] max-h-[85vh]">
        <DialogHeader>
          <DialogTitle>Context Viewer</DialogTitle>
          <DialogDescription>{descText}</DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-2">
          <Button
            variant={viewMode === 'conversation' ? 'default' : 'outline'}
            size="sm"
            onClick={() => setViewMode('conversation')}
          >
            Conversation ({conversation.length})
          </Button>
          <Button
            variant={viewMode === 'frames' ? 'default' : 'outline'}
            size="sm"
            onClick={() => setViewMode('frames')}
          >
            All Frames ({frames.length})
          </Button>
          <div className="ml-auto">
            <CopyBtn text={allContent} />
          </div>
        </div>

        <div className="overflow-y-auto min-h-0 -mx-6 px-6">
          {viewMode === 'conversation' ? (
            <div className="space-y-3 pb-4">
              {conversation.length === 0 ? (
                <div className="text-center text-muted-foreground py-12">
                  No conversation messages found in blob data.
                  <br />
                  <span className="text-xs">Try switching to &quot;All Frames&quot; view to see raw data.</span>
                </div>
              ) : (
                conversation.map((msg, idx) => <MessageCard key={idx} message={msg} />)
              )}
            </div>
          ) : (
            <div className="space-y-3 pb-4">
              {frames.length === 0 ? (
                <div className="text-center text-muted-foreground py-12">
                  No SetBlobArgs frames found in this session.
                </div>
              ) : (
                <>
                  <CollapsibleSection title="Trace Info" badge={frames[0]?.traceId?.slice(0, 16)} defaultOpen={false}>
                    <div className="space-y-1 text-xs font-mono">
                      {Array.from(new Set(frames.map((f) => f.traceId).filter(Boolean))).map((tid, i) => (
                        <div key={i} className="flex items-center gap-2">
                          <span className="text-muted-foreground">traceId:</span>
                          <span>{tid}</span>
                        </div>
                      ))}
                    </div>
                  </CollapsibleSection>
                  {frames.map((frame, idx) => <RawFrameCard key={idx} frame={frame} />)}
                </>
              )}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
