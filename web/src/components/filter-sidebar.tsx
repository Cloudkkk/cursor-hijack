'use client';

import { useState } from 'react';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { ChevronRight, ChevronDown } from 'lucide-react';

interface FilterSidebarProps {
  availableFilters: Map<string, Set<string>>;
  methodCounts: Map<string, number>; // service.method -> session count
  selectedServices: Set<string>;
  selectedMethods: Set<string>;
  onServiceToggle: (service: string) => void;
  onMethodToggle: (fullMethod: string) => void;
  onClearFilters: () => void;
}

export function FilterSidebar({
  availableFilters,
  methodCounts,
  selectedServices,
  selectedMethods,
  onServiceToggle,
  onMethodToggle,
  onClearFilters,
}: FilterSidebarProps) {
  const hasFilters = selectedServices.size > 0 || selectedMethods.size > 0;
  const services = Array.from(availableFilters.entries()).sort((a, b) => 
    a[0].localeCompare(b[0])
  );

  // Track collapsed services (inverted logic - all expanded by default)
  const [collapsedServices, setCollapsedServices] = useState<Set<string>>(new Set());

  const toggleExpand = (service: string) => {
    setCollapsedServices((prev) => {
      const next = new Set(prev);
      if (next.has(service)) {
        next.delete(service);
      } else {
        next.add(service);
      }
      return next;
    });
  };

  // Get short service name (last part)
  const getShortName = (fullName: string) => {
    const parts = fullName.split('.');
    return parts[parts.length - 1];
  };

  return (
    <div className="flex flex-col h-full overflow-hidden border-r">
      <div className="p-3 border-b flex items-center justify-between flex-shrink-0 bg-muted/50">
        <span className="font-semibold text-sm">
          Services
          <span className="ml-1 text-xs font-normal text-gray-500">({services.length})</span>
        </span>
        {hasFilters && (
          <Button
            variant="ghost"
            size="sm"
            className="h-6 px-2 text-xs"
            onClick={onClearFilters}
          >
            Clear
          </Button>
        )}
      </div>
      <ScrollArea className="flex-1 min-h-0">
        <div className="p-2">
          {services.length === 0 ? (
            <div className="text-center text-gray-500 dark:text-gray-400 text-xs py-4">
              No services yet
            </div>
          ) : (
            <div className="space-y-0.5">
              {services.map(([service, methods]) => {
                const isServiceSelected = selectedServices.has(service);
                const methodList = Array.from(methods).sort();
                const isExpanded = !collapsedServices.has(service);
                const hasSelectedMethod = methodList.some(m => selectedMethods.has(`${service}.${m}`));
                const shortName = getShortName(service);
                // Calculate total sessions for this service
                const serviceSessionCount = methodList.reduce((sum, method) => {
                  return sum + (methodCounts.get(`${service}.${method}`) || 0);
                }, 0);

                return (
                  <div key={service}>
                    {/* Service row */}
                    <div className="flex items-center gap-0.5 min-w-0">
                      {/* Expand toggle */}
                      <button
                        onClick={() => toggleExpand(service)}
                        className="w-5 h-5 flex items-center justify-center text-gray-500 hover:text-gray-700 dark:hover:text-gray-300 flex-shrink-0"
                      >
                        {isExpanded ? (
                          <ChevronDown className="w-3.5 h-3.5" />
                        ) : (
                          <ChevronRight className="w-3.5 h-3.5" />
                        )}
                      </button>

                      {/* Service name */}
                      <button
                        onClick={() => onServiceToggle(service)}
                        title={service}
                        className={cn(
                          'flex-1 min-w-0 text-left px-2 py-1.5 rounded text-xs font-medium transition-colors flex items-center justify-between gap-2',
                          isServiceSelected
                            ? 'bg-primary text-primary-foreground'
                            : hasSelectedMethod
                            ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-600 dark:text-blue-400'
                            : 'hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-700 dark:text-gray-300'
                        )}
                      >
                        <span className="break-all">{shortName}</span>
                        <span className={cn(
                          'text-[10px] px-1.5 py-0.5 rounded font-medium flex-shrink-0 min-w-[18px] text-center',
                          isServiceSelected
                            ? 'bg-white/20 text-white'
                            : 'bg-gray-200 dark:bg-gray-700 text-gray-700 dark:text-gray-300'
                        )}>
                          {serviceSessionCount}
                        </span>
                      </button>
                    </div>

                    {/* Methods (collapsible) */}
                    {isExpanded && (
                      <div className="ml-5 mt-0.5 space-y-0.5 border-l border-gray-300 dark:border-gray-600 pl-2">
                        {methodList.map((method) => {
                          const fullMethod = `${service}.${method}`;
                          const isMethodSelected = selectedMethods.has(fullMethod);
                          const sessionCount = methodCounts.get(fullMethod) || 0;

                          return (
                            <button
                              key={method}
                              onClick={() => onMethodToggle(fullMethod)}
                              title={method}
                              className={cn(
                                'w-full text-left px-1.5 py-1 rounded text-xs transition-colors flex items-center justify-between gap-1',
                                isMethodSelected
                                  ? 'bg-blue-500 text-white'
                                  : 'hover:bg-gray-100 dark:hover:bg-gray-800 text-gray-600 dark:text-gray-400'
                              )}
                            >
                              <span className="break-all flex-1">{method}</span>
                              <span className={cn(
                                'text-[10px] px-1 rounded flex-shrink-0',
                                isMethodSelected 
                                  ? 'text-white/80' 
                                  : 'bg-gray-100 dark:bg-gray-700 text-gray-500 dark:text-gray-400'
                              )}>
                                {sessionCount}
                              </span>
                            </button>
                          );
                        })}
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </ScrollArea>
    </div>
  );
}
