import { createClient } from '@connectrpc/connect'
import { createConnectTransport } from '@connectrpc/connect-web'
import { AuthService as AuthServiceClient } from '../../gen/aquarium/v2/auth_pb'
import type { User, UserPermission } from '../contexts/AuthContext'

// Create transport for ConnectRPC
const transport = createConnectTransport({
  baseUrl: '/grpc',
  // Add authentication header
  interceptors: [
    (next) => async (req) => {
      const token = localStorage.getItem('authToken')
      if (token) {
        req.header.set('Authorization', `Bearer ${token}`)
      }
      return await next(req)
    }
  ]
})

// Create client
const client = createClient(AuthServiceClient, transport)

export interface LoginResult {
  success: boolean
  message?: string
  token?: {
    token: string
    refresh_token: string
    expires_at: Date
    refresh_expires_at: Date
  }
  user?: User
}

export interface TokenValidationResult {
  success: boolean
  message?: string
  user?: User
}

export interface TokenRefreshResult {
  success: boolean
  message?: string
  token?: {
    token: string
    refresh_token: string
    expires_at: Date
    refresh_expires_at: Date
  }
}

export const AuthService = {
  async login(username: string, password: string): Promise<LoginResult> {
    try {
      const response = await client.login({
        username,
        password,
      })

      if (response.status && response.token && response.session) {
        return {
          success: true,
          message: response.message,
          token: {
            token: response.token.token,
            refresh_token: response.token.refreshToken,
            expires_at: response.token.expiresAt ? response.token.expiresAt.toDate() : new Date(),
            refresh_expires_at: response.token.refreshExpiresAt ? response.token.refreshExpiresAt.toDate() : new Date(),
          },
          user: {
            name: response.session.userName,
            roles: response.session.roles,
            permissions: response.session.permissions.map((p): UserPermission => ({
              resource: p.resource,
              action: p.action,
              description: p.description,
            })),
          },
        }
      }

      return {
        success: false,
        message: response.message || 'Login failed',
      }
    } catch (error) {
      console.error('Login error:', error)
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Login failed',
      }
    }
  },

  async refreshToken(refreshToken: string): Promise<TokenRefreshResult> {
    try {
      const response = await client.refreshToken({
        refreshToken,
      })

      if (response.status && response.token) {
        return {
          success: true,
          message: response.message,
          token: {
            token: response.token.token,
            refresh_token: response.token.refreshToken,
            expires_at: response.token.expiresAt ? response.token.expiresAt.toDate() : new Date(),
            refresh_expires_at: response.token.refreshExpiresAt ? response.token.refreshExpiresAt.toDate() : new Date(),
          },
        }
      }

      return {
        success: false,
        message: response.message || 'Token refresh failed',
      }
    } catch (error) {
      console.error('Token refresh error:', error)
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Token refresh failed',
      }
    }
  },

  async validateToken(token: string): Promise<TokenValidationResult> {
    try {
      const response = await client.validateToken({
        token,
      })

      if (response.status && response.session) {
        return {
          success: true,
          message: response.message,
          user: {
            name: response.session.userName,
            roles: response.session.roles,
            permissions: response.session.permissions.map((p): UserPermission => ({
              resource: p.resource,
              action: p.action,
              description: p.description,
            })),
          },
        }
      }

      return {
        success: false,
        message: response.message || 'Token validation failed',
      }
    } catch (error) {
      console.error('Token validation error:', error)
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Token validation failed',
      }
    }
  },

  async getPermissions(): Promise<TokenValidationResult> {
    try {
      const response = await client.getPermissions({})

      if (response.status && response.session) {
        return {
          success: true,
          message: response.message,
          user: {
            name: response.session.userName,
            roles: response.session.roles,
            permissions: response.session.permissions.map((p): UserPermission => ({
              resource: p.resource,
              action: p.action,
              description: p.description,
            })),
          },
        }
      }

      return {
        success: false,
        message: response.message || 'Failed to get permissions',
      }
    } catch (error) {
      console.error('Get permissions error:', error)
      return {
        success: false,
        message: error instanceof Error ? error.message : 'Failed to get permissions',
      }
    }
  },
} 