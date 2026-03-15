package jwt

import (
	"crypto/rsa"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	accessTTL  = 15 * time.Minute
	refreshTTL = 7 * 24 * time.Hour
)

// Claims is the custom JWT payload for FleetOps access tokens.
type Claims struct {
	jwt.RegisteredClaims
	TenantID string `json:"tid"`
	Role     string `json:"role"`
}

// TokenService signs and validates JWTs using RS256.
type TokenService struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
}

// NewTokenService loads RSA keys from env-specified paths.
//
//	JWT_PRIVATE_KEY_PATH — PEM file (required for iam-service which issues tokens)
//	JWT_PUBLIC_KEY_PATH  — PEM file (used by all services for validation)
func NewTokenService() (*TokenService, error) {
	ts := &TokenService{}

	privPath := os.Getenv("JWT_PRIVATE_KEY_PATH")
	pubPath := os.Getenv("JWT_PUBLIC_KEY_PATH")

	if privPath != "" {
		b, err := os.ReadFile(privPath)
		if err != nil {
			return nil, fmt.Errorf("jwt: read private key: %w", err)
		}
		key, err := jwt.ParseRSAPrivateKeyFromPEM(b)
		if err != nil {
			return nil, fmt.Errorf("jwt: parse private key: %w", err)
		}
		ts.privateKey = key
		ts.publicKey = &key.PublicKey // derive public from private
	}

	if pubPath != "" {
		b, err := os.ReadFile(pubPath)
		if err != nil {
			return nil, fmt.Errorf("jwt: read public key: %w", err)
		}
		key, err := jwt.ParseRSAPublicKeyFromPEM(b)
		if err != nil {
			return nil, fmt.Errorf("jwt: parse public key: %w", err)
		}
		ts.publicKey = key
	}

	if ts.publicKey == nil {
		return nil, errors.New("jwt: at least JWT_PUBLIC_KEY_PATH or JWT_PRIVATE_KEY_PATH is required")
	}

	return ts, nil
}

// Sign issues a new RS256 access token. Returns (tokenString, jti, error).
func (ts *TokenService) Sign(userID, tenantID, role string) (string, string, error) {
	if ts.privateKey == nil {
		return "", "", errors.New("jwt: private key not loaded")
	}

	jti := uuid.NewString()
	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(accessTTL)),
			ID:        jti,
		},
		TenantID: tenantID,
		Role:     role,
	}

	token, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(ts.privateKey)
	return token, jti, err
}

// SignRefresh issues a long-lived refresh token (opaque JWT, no role claim).
func (ts *TokenService) SignRefresh(userID, parentJTI string) (string, error) {
	if ts.privateKey == nil {
		return "", errors.New("jwt: private key not loaded")
	}

	now := time.Now().UTC()
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(refreshTTL)),
		ID:        uuid.NewString(),
	}

	return jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(ts.privateKey)
}

// Validate parses and verifies an access token, returning its claims.
func (ts *TokenService) Validate(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("jwt: unexpected alg %v", t.Header["alg"])
		}
		return ts.publicKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt: invalid token: %w", err)
	}

	c, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("jwt: invalid claims")
	}

	return c, nil
}
