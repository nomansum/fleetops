package auth

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims is the custom JWT payload for FleetOps.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tid"`
	Role     string `json:"role"`
}

// TokenService signs and validates JWTs using RS256.
type TokenService struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	accessTTL  time.Duration
}

// NewTokenService loads RSA keys from env-specified paths.
//
//	JWT_PRIVATE_KEY_PATH — PEM file for signing (iam-service only)
//	JWT_PUBLIC_KEY_PATH  — PEM file for validation (all services)
func NewTokenService() (*TokenService, error) {
	ts := &TokenService{accessTTL: 15 * time.Minute}

	if path := os.Getenv("JWT_PRIVATE_KEY_PATH"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("auth: read private key: %w", err)
		}
		key, err := jwt.ParseRSAPrivateKeyFromPEM(b)
		if err != nil {
			return nil, fmt.Errorf("auth: parse private key: %w", err)
		}
		ts.privateKey = key
		ts.publicKey = &key.PublicKey
	}

	if path := os.Getenv("JWT_PUBLIC_KEY_PATH"); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("auth: read public key: %w", err)
		}
		key, err := jwt.ParseRSAPublicKeyFromPEM(b)
		if err != nil {
			return nil, fmt.Errorf("auth: parse public key: %w", err)
		}
		ts.publicKey = key
	}

	if ts.publicKey == nil {
		return nil, errors.New("auth: JWT_PUBLIC_KEY_PATH is required")
	}

	return ts, nil
}

// Sign creates a signed access token for the given subject.
func (ts *TokenService) Sign(userID, tenantID, role, jti string) (string, error) {
	if ts.privateKey == nil {
		return "", errors.New("auth: private key not loaded — cannot sign")
	}

	now := time.Now()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ts.accessTTL)),
			ID:        jti,
		},
		TenantID: tenantID,
		Role:     role,
	}

	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(ts.privateKey)
}

// Validate parses and verifies a token, returning its claims.
func (ts *TokenService) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("auth: unexpected signing method: %v", t.Header["alg"])
		}
		return ts.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: invalid token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("auth: invalid claims")
	}

	return claims, nil
}
