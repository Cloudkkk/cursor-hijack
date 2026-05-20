'use client';

import { Button } from '@/components/ui/button';
import { Play, Pause, Trash2 } from 'lucide-react';

interface FilterBarProps {
  isPaused: boolean;
  onTogglePause: () => void;
  onClear: () => void;
}

export function FilterBar({ isPaused, onTogglePause, onClear }: FilterBarProps) {
  return (
    <div className="flex items-center gap-2">
      <Button
        variant={isPaused ? 'default' : 'outline'}
        size="sm"
        onClick={onTogglePause}
      >
        {isPaused ? (
          <>
            <Play className="w-4 h-4 mr-1" />
            Resume
          </>
        ) : (
          <>
            <Pause className="w-4 h-4 mr-1" />
            Pause
          </>
        )}
      </Button>
      <Button variant="outline" size="sm" onClick={onClear}>
        <Trash2 className="w-4 h-4 mr-1" />
        Clear
      </Button>
    </div>
  );
}
