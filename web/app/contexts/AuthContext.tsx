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

import React, { createContext, useContext, useState, useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { authService, tokenStorage, type AuthTokens, type AuthUser } from '../lib/auth';

interface AuthContextType {
  user: AuthUser | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  login: (username: string, password: string) => Promise<{ success: boolean; message: string }>;
  logout: () => void;
  checkAuth: () => Promise<void>;
  hasPermission: (resource: string, action: string) => boolean;
  hasRole: (role: string) => boolean;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (context === undefined) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return context;
};

interface AuthProviderProps {
  children: ReactNode;
}

export const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const refreshTimerRef = useRef<number | null>(null);

  const isAuthenticated = user !== null;

  const checkAuth = async () => {
    setIsLoading(true);
    try {
      const tokens = tokenStorage.getTokens();
      if (!tokens) {
        setUser(null);
        return;
      }

      // Check if refresh token is expired
      if (tokenStorage.isRefreshTokenExpired(tokens)) {
        tokenStorage.clearTokens();
        setUser(null);
        return;
      }

      // If access token is expired, try to refresh
      if (tokenStorage.isTokenExpired(tokens)) {
        const refreshResult = await authService.refreshToken(tokens.refreshToken);
        if (refreshResult.success && refreshResult.tokens) {
          tokenStorage.setTokens(refreshResult.tokens);
          // Get updated user permissions
          const permissionsResult = await authService.getPermissions();
          if (permissionsResult.success && permissionsResult.user) {
            setUser(permissionsResult.user);
          }
          scheduleTokenRefresh(refreshResult.tokens);
        } else {
          tokenStorage.clearTokens();
          setUser(null);
        }
        return;
      }

      // Token is valid, validate it and get user info
      const validateResult = await authService.validateToken(tokens.accessToken);
      if (validateResult.success && validateResult.user) {
        setUser(validateResult.user);
        scheduleTokenRefresh(tokens);
      } else {
        tokenStorage.clearTokens();
        setUser(null);
      }
    } catch (error) {
      console.error('Auth check error:', error);
      tokenStorage.clearTokens();
      setUser(null);
    } finally {
      setIsLoading(false);
    }
  };

  const login = async (username: string, password: string): Promise<{ success: boolean; message: string }> => {
    try {
      const result = await authService.login(username, password);

      if (result.success && result.tokens && result.user) {
        tokenStorage.setTokens(result.tokens);
        setUser(result.user);
        scheduleTokenRefresh(result.tokens);
      }

      return {
        success: result.success,
        message: result.message,
      };
    } catch (error) {
      console.error('Login error:', error);
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Login failed',
      };
    }
  };

  const logout = () => {
    tokenStorage.clearTokens();
    setUser(null);
    clearRefreshTimer();
  };

  // Clear the refresh timer
  const clearRefreshTimer = () => {
    if (refreshTimerRef.current) {
      clearTimeout(refreshTimerRef.current);
      refreshTimerRef.current = null;
    }
  };

  // Schedule proactive token refresh
  const scheduleTokenRefresh = (tokens: AuthTokens) => {
    clearRefreshTimer();

    // Calculate time until token expires (refresh 5 minutes before expiration)
    const refreshBuffer = 5 * 60 * 1000; // 5 minutes in milliseconds
    const expiresAt = tokens.expiresAt.getTime();
    const now = Date.now();
    const timeUntilRefresh = Math.max(0, expiresAt - now - refreshBuffer);

    console.log(`[AuthContext] Scheduling token refresh in ${Math.round(timeUntilRefresh / 1000 / 60)} minutes`);

    refreshTimerRef.current = window.setTimeout(async () => {
      try {
        console.log('[AuthContext] Proactive token refresh triggered');
        const currentTokens = tokenStorage.getTokens();

        if (!currentTokens || tokenStorage.isRefreshTokenExpired(currentTokens)) {
          console.log('[AuthContext] Refresh token expired, logging out');
          logout();
          return;
        }

        const refreshResult = await authService.refreshToken(currentTokens.refreshToken);
        if (refreshResult.success && refreshResult.tokens) {
          console.log('[AuthContext] Token refreshed successfully');
          tokenStorage.setTokens(refreshResult.tokens);

          // Get updated user permissions
          const permissionsResult = await authService.getPermissions();
          if (permissionsResult.success && permissionsResult.user) {
            setUser(permissionsResult.user);
          }

          // Schedule the next refresh
          scheduleTokenRefresh(refreshResult.tokens);
        } else {
          console.log('[AuthContext] Token refresh failed, logging out');
          logout();
        }
      } catch (error) {
        console.error('[AuthContext] Proactive token refresh error:', error);
        logout();
      }
    }, timeUntilRefresh);
  };

  const hasPermission = (resource: string, action: string): boolean => {
    if (!user) return false;

    return user.permissions.some(
      permission => permission.resource === resource && permission.action === action
    );
  };

  const hasRole = (role: string): boolean => {
    if (!user) return false;

    return user.roles.includes(role);
  };

  useEffect(() => {
    checkAuth();

    // Cleanup timer on unmount
    return () => {
      clearRefreshTimer();
    };
  }, []);

  const value: AuthContextType = {
    user,
    isAuthenticated,
    isLoading,
    login,
    logout,
    checkAuth,
    hasPermission,
    hasRole,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};
