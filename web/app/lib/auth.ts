import { create } from "@bufbuild/protobuf";
import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import {
  AuthService as AuthServiceDef,
  AuthServiceLoginRequestSchema,
  AuthServiceRefreshTokenRequestSchema,
  AuthServiceGetPermissionsRequestSchema,
  AuthServiceValidateTokenRequestSchema,
  type AuthServiceLoginResponse,
  type AuthServiceRefreshTokenResponse,
  type AuthServiceGetPermissionsResponse,
  type AuthServiceValidateTokenResponse,
  type JWTToken,
  type UserSession,
  type UserPermission,
} from "../../gen/aquarium/v2/auth_pb";

// Create the ConnectRPC transport
const transport = createConnectTransport({
  baseUrl: typeof window !== "undefined" ? `${window.location.origin}/grpc` : "http://localhost:8001/grpc",
});

// Create the auth client
const authClient = createClient(AuthServiceDef, transport);

export interface AuthTokens {
  accessToken: string;
  refreshToken: string;
  expiresAt: Date;
  refreshExpiresAt: Date;
}

export interface AuthUser {
  userName: string;
  roles: string[];
  permissions: Array<{
    resource: string;
    action: string;
    description: string;
  }>;
  createdAt?: Date;
  lastUsed?: Date;
}

export interface LoginResult {
  success: boolean;
  message: string;
  tokens?: AuthTokens;
  user?: AuthUser;
}

export interface RefreshResult {
  success: boolean;
  message: string;
  tokens?: AuthTokens;
}

export interface PermissionsResult {
  success: boolean;
  message: string;
  user?: AuthUser;
}

export interface ValidationResult {
  success: boolean;
  message: string;
  user?: AuthUser;
}

// Helper function to convert protobuf timestamp to Date
function timestampToDate(timestamp: any): Date | undefined {
  if (!timestamp) return undefined;
  return new Date(Number(timestamp.seconds) * 1000 + timestamp.nanos / 1000000);
}

// Helper function to convert protobuf JWTToken to AuthTokens
function convertJWTToken(token: JWTToken): AuthTokens {
  return {
    accessToken: token.token,
    refreshToken: token.refreshToken,
    expiresAt: timestampToDate(token.expiresAt) || new Date(Date.now() + 3600000), // 1 hour default
    refreshExpiresAt: timestampToDate(token.refreshExpiresAt) || new Date(Date.now() + 86400000), // 24 hours default
  };
}

// Helper function to convert protobuf UserSession to AuthUser
function convertUserSession(session: UserSession): AuthUser {
  return {
    userName: session.userName,
    roles: session.roles,
    permissions: session.permissions.map((p: UserPermission) => ({
      resource: p.resource,
      action: p.action,
      description: p.description,
    })),
    createdAt: timestampToDate(session.createdAt),
    lastUsed: timestampToDate(session.lastUsed),
  };
}

export class AquariumAuthService {
  /**
   * Login with username and password
   */
  async login(username: string, password: string): Promise<LoginResult> {
    try {
      const request = create(AuthServiceLoginRequestSchema, {
        username,
        password,
      });

      const response = await authClient.login(request);

      const result: LoginResult = {
        success: response.status,
        message: response.message,
      };

      if (response.status && response.token && response.session) {
        result.tokens = convertJWTToken(response.token);
        result.user = convertUserSession(response.session);
      }

      return result;
    } catch (error) {
      console.error("Login error:", error);
      return {
        success: false,
        message: error instanceof Error ? error.message : "Login failed",
      };
    }
  }

  /**
   * Refresh access token using refresh token
   */
  async refreshToken(refreshToken: string): Promise<RefreshResult> {
    try {
      const request = create(AuthServiceRefreshTokenRequestSchema, {
        refreshToken,
      });

      const response = await authClient.refreshToken(request);

      const result: RefreshResult = {
        success: response.status,
        message: response.message,
      };

      if (response.status && response.token) {
        result.tokens = convertJWTToken(response.token);
      }

      return result;
    } catch (error) {
      console.error("Token refresh error:", error);
      return {
        success: false,
        message: error instanceof Error ? error.message : "Token refresh failed",
      };
    }
  }

  /**
   * Get current user permissions
   */
  async getPermissions(): Promise<PermissionsResult> {
    try {
      const request = create(AuthServiceGetPermissionsRequestSchema, {});

      const response = await authClient.getPermissions(request);

      const result: PermissionsResult = {
        success: response.status,
        message: response.message,
      };

      if (response.status && response.session) {
        result.user = convertUserSession(response.session);
      }

      return result;
    } catch (error) {
      console.error("Get permissions error:", error);
      return {
        success: false,
        message: error instanceof Error ? error.message : "Failed to get permissions",
      };
    }
  }

  /**
   * Validate JWT token
   */
  async validateToken(token: string): Promise<ValidationResult> {
    try {
      const request = create(AuthServiceValidateTokenRequestSchema, {
        token,
      });

      const response = await authClient.validateToken(request);

      const result: ValidationResult = {
        success: response.status,
        message: response.message,
      };

      if (response.status && response.session) {
        result.user = convertUserSession(response.session);
      }

      return result;
    } catch (error) {
      console.error("Token validation error:", error);
      return {
        success: false,
        message: error instanceof Error ? error.message : "Token validation failed",
      };
    }
  }
}

// Export singleton instance
export const authService = new AquariumAuthService();

// Token storage helpers
export const tokenStorage = {
  getTokens(): AuthTokens | null {
    if (typeof window === "undefined") return null;

    const stored = localStorage.getItem("aquarium_auth_tokens");
    if (!stored) return null;

    try {
      const tokens = JSON.parse(stored);
      return {
        ...tokens,
        expiresAt: new Date(tokens.expiresAt),
        refreshExpiresAt: new Date(tokens.refreshExpiresAt),
      };
    } catch {
      return null;
    }
  },

  setTokens(tokens: AuthTokens): void {
    if (typeof window === "undefined") return;

    localStorage.setItem("aquarium_auth_tokens", JSON.stringify(tokens));
  },

  clearTokens(): void {
    if (typeof window === "undefined") return;

    localStorage.removeItem("aquarium_auth_tokens");
  },

  isTokenExpired(token: AuthTokens): boolean {
    return new Date() >= token.expiresAt;
  },

  isRefreshTokenExpired(token: AuthTokens): boolean {
    return new Date() >= token.refreshExpiresAt;
  },
};
