package commands

import (
	"errors"
	"fmt"

	"github.com/fleetops/iam-service/internal/domain"
	"github.com/fleetops/iam-service/internal/infrastructure/jwt"
	"golang.org/x/crypto/bcrypt"
)

// LoginInput is the command payload for user authentication.
type LoginInput struct {
	TenantID string
	Email    string
	Password string
}

// LoginOutput holds the issued tokens and the authenticated user.
type LoginOutput struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64 // seconds
	User         *domain.User
}

// LoginHandler handles the Login command.
type LoginHandler struct {
	users    domain.UserRepository
	tokenSvc *jwt.TokenService
}

func NewLoginHandler(users domain.UserRepository, tokenSvc *jwt.TokenService) *LoginHandler {
	return &LoginHandler{users: users, tokenSvc: tokenSvc}
}

func (h *LoginHandler) Handle(in LoginInput) (*LoginOutput, error) {
	user, err := h.users.FindByEmail(in.TenantID, in.Email)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	if !user.Active {
		return nil, errors.New("account suspended")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)); err != nil {
		return nil, errors.New("invalid credentials")
	}

	accessToken, jti, err := h.tokenSvc.Sign(user.ID, user.TenantID, string(user.Role))
	if err != nil {
		return nil, fmt.Errorf("login: sign token: %w", err)
	}

	refreshToken, err := h.tokenSvc.SignRefresh(user.ID, jti)
	if err != nil {
		return nil, fmt.Errorf("login: sign refresh: %w", err)
	}

	return &LoginOutput{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    900, // 15 minutes
		User:         user,
	}, nil
}
