// Package middleware contains Gin middleware for the api-gateway.
package middleware

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"

	iamv1 "github.com/fleetops/gen/iam/v1"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// AuthMiddleware validates JWTs by calling iam-service.ValidateToken.
// Results are cached in Redis for 30 seconds per token hash to reduce latency.
//
// On success it sets the following keys in the Gin context:
//
//	user_id   — authenticated user ID
//	tenant_id — tenant ID from the JWT
//	role      — user role
//	jti       — JWT ID (used for revocation checks)
func AuthMiddleware(iamClient iamv1.IAMServiceClient, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "missing or invalid Authorization header"})
			return
		}

		token := strings.TrimPrefix(header, "Bearer ")
		ctx := c.Request.Context()

		// ── Redis cache ───────────────────────────────────────────────────────
		cacheKey := fmt.Sprintf("jwt:%x", sha256.Sum256([]byte(token)))

		if cached, err := rdb.HGetAll(ctx, cacheKey).Result(); err == nil && len(cached) > 0 {
			c.Set("user_id", cached["user_id"])
			c.Set("tenant_id", cached["tenant_id"])
			c.Set("role", cached["role"])
			c.Set("jti", cached["jti"])
			c.Next()
			return
		}

		// ── Call iam-service ──────────────────────────────────────────────────
		rpcCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		resp, err := iamClient.ValidateToken(rpcCtx, &iamv1.ValidateTokenRequest{Token: token})
		if err != nil || !resp.Valid {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
			return
		}

		// Cache the result
		_ = rdb.HSet(ctx, cacheKey, map[string]any{
			"user_id":   resp.UserId,
			"tenant_id": resp.TenantId,
			"role":      resp.Role,
			"jti":       resp.Jti,
		})
		_ = rdb.Expire(ctx, cacheKey, 30*time.Second)

		c.Set("user_id", resp.UserId)
		c.Set("tenant_id", resp.TenantId)
		c.Set("role", resp.Role)
		c.Set("jti", resp.Jti)
		c.Next()
	}
}

// RequireRole aborts with 403 if the authenticated user's role is not in the allowed set.
func RequireRole(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]bool, len(roles))
	for _, r := range roles {
		allowed[r] = true
	}
	return func(c *gin.Context) {
		role, _ := c.Get("role")
		if !allowed[fmt.Sprint(role)] {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
			return
		}
		c.Next()
	}
}

// RateLimitMiddleware implements a sliding window rate limit using Redis.
// Allows maxRequests per window per user.
func RateLimitMiddleware(rdb *redis.Client, maxRequests int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		key := fmt.Sprintf("rl:%v", userID)
		ctx := c.Request.Context()

		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			c.Next() // fail open on Redis error
			return
		}

		if count == 1 {
			_ = rdb.Expire(ctx, key, window)
		}

		if count > int64(maxRequests) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "rate limit exceeded"})
			return
		}

		c.Next()
	}
}
