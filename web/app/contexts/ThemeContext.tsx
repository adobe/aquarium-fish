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

import React, { createContext, useContext, useState, useEffect } from 'react';
import type { ReactNode } from 'react';

export type Theme = 'light' | 'dark' | 'system';

interface ThemeContextType {
  theme: Theme;
  actualTheme: 'light' | 'dark';
  setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined);

export const useTheme = () => {
  const context = useContext(ThemeContext);
  if (context === undefined) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
};

interface ThemeProviderProps {
  children: ReactNode;
}

export const ThemeProvider: React.FC<ThemeProviderProps> = ({ children }) => {
  const [theme, setThemeState] = useState<Theme>('system');
  const [actualTheme, setActualTheme] = useState<'light' | 'dark'>('light');

  const updateActualTheme = (currentTheme: Theme) => {
    if (currentTheme === 'system') {
      const systemPrefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
      setActualTheme(systemPrefersDark ? 'dark' : 'light');
    } else {
      setActualTheme(currentTheme);
    }
  };

  const setTheme = (newTheme: Theme) => {
    setThemeState(newTheme);
    updateActualTheme(newTheme);
    localStorage.setItem('aquarium_theme', newTheme);
  };

  useEffect(() => {
    // Load theme from localStorage or default to system
    const savedTheme = localStorage.getItem('aquarium_theme') as Theme;
    const initialTheme = savedTheme || 'system';
    setThemeState(initialTheme);
    updateActualTheme(initialTheme);

    // Listen for system theme changes
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    const handleSystemThemeChange = () => {
      if (theme === 'system') {
        updateActualTheme('system');
      }
    };

    mediaQuery.addEventListener('change', handleSystemThemeChange);
    return () => mediaQuery.removeEventListener('change', handleSystemThemeChange);
  }, [theme]);

  // Apply theme to document
  useEffect(() => {
    const root = document.documentElement;

    if (actualTheme === 'dark') {
      root.classList.add('dark');
    } else {
      root.classList.remove('dark');
    }
  }, [actualTheme]);

  const value: ThemeContextType = {
    theme,
    actualTheme,
    setTheme,
  };

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
};
