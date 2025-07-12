import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react'
import { AuthService } from '../services/auth'

export interface User {
  name: string
  roles: string[]
  permissions: UserPermission[]
}

export interface UserPermission {
  resource: string
  action: string
  description: string
}

interface AuthContextType {
  user: User | null
  isAuthenticated: boolean
  isLoading: boolean
  login: (username: string, password: string) => Promise<boolean>
  logout: () => void
  refreshToken: () => Promise<boolean>
  hasPermission: (resource: string, action: string) => boolean
}

const AuthContext = createContext<AuthContextType | undefined>(undefined)

export const useAuth = () => {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}

interface AuthProviderProps {
  children: ReactNode
}

export const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    // Check for existing token on app load
    const token = localStorage.getItem('authToken')
    if (token) {
      validateToken(token)
    } else {
      setIsLoading(false)
    }
  }, [])

  const validateToken = async (token: string) => {
    try {
      const result = await AuthService.validateToken(token)
      if (result.success && result.user) {
        setUser(result.user)
      } else {
        localStorage.removeItem('authToken')
        localStorage.removeItem('refreshToken')
      }
    } catch (error) {
      console.error('Token validation failed:', error)
      localStorage.removeItem('authToken')
      localStorage.removeItem('refreshToken')
    } finally {
      setIsLoading(false)
    }
  }

  const login = async (username: string, password: string): Promise<boolean> => {
    try {
      setIsLoading(true)
      const result = await AuthService.login(username, password)
      
      if (result.success && result.token && result.user) {
        localStorage.setItem('authToken', result.token.token)
        localStorage.setItem('refreshToken', result.token.refresh_token)
        setUser(result.user)
        return true
      }
      return false
    } catch (error) {
      console.error('Login failed:', error)
      return false
    } finally {
      setIsLoading(false)
    }
  }

  const logout = () => {
    localStorage.removeItem('authToken')
    localStorage.removeItem('refreshToken')
    setUser(null)
  }

  const refreshToken = async (): Promise<boolean> => {
    try {
      const refreshToken = localStorage.getItem('refreshToken')
      if (!refreshToken) return false

      const result = await AuthService.refreshToken(refreshToken)
      if (result.success && result.token) {
        localStorage.setItem('authToken', result.token.token)
        localStorage.setItem('refreshToken', result.token.refresh_token)
        return true
      }
      return false
    } catch (error) {
      console.error('Token refresh failed:', error)
      return false
    }
  }

  const hasPermission = (resource: string, action: string): boolean => {
    if (!user) return false
    
    return user.permissions.some(
      permission => permission.resource === resource && permission.action === action
    )
  }

  const value: AuthContextType = {
    user,
    isAuthenticated: !!user,
    isLoading,
    login,
    logout,
    refreshToken,
    hasPermission,
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
} 