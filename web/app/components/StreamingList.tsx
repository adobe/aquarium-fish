/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

import React, { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { useStreaming } from '../contexts/StreamingContext';

export type ObjectType = 'applications' | 'labels' | 'nodes' | 'users' | 'roles';
export type ItemStatus = 'normal' | 'created' | 'updated' | 'removing';

export interface ListItemAction {
  label: string;
  onClick: (item: any) => void;
  className?: string;
  condition?: (item: any) => boolean;
  permission?: { resource: string; action: string };
}

export interface ListColumn {
  key: string;
  label: string;
  render: (item: any) => React.ReactNode;
  sortable?: boolean;
  filterable?: boolean;
}

export interface StreamingListProps {
  objectType: ObjectType;
  columns: ListColumn[];
  actions?: ListItemAction[];
  filterBy?: string[];
  sortBy?: { key: string; direction: 'asc' | 'desc' };
  emptyMessage?: string;
  className?: string;
  itemKey: (item: any) => string;
  onItemClick?: (item: any) => void;
  permissions?: {
    list: { resource: string; action: string };
  };
  customData?: any[]; // Override automatic data selection with custom processed data
}

interface AnimatedItem {
  item: any;
  id: string;
  status: ItemStatus;
  changedFields?: Set<string>;
  removeTimeout?: number;
}

export const StreamingList: React.FC<StreamingListProps> = ({
  objectType,
  columns,
  actions = [],
  filterBy = [],
  sortBy,
  emptyMessage = 'No items found',
  className = '',
  itemKey,
  onItemClick,
  permissions,
  customData,
}) => {
  const { hasPermission } = useAuth();
  const { data, subscribe } = useStreaming();

  const [items, setItems] = useState<AnimatedItem[]>([]);
  const [filters, setFilters] = useState<Record<string, string>>({});
  const [sortConfig, setSortConfig] = useState(sortBy || { key: '', direction: 'asc' });
  const [isVisible, setIsVisible] = useState(false);
  const [hasInitialized, setHasInitialized] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const previousDataRef = useRef<any[]>([]);

  // Check if component is visible using Intersection Observer
  useEffect(() => {
    const observer = new IntersectionObserver(
      ([entry]) => {
        setIsVisible(entry.isIntersecting);
      },
      { threshold: 0.1 }
    );

    if (containerRef.current) {
      observer.observe(containerRef.current);
    }

    return () => observer.disconnect();
  }, []);

  // Get raw data from streaming context or use custom data
  const rawData = useMemo(() => {
    if (customData) {
      return customData;
    }

    switch (objectType) {
      case 'applications':
        return data.applications;
      case 'labels':
        return data.labels;
      case 'nodes':
        return data.nodes;
      case 'users':
        return data.users;
      case 'roles':
        return data.roles;
      default:
        return [];
    }
  }, [data, objectType, customData]);

  // Process streaming updates with animations
  const handleDataUpdate = useCallback((newData: any[]) => {
    if (!hasInitialized) {
      // Initial load - no animations
      const initialItems: AnimatedItem[] = newData.map(item => ({
        item,
        id: itemKey(item),
        status: 'normal',
      }));
      setItems(initialItems);
      previousDataRef.current = newData;
      setHasInitialized(true);
      return;
    }

    const previousData = previousDataRef.current;
    const previousIds = new Set(previousData.map(itemKey));
    const newIds = new Set(newData.map(itemKey));

    setItems(prevItems => {
      const itemsMap = new Map(prevItems.map(item => [item.id, item]));
      const updatedItems: AnimatedItem[] = [];

      // Process each item in the new data
      newData.forEach(newItem => {
        const id = itemKey(newItem);
        const existingItem = itemsMap.get(id);

        if (!previousIds.has(id)) {
          // New item - animate creation
          updatedItems.push({
            item: newItem,
            id,
            status: 'created',
          });
        } else if (existingItem) {
          // Check if item was updated
          //const wasUpdated = JSON.stringify(existingItem.item) !== JSON.stringify(newItem);
          //if (wasUpdated) {
          // Find changed fields
          const changedFields = new Set<string>();
          columns.forEach(column => {
            if (existingItem.item[column.key] !== newItem[column.key]) {
              changedFields.add(column.key);
            }
          });

          const wasUpdated = changedFields.size > 0
          if (wasUpdated) {
            updatedItems.push({
              ...existingItem,
              item: newItem,
              status: 'updated',
              changedFields,
            });
          } else {
            updatedItems.push({
              ...existingItem,
              item: newItem,
              status: 'normal',
            });
          }
        } else {
          updatedItems.push({
            item: newItem,
            id,
            status: 'normal',
          });
        }
      });

      // Handle removed items
      previousIds.forEach(id => {
        if (!newIds.has(id)) {
          const existingItem = itemsMap.get(id);
          if (existingItem && existingItem.status !== 'removing') {
            const removeTimeout = window.setTimeout(() => {
              setItems(current => current.filter(item => item.id !== id));
            }, 30000); // 30 seconds

            updatedItems.push({
              ...existingItem,
              status: 'removing',
              removeTimeout,
            });
          }
        }
      });

      return updatedItems;
    });

    previousDataRef.current = newData;

    // Clear animation states after a delay
    setTimeout(() => {
      setItems(prevItems =>
        prevItems.map(item => ({
          ...item,
          status: item.status === 'removing' ? 'removing' : 'normal',
          changedFields: undefined,
        }))
      );
    }, 2000);
  }, [hasInitialized, itemKey, columns]);

    // Subscribe to streaming updates when visible (or use custom data)
  useEffect(() => {
    if (!isVisible) return;

    if (customData) {
      // For custom data, we react to changes in the customData prop directly
      handleDataUpdate(customData);
      return;
    }

    const unsubscribe = subscribe((streamingData) => {
      // Get the relevant data from the streaming context
      let newData: any[];
      switch (objectType) {
        case 'applications':
          newData = streamingData.applications;
          break;
        case 'labels':
          newData = streamingData.labels;
          break;
        case 'nodes':
          newData = streamingData.nodes;
          break;
        case 'users':
          newData = streamingData.users;
          break;
        case 'roles':
          newData = streamingData.roles;
          break;
        default:
          newData = [];
      }

      handleDataUpdate(newData);
    });

    return unsubscribe;
  }, [isVisible, subscribe, handleDataUpdate, objectType, customData]);

  // Initialize when first visible
  useEffect(() => {
    if (isVisible && !hasInitialized) {
      handleDataUpdate(rawData);
    }
  }, [isVisible, hasInitialized, handleDataUpdate, rawData]);

  // Filter and sort items
  const processedItems = useMemo(() => {
    let filtered = items;

    // Apply filters
    Object.entries(filters).forEach(([key, value]) => {
      if (value.trim()) {
        filtered = filtered.filter(({ item }) => {
          const itemValue = item[key];
          return String(itemValue).toLowerCase().includes(value.toLowerCase());
        });
      }
    });

    // Apply sorting
    if (sortConfig.key) {
      filtered.sort((a, b) => {
        const aValue = a.item[sortConfig.key];
        const bValue = b.item[sortConfig.key];

        let comparison = 0;
        if (aValue < bValue) comparison = -1;
        if (aValue > bValue) comparison = 1;

        return sortConfig.direction === 'desc' ? -comparison : comparison;
      });
    }

    return filtered;
  }, [items, filters, sortConfig]);

  // Check permissions
  const canList = !permissions?.list || hasPermission(permissions.list.resource, permissions.list.action);

  if (!canList) {
    return (
      <div className="text-center py-8">
        <p className="text-red-600 dark:text-red-400">
          You don't have permission to view this list.
        </p>
        {permissions?.list && (
          <p className="text-gray-600 dark:text-gray-400 mt-2">
            Required permission: {permissions.list.resource}.{permissions.list.action}
          </p>
        )}
      </div>
    );
  }

  return (
    <div ref={containerRef} className={`streaming-list ${className}`}>
      {/* Filters */}
      {filterBy.length > 0 && (
        <div className="mb-4 flex flex-wrap gap-4">
          {filterBy.map(filterKey => {
            const column = columns.find(col => col.key === filterKey);
            if (!column || !column.filterable) return null;

            return (
              <input
                key={filterKey}
                type="text"
                placeholder={`Filter by ${column.label.toLowerCase()}`}
                value={filters[filterKey] || ''}
                onChange={(e) => setFilters(prev => ({ ...prev, [filterKey]: e.target.value }))}
                className="px-3 py-2 border border-gray-300 rounded-md dark:bg-gray-700 dark:border-gray-600 dark:text-white"
              />
            );
          })}
        </div>
      )}

      {/* List */}
      {!hasInitialized ? (
        <div className="text-center py-8">
          <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-blue-600 mx-auto"></div>
          <p className="mt-2 text-gray-600 dark:text-gray-400">Loading...</p>
        </div>
      ) : processedItems.length === 0 ? (
        <div className="text-center py-8 text-gray-500 dark:text-gray-400">
          {emptyMessage}
        </div>
      ) : (
        <div className="bg-white dark:bg-gray-800 shadow overflow-hidden sm:rounded-md">
          <ul className="divide-y divide-gray-200 dark:divide-gray-700">
            {processedItems.map(({ item, id, status, changedFields }) => (
              <li
                key={id}
                className={`
                  px-6 py-4 transition-all duration-300 cursor-pointer
                  ${status === 'created' ? 'animate-pulse bg-green-50 dark:bg-green-900/20 border-l-4 border-green-500' : ''}
                  ${status === 'updated' ? 'animate-pulse bg-blue-50 dark:bg-blue-900/20 border-l-4 border-blue-500' : ''}
                  ${status === 'removing' ? 'opacity-50 bg-red-50 dark:bg-red-900/20 border-l-4 border-red-500' : ''}
                  ${status === 'normal' ? 'hover:bg-gray-50 dark:hover:bg-gray-700' : ''}
                `}
                onClick={() => onItemClick?.(item)}
              >
                <div className="flex items-center justify-between">
                  <div className="flex items-center space-x-4 flex-1">
                    {columns.map(column => (
                      <div key={column.key} className="flex-1">
                        <div
                          className={`
                            transition-all duration-200
                            ${changedFields?.has(column.key) ? 'animate-pulse bg-yellow-200 dark:bg-yellow-800 rounded px-1' : ''}
                          `}
                        >
                          {column.render(item)}
                        </div>
                      </div>
                    ))}
                  </div>

                  {actions.length > 0 && (
                    <div className="flex items-center space-x-2 ml-4">
                      {actions.map((action, index) => {
                        const shouldShow = !action.condition || action.condition(item);
                        const hasActionPermission = !action.permission ||
                          hasPermission(action.permission.resource, action.permission.action);

                        if (!shouldShow || !hasActionPermission) return null;

                        return (
                          <button
                            key={index}
                            onClick={(e) => {
                              e.stopPropagation();
                              action.onClick(item);
                            }}
                            className={action.className || 'px-3 py-1 text-sm bg-gray-100 text-gray-800 rounded-md hover:bg-gray-200'}
                          >
                            {action.label}
                          </button>
                        );
                      })}
                    </div>
                  )}
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
};
