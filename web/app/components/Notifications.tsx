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

import React, { createContext, useContext, useState, useCallback, useRef, useEffect } from 'react';

// Notification types
export type NotificationType = 'error' | 'warning' | 'info';
export interface Notification {
  id: string;
  type: NotificationType;
  message: string;
  timestamp: Date;
  details?: string;
}

interface NotificationContextType {
  sendNotification: (type: NotificationType, message: string, details?: string) => void;
  clearNotification: (id: string) => void;
  clearAllNotifications: () => void;
}

const NotificationContext = createContext<NotificationContextType | undefined>(undefined);

export const useNotification = () => {
  const ctx = useContext(NotificationContext);
  if (!ctx) throw new Error('useNotification must be used within a NotificationProvider');
  return ctx;
};

export const NotificationProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [isHovered, setIsHovered] = useState(false);
  const [visible, setVisible] = useState(true);
  const hideTimeout = useRef<number | null>(null);

  const sendNotification = useCallback((type: NotificationType, message: string, details?: string) => {
    const notification: Notification = {
      id: crypto.randomUUID(),
      type,
      message,
      timestamp: new Date(),
      details,
    };
    setNotifications(prev => [notification, ...prev.slice(0, 4)]); // Keep last 5
  }, []);

  const clearNotification = useCallback((id: string) => {
    setNotifications(prev => prev.filter(n => n.id !== id));
  }, []);

  const clearAllNotifications = useCallback(() => {
    setNotifications([]);
  }, []);

  useEffect(() => {
    if (notifications.length === 0) {
      setVisible(false);
      return;
    }
    setVisible(true);
    if (!isHovered) {
      if (hideTimeout.current) clearTimeout(hideTimeout.current);
      hideTimeout.current = window.setTimeout(() => {
        setVisible(false);
      }, 10000);
    } else {
      if (hideTimeout.current) clearTimeout(hideTimeout.current);
    }
    return () => {
      if (hideTimeout.current) clearTimeout(hideTimeout.current);
    };
  }, [notifications, isHovered]);

  return (
    <NotificationContext.Provider value={{ sendNotification, clearNotification, clearAllNotifications }}>
      {children}
      <NotificationsUI
        notifications={notifications}
        clearNotification={clearNotification}
        clearAllNotifications={clearAllNotifications}
        isHovered={isHovered}
        setIsHovered={setIsHovered}
        visible={visible}
      />
    </NotificationContext.Provider>
  );
};

// UI component for notifications
const NotificationsUI: React.FC<{
  notifications: Notification[];
  clearNotification: (id: string) => void;
  clearAllNotifications: () => void;
  isHovered: boolean;
  setIsHovered: (v: boolean) => void;
  visible: boolean;
}> = ({ notifications, clearNotification, clearAllNotifications, isHovered, setIsHovered, visible }) => {
  if (notifications.length === 0 || !visible) {
    return null;
  }

  const getNotificationIcon = (type: string) => {
    switch (type) {
      case 'error':
        return (
          <svg className="w-5 h-5 text-red-500" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7 4a1 1 0 11-2 0 1 1 0 012 0zm-1-9a1 1 0 00-1 1v4a1 1 0 102 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
          </svg>
        );
      case 'warning':
        return (
          <svg className="w-5 h-5 text-yellow-500" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M8.257 3.099c.765-1.36 2.722-1.36 3.486 0l5.58 9.92c.75 1.334-.213 2.98-1.742 2.98H4.42c-1.53 0-2.493-1.646-1.743-2.98l5.58-9.92zM11 13a1 1 0 11-2 0 1 1 0 012 0zm-1-8a1 1 0 00-1 1v3a1 1 0 002 0V6a1 1 0 00-1-1z" clipRule="evenodd" />
          </svg>
        );
      case 'info':
        return (
          <svg className="w-5 h-5 text-blue-500" fill="currentColor" viewBox="0 0 20 20">
            <path fillRule="evenodd" d="M18 10a8 8 0 11-16 0 8 8 0 0116 0zm-7-4a1 1 0 11-2 0 1 1 0 012 0zM9 9a1 1 0 000 2v3a1 1 0 001 1h1a1 1 0 100-2v-3a1 1 0 00-1-1H9z" clipRule="evenodd" />
          </svg>
        );
      default:
        return null;
    }
  };

  const getNotificationColors = (type: string) => {
    switch (type) {
      case 'error':
        return 'bg-red-100 border-red-500 text-red-700 dark:bg-red-900 dark:text-red-300 dark:border-red-700';
      case 'warning':
        return 'bg-yellow-100 border-yellow-500 text-yellow-700 dark:bg-yellow-900 dark:text-yellow-300 dark:border-yellow-700';
      case 'info':
        return 'bg-blue-100 border-blue-500 text-blue-700 dark:bg-blue-900 dark:text-blue-300 dark:border-blue-700';
      default:
        return 'bg-gray-100 border-gray-500 text-gray-700 dark:bg-gray-900 dark:text-gray-300 dark:border-gray-700';
    }
  };

  return (
    <div
      className="fixed bottom-4 right-4 z-50 space-y-2 max-w-sm flex flex-col-reverse"
      onMouseEnter={() => setIsHovered(true)}
      onMouseLeave={() => setIsHovered(false)}
      style={{ alignItems: 'flex-end' }}
    >
      <div className="flex justify-end w-full">
        {notifications.length > 1 && (
          <button
            onClick={clearAllNotifications}
            className="mb-2 px-2 py-1 text-xs bg-gray-200 text-gray-700 rounded-md hover:bg-gray-300 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
          >
            Clear All ({notifications.length})
          </button>
        )}
      </div>

      {notifications.slice().reverse().map((notification, index) => (
        <div
          key={notification.id}
          className={`border-l-4 p-4 rounded-md shadow-lg transition-all duration-300 ease-in-out transform ${
            index === 0 ? 'translate-x-0 opacity-100' : 'translate-x-full opacity-90'
          } ${getNotificationColors(notification.type)}`}
          style={{
            marginBottom: index * 8,
            zIndex: 50 - index,
          }}
        >
          <div className="flex items-start">
            <div className="flex-shrink-0">
              {getNotificationIcon(notification.type)}
            </div>
            <div className="ml-3 flex-1">
              <div className="flex items-center justify-between">
                <p className="text-sm font-medium">{notification.message}</p>
                <button
                  onClick={() => clearNotification(notification.id)}
                  className="ml-2 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
                >
                  <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                    <path fillRule="evenodd" d="M4.293 4.293a1 1 0 011.414 0L10 8.586l4.293-4.293a1 1 0 111.414 1.414L11.414 10l4.293 4.293a1 1 0 01-1.414 1.414L10 11.414l-4.293 4.293a1 1 0 01-1.414-1.414L8.586 10 4.293 5.707a1 1 0 010-1.414z" clipRule="evenodd" />
                  </svg>
                </button>
              </div>
              {notification.details && (
                <details className="mt-2">
                  <summary className="text-xs cursor-pointer hover:underline">
                    Details
                  </summary>
                  <div className="mt-1 text-xs font-mono bg-gray-50 dark:bg-gray-800 p-2 rounded max-h-32 overflow-y-auto">
                    {notification.details}
                  </div>
                </details>
              )}
              <div className="text-xs text-gray-500 dark:text-gray-400 mt-1">
                {notification.timestamp.toLocaleTimeString()}
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
};
