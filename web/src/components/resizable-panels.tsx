'use client';

import { useState, useRef, useEffect, Children, ReactNode } from 'react';
import { cn } from '@/lib/utils';

interface ResizablePanelsProps {
  children: ReactNode;
  defaultSizes: number[]; // percentages (should sum to 100)
  minSizes?: number[];    // pixels
  className?: string;
}

export function ResizablePanels({
  children,
  defaultSizes,
  minSizes = [],
  className,
}: ResizablePanelsProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [sizes, setSizes] = useState<number[]>(defaultSizes);
  const draggingRef = useRef<number | null>(null);
  const startXRef = useRef<number>(0);
  const startSizesRef = useRef<number[]>([]);

  const childArray = Children.toArray(children);

  const handleMouseDown = (index: number, e: React.MouseEvent) => {
    e.preventDefault();
    draggingRef.current = index;
    startXRef.current = e.clientX;
    startSizesRef.current = [...sizes];
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';
  };

  useEffect(() => {
    const handleMouseMove = (e: MouseEvent) => {
      if (draggingRef.current === null || !containerRef.current) return;

      const containerWidth = containerRef.current.offsetWidth;
      const deltaX = e.clientX - startXRef.current;
      const deltaPercent = (deltaX / containerWidth) * 100;

      const index = draggingRef.current;
      const newSizes = [...startSizesRef.current];

      // Adjust the panel to the left and right of the separator
      let leftSize = newSizes[index] + deltaPercent;
      let rightSize = newSizes[index + 1] - deltaPercent;

      // Apply minimum sizes (convert pixels to percent)
      const leftMinPercent = minSizes[index] ? (minSizes[index] / containerWidth) * 100 : 5;
      const rightMinPercent = minSizes[index + 1] ? (minSizes[index + 1] / containerWidth) * 100 : 5;

      if (leftSize < leftMinPercent) {
        leftSize = leftMinPercent;
        rightSize = startSizesRef.current[index] + startSizesRef.current[index + 1] - leftMinPercent;
      }
      if (rightSize < rightMinPercent) {
        rightSize = rightMinPercent;
        leftSize = startSizesRef.current[index] + startSizesRef.current[index + 1] - rightMinPercent;
      }

      newSizes[index] = leftSize;
      newSizes[index + 1] = rightSize;

      setSizes(newSizes);
    };

    const handleMouseUp = () => {
      draggingRef.current = null;
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    document.addEventListener('mousemove', handleMouseMove);
    document.addEventListener('mouseup', handleMouseUp);

    return () => {
      document.removeEventListener('mousemove', handleMouseMove);
      document.removeEventListener('mouseup', handleMouseUp);
    };
  }, [minSizes]);

  // Calculate total separators width (in pixels, roughly)
  const separatorCount = childArray.length - 1;

  return (
    <div ref={containerRef} className={cn('flex h-full w-full', className)}>
      {childArray.map((child, index) => (
        <div key={index} className="contents">
          {/* Panel */}
          <div
            style={{ 
              width: `calc(${sizes[index]}% - ${(separatorCount * 4) / childArray.length}px)`,
              flexShrink: 0,
            }}
            className="h-full overflow-hidden"
          >
            {child}
          </div>

          {/* Separator (not after last panel) */}
          {index < childArray.length - 1 && (
            <div
              onMouseDown={(e) => handleMouseDown(index, e)}
              className="w-1 h-full bg-border hover:bg-blue-500 active:bg-blue-600 cursor-col-resize flex-shrink-0 transition-colors"
            />
          )}
        </div>
      ))}
    </div>
  );
}
