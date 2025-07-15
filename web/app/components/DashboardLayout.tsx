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

import React, { useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router';
import type { ReactNode } from 'react';
import { useAuth } from '../contexts/AuthContext';
import { useTheme } from '../contexts/ThemeContext';

interface DashboardLayoutProps {
  children: ReactNode;
}

export const DashboardLayout: React.FC<DashboardLayoutProps> = ({ children }) => {
  const { user, logout } = useAuth();
  const { theme, setTheme, actualTheme } = useTheme();
  const location = useLocation();
  const navigate = useNavigate();
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);
  const [isUserMenuOpen, setIsUserMenuOpen] = useState(false);
  const [isCollapsed, setIsCollapsed] = useState(false);

  const navigation = [
    { name: 'Applications', href: '/applications', icon: '📱' },
    { name: 'Status', href: '/status', icon: '📊' },
    { name: 'Manage', href: '/manage', icon: '⚙️' },
  ];

  const themeOptions = [
    { value: 'system', label: 'System', icon: '🖥️' },
    { value: 'light', label: 'Light', icon: '☀️' },
    { value: 'dark', label: 'Dark', icon: '🌙' },
  ];

  const isActiveRoute = (href: string) => location.pathname === href;

  const handleLogout = () => {
    logout();
    navigate('/login');
  };

  return (
    <div className="min-h-screen bg-gray-50 dark:bg-gray-900">
      {/* Desktop Sidebar */}
      <div
        className={`hidden lg:flex lg:flex-col lg:fixed lg:inset-y-0 lg:left-0 lg:z-50 bg-white dark:bg-gray-800 border-r border-gray-200 dark:border-gray-700 shadow-lg transition-all duration-300 ${
          isCollapsed ? 'lg:w-16' : 'lg:w-64'
        }`}
        onMouseEnter={() => setIsCollapsed(false)}
        onMouseLeave={() => setIsCollapsed(true)}
      >
        <div className="flex items-center justify-between h-16 px-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center min-w-0">
            {!isCollapsed && (
              <span className="text-xl font-bold text-gray-900 dark:text-white">
                Aquarium Fish
              </span>
            )}
            {isCollapsed && (
              <span className="text-xl font-bold text-gray-900 dark:text-white">
                🐠
              </span>
            )}
          </div>
        </div>

        <nav className="flex-1 px-2 py-4 space-y-2">
          {navigation.map((item) => (
            <Link
              key={item.name}
              to={item.href}
              className={`group flex items-center px-3 py-2 text-sm font-medium rounded-md transition-colors ${
                isActiveRoute(item.href)
                  ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-200'
                  : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700'
              }`}
            >
              <span className="text-lg flex-shrink-0">{item.icon}</span>
              {!isCollapsed && (
                <span className="ml-3">
                  {item.name}
                </span>
              )}
            </Link>
          ))}
        </nav>
      </div>

      {/* Mobile Sidebar */}
      <div className={`lg:hidden fixed inset-y-0 left-0 z-50 w-64 bg-white dark:bg-gray-800 shadow-lg transform ${
        isSidebarOpen ? 'translate-x-0' : '-translate-x-full'
      } transition-transform duration-300 ease-in-out`}>
        <div className="flex items-center justify-between h-16 px-4 border-b border-gray-200 dark:border-gray-700">
          <div className="flex items-center">
            <span className="text-xl font-bold text-gray-900 dark:text-white">
              Aquarium Fish
            </span>
          </div>
          <button
            onClick={() => setIsSidebarOpen(false)}
            className="p-2 rounded-md text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <span className="sr-only">Close sidebar</span>
            <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <nav className="mt-5 px-2 space-y-1">
          {navigation.map((item) => (
            <Link
              key={item.name}
              to={item.href}
              className={`group flex items-center px-2 py-2 text-sm font-medium rounded-md ${
                isActiveRoute(item.href)
                  ? 'bg-blue-50 dark:bg-blue-900/20 text-blue-700 dark:text-blue-200'
                  : 'text-gray-700 dark:text-gray-300 hover:bg-gray-50 dark:hover:bg-gray-700'
              }`}
              onClick={() => setIsSidebarOpen(false)}
            >
              <span className="mr-3 text-lg">{item.icon}</span>
              {item.name}
            </Link>
          ))}
        </nav>
      </div>

      {/* Mobile Overlay */}
      {isSidebarOpen && (
        <div
          className="lg:hidden fixed inset-0 bg-gray-600 bg-opacity-75 z-40"
          onClick={() => setIsSidebarOpen(false)}
        />
      )}

      {/* Main content */}
      <div className={`transition-all duration-300 ${
        isCollapsed ? 'lg:ml-16' : 'lg:ml-64'
      }`}>
        {/* Top bar */}
        <div className="bg-white dark:bg-gray-800 shadow-sm border-b border-gray-200 dark:border-gray-700 sticky top-0 z-30">
          <div className="flex items-center justify-between h-16 px-4">
            <button
              onClick={() => setIsSidebarOpen(true)}
              className="lg:hidden p-2 rounded-md text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
            >
              <span className="sr-only">Open sidebar</span>
              <svg className="w-6 h-6" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M4 6h16M4 12h16M4 18h16" />
              </svg>
            </button>

            {/* Push content to the right */}
            <div className="flex-1"></div>

            <div className="flex items-center space-x-4">
              {/* User menu */}
              <div className="relative">
                <button
                  onClick={() => setIsUserMenuOpen(!isUserMenuOpen)}
                  className="flex items-center text-sm rounded-full focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-blue-500"
                >
                  <span className="sr-only">Open user menu</span>
                  <div className="h-8 w-8 bg-blue-600 rounded-full flex items-center justify-center">
                    <span className="text-white font-medium">
                      {user?.userName?.charAt(0).toUpperCase() || 'U'}
                    </span>
                  </div>
                </button>

                {isUserMenuOpen && (
                  <div className="absolute right-0 mt-2 w-56 bg-white dark:bg-gray-800 rounded-md shadow-lg z-50 border border-gray-200 dark:border-gray-700">
                    <div className="py-1">
                      <div className="px-4 py-2 text-sm text-gray-700 dark:text-gray-300 border-b border-gray-200 dark:border-gray-700">
                        <div className="font-medium">{user?.userName}</div>
                        <div className="text-xs text-gray-500 dark:text-gray-400">
                          {user?.roles?.join(', ')}
                        </div>
                      </div>

                      {/* Theme selector in user menu */}
                      <div className="px-4 py-2 border-b border-gray-200 dark:border-gray-700">
                        <div className="text-xs font-medium text-gray-500 dark:text-gray-400 mb-2">Theme</div>
                        <div className="flex bg-gray-100 dark:bg-gray-700 rounded-lg p-1">
                          {themeOptions.map((option) => (
                            <button
                              key={option.value}
                              onClick={() => setTheme(option.value as any)}
                              className={`flex-1 px-2 py-1 rounded-md text-xs font-medium transition-colors ${
                                theme === option.value
                                  ? 'bg-white dark:bg-gray-800 text-gray-900 dark:text-white shadow-sm'
                                  : 'text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white'
                              }`}
                            >
                              <span className="mr-1">{option.icon}</span>
                              {option.label}
                            </button>
                          ))}
                        </div>
                      </div>

                      <button
                        onClick={handleLogout}
                        className="block w-full text-left px-4 py-2 text-sm text-gray-700 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-gray-700"
                      >
                        Sign out
                      </button>
                    </div>
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>

        {/* Page content */}
        <main className="p-4">
          {children}
        </main>
      </div>
    </div>
  );
};
