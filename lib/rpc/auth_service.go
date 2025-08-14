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

package rpc

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/golang-jwt/jwt/v4"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/adobe/aquarium-fish/lib/auth"
	"github.com/adobe/aquarium-fish/lib/fish"
	"github.com/adobe/aquarium-fish/lib/log"
	aquariumv2 "github.com/adobe/aquarium-fish/lib/rpc/proto/aquarium/v2"
	rpcutil "github.com/adobe/aquarium-fish/lib/rpc/util"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

// AuthService implements the authentication service
type AuthService struct {
	fish            *fish.Fish
	jwtSecret       []byte
	tokenDuration   time.Duration
	refreshSecret   []byte
	refreshDuration time.Duration
}

// JWT claims structure
type JWTClaims struct {
	UserName string   `json:"user_name"`
	Roles    []string `json:"roles"`
	jwt.RegisteredClaims
}

// RefreshTokenClaims structure for refresh tokens
type RefreshTokenClaims struct {
	UserName string `json:"user_name"`
	jwt.RegisteredClaims
}

// NewAuthService creates a new auth service
func NewAuthService(f *fish.Fish) *AuthService {
	// Generate refresh secret
	refreshSecret := make([]byte, 32)
	if _, err := rand.Read(refreshSecret); err != nil {
		panic(fmt.Sprintf("failed to generate refresh secret: %v", err))
	}

	return &AuthService{
		fish:            f,
		jwtSecret:       rpcutil.GetJWTSecret(), // Use shared JWT secret
		tokenDuration:   time.Hour,              // Access token valid for 1 hour
		refreshSecret:   refreshSecret,
		refreshDuration: 24 * time.Hour, // Refresh token valid for 24 hours
	}
}

// Login authenticates a user and returns a JWT token
func (s *AuthService) Login(ctx context.Context, req *connect.Request[aquariumv2.AuthServiceLoginRequest]) (*connect.Response[aquariumv2.AuthServiceLoginResponse], error) {
	logger := log.WithFunc("rpc", "AuthService.Login")
	logger.Debug("Login attempt", "username", req.Msg.GetUsername())

	// Authenticate user
	user := s.fish.DB().UserAuth(ctx, req.Msg.GetUsername(), req.Msg.GetPassword())
	if user == nil {
		logger.Debug("Authentication failed", "username", req.Msg.GetUsername())
		return connect.NewResponse(&aquariumv2.AuthServiceLoginResponse{
			Status:  false,
			Message: "Authentication failed",
		}), nil
	}

	// Generate JWT token
	token, err := s.generateToken(user)
	if err != nil {
		logger.Error("Failed to generate JWT token", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceLoginResponse{
			Status:  false,
			Message: "Failed to generate token",
		}), nil
	}

	// Get user permissions
	session, err := s.buildUserSession(ctx, user)
	if err != nil {
		logger.Error("Failed to build user session", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceLoginResponse{
			Status:  false,
			Message: "Failed to build session",
		}), nil
	}

	logger.Debug("Login successful", "username", req.Msg.GetUsername())
	return connect.NewResponse(&aquariumv2.AuthServiceLoginResponse{
		Status:  true,
		Message: "Login successful",
		Token:   token,
		Session: session,
	}), nil
}

// RefreshToken refreshes an existing JWT token
func (s *AuthService) RefreshToken(ctx context.Context, req *connect.Request[aquariumv2.AuthServiceRefreshTokenRequest]) (*connect.Response[aquariumv2.AuthServiceRefreshTokenResponse], error) {
	logger := log.WithFunc("rpc", "AuthService.RefreshToken")

	// Validate refresh token
	token, err := jwt.ParseWithClaims(req.Msg.GetRefreshToken(), &RefreshTokenClaims{}, func(_ /*token*/ *jwt.Token) (any, error) {
		return s.refreshSecret, nil
	})

	if err != nil || !token.Valid {
		logger.Debug("Invalid refresh token", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceRefreshTokenResponse{
			Status:  false,
			Message: "Invalid refresh token",
		}), nil
	}

	claims, ok := token.Claims.(*RefreshTokenClaims)
	if !ok {
		logger.Error("Invalid refresh token claims")
		return connect.NewResponse(&aquariumv2.AuthServiceRefreshTokenResponse{
			Status:  false,
			Message: "Invalid token claims",
		}), nil
	}

	// Get user from database
	user, err := s.fish.DB().UserGet(ctx, claims.UserName)
	if err != nil {
		logger.Error("Failed to get user", "username", claims.UserName, "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceRefreshTokenResponse{
			Status:  false,
			Message: "User not found",
		}), nil
	}

	// Generate new JWT token
	newToken, err := s.generateToken(user)
	if err != nil {
		logger.Error("Failed to generate new JWT token", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceRefreshTokenResponse{
			Status:  false,
			Message: "Failed to generate token",
		}), nil
	}

	logger.Debug("Token refreshed successfully", "username", claims.UserName)
	return connect.NewResponse(&aquariumv2.AuthServiceRefreshTokenResponse{
		Status:  true,
		Message: "Token refreshed successfully",
		Token:   newToken,
	}), nil
}

// GetPermissions returns the current user's permissions
func (s *AuthService) GetPermissions(ctx context.Context, _ /*req*/ *connect.Request[aquariumv2.AuthServiceGetPermissionsRequest]) (*connect.Response[aquariumv2.AuthServiceGetPermissionsResponse], error) {
	logger := log.WithFunc("rpc", "AuthService.GetPermissions")

	// Get user from context (should be set by auth middleware)
	user := rpcutil.GetUserFromContext(ctx)
	if user == nil {
		logger.Debug("No user found in context")
		return connect.NewResponse(&aquariumv2.AuthServiceGetPermissionsResponse{
			Status:  false,
			Message: "No authenticated user",
		}), nil
	}

	// Build user session with permissions
	session, err := s.buildUserSession(ctx, user)
	if err != nil {
		logger.Error("Failed to build user session", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceGetPermissionsResponse{
			Status:  false,
			Message: "Failed to get permissions",
		}), nil
	}

	logger.Debug("Permissions retrieved successfully", "username", user.Name)
	return connect.NewResponse(&aquariumv2.AuthServiceGetPermissionsResponse{
		Status:  true,
		Message: "Permissions retrieved successfully",
		Session: session,
	}), nil
}

// ValidateToken validates a JWT token
func (s *AuthService) ValidateToken(ctx context.Context, req *connect.Request[aquariumv2.AuthServiceValidateTokenRequest]) (*connect.Response[aquariumv2.AuthServiceValidateTokenResponse], error) {
	logger := log.WithFunc("rpc", "AuthService.ValidateToken")

	// Parse and validate token
	token, err := jwt.ParseWithClaims(req.Msg.GetToken(), &JWTClaims{}, func(_ /*token*/ *jwt.Token) (any, error) {
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		logger.Debug("Invalid JWT token", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceValidateTokenResponse{
			Status:  false,
			Message: "Invalid token",
		}), nil
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		logger.Error("Invalid JWT claims")
		return connect.NewResponse(&aquariumv2.AuthServiceValidateTokenResponse{
			Status:  false,
			Message: "Invalid token claims",
		}), nil
	}

	// Get user from database to ensure they still exist
	user, err := s.fish.DB().UserGet(ctx, claims.UserName)
	if err != nil {
		logger.Error("Failed to get user", "username", claims.UserName, "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceValidateTokenResponse{
			Status:  false,
			Message: "User not found",
		}), nil
	}

	// Build user session
	session, err := s.buildUserSession(ctx, user)
	if err != nil {
		logger.Error("Failed to build user session", "err", err)
		return connect.NewResponse(&aquariumv2.AuthServiceValidateTokenResponse{
			Status:  false,
			Message: "Failed to build session",
		}), nil
	}

	logger.Debug("Token validated successfully", "username", claims.UserName)
	return connect.NewResponse(&aquariumv2.AuthServiceValidateTokenResponse{
		Status:  true,
		Message: "Token is valid",
		Session: session,
	}), nil
}

// generateToken generates a JWT token for the user
func (s *AuthService) generateToken(user *typesv2.User) (*aquariumv2.JWTToken, error) {
	now := time.Now()
	expiresAt := now.Add(s.tokenDuration)
	refreshExpiresAt := now.Add(s.refreshDuration)

	// Create access token
	claims := &JWTClaims{
		UserName: user.Name,
		Roles:    user.Roles,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   user.Name,
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	accessTokenString, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign access token: %w", err)
	}

	// Create refresh token
	refreshClaims := &RefreshTokenClaims{
		UserName: user.Name,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   user.Name,
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString(s.refreshSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to sign refresh token: %w", err)
	}

	return &aquariumv2.JWTToken{
		Token:            accessTokenString,
		ExpiresAt:        timestamppb.New(expiresAt),
		RefreshToken:     refreshTokenString,
		RefreshExpiresAt: timestamppb.New(refreshExpiresAt),
	}, nil
}

// buildUserSession creates a complete user session with permissions
func (s *AuthService) buildUserSession(ctx context.Context, user *typesv2.User) (*aquariumv2.UserSession, error) {
	logger := log.WithFunc("rpc", "AuthService.buildUserSession").With("user", user.Name)
	now := time.Now()

	// Get all available permissions for the user's roles
	enforcer := auth.GetEnforcer()
	if enforcer == nil {
		return nil, fmt.Errorf("RBAC enforcer not available")
	}

	// Build permissions list using actual role permissions from database
	var permissions []*aquariumv2.Permission

	// Create a map to avoid duplicates
	permissionMap := make(map[string]*aquariumv2.Permission)

	// Collect all permissions from user's roles
	for _, roleName := range user.Roles {
		role, err := s.fish.DB().RoleGet(ctx, roleName)
		if err != nil {
			logger.Warn("Unable to get role, skipping", "role", roleName)
			continue
		}
		for _, perm := range role.Permissions {
			key := fmt.Sprintf("%s.%s", perm.Resource, perm.Action)
			if _, exists := permissionMap[key]; !exists {
				permissionMap[key] = perm.ToPermission()
			}
		}
	}

	// Convert map to slice
	for _, perm := range permissionMap {
		permissions = append(permissions, perm)
	}

	return &aquariumv2.UserSession{
		UserName:    user.Name,
		Roles:       user.Roles,
		Permissions: permissions,
		CreatedAt:   timestamppb.New(now),
		LastUsed:    timestamppb.New(now),
	}, nil
}

// ParseJWTToken parses a JWT token and returns the claims
func (s *AuthService) ParseJWTToken(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(_ /*token*/ *jwt.Token) (any, error) {
		return s.jwtSecret, nil
	})

	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}
