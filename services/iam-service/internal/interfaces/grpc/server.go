package grpc

import (
	"context"
	"time"

	iamv1 "github.com/fleetops/gen/iam/v1"
	"github.com/fleetops/iam-service/internal/application/commands"
	"github.com/fleetops/iam-service/internal/domain"
	"github.com/fleetops/iam-service/internal/infrastructure/jwt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements iamv1.IAMServiceServer.
type Server struct {
	registerHandler *commands.RegisterHandler
	loginHandler    *commands.LoginHandler
	users           domain.UserRepository
	tokenSvc        *jwt.TokenService
}

func NewServer(
	reg *commands.RegisterHandler,
	login *commands.LoginHandler,
	users domain.UserRepository,
	tokenSvc *jwt.TokenService,
) *Server {
	return &Server{
		registerHandler: reg,
		loginHandler:    login,
		users:           users,
		tokenSvc:        tokenSvc,
	}
}

func (s *Server) Register(ctx context.Context, req *iamv1.RegisterRequest) (*iamv1.RegisterResponse, error) {
	out, err := s.registerHandler.Handle(commands.RegisterInput{
		TenantName:    req.TenantName,
		BillingEmail:  req.BillingEmail,
		AdminName:     req.AdminName,
		AdminEmail:    req.AdminEmail,
		AdminPassword: req.AdminPassword,
	})
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	return &iamv1.RegisterResponse{
		Tenant: domainTenantToProto(out.Tenant),
		Admin:  domainUserToProto(out.Admin),
	}, nil
}

func (s *Server) Login(ctx context.Context, req *iamv1.LoginRequest) (*iamv1.LoginResponse, error) {
	out, err := s.loginHandler.Handle(commands.LoginInput{
		TenantID: req.TenantId,
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, err.Error())
	}

	return &iamv1.LoginResponse{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		ExpiresIn:    out.ExpiresIn,
		User:         domainUserToProto(out.User),
	}, nil
}

func (s *Server) ValidateToken(ctx context.Context, req *iamv1.ValidateTokenRequest) (*iamv1.ValidateTokenResponse, error) {
	claims, err := s.tokenSvc.Validate(req.Token)
	if err != nil {
		return &iamv1.ValidateTokenResponse{Valid: false}, nil
	}

	return &iamv1.ValidateTokenResponse{
		UserId:   claims.Subject,
		TenantId: claims.TenantID,
		Role:     claims.Role,
		Jti:      claims.ID,
		Valid:    true,
	}, nil
}

func (s *Server) RevokeToken(ctx context.Context, req *iamv1.RevokeTokenRequest) (*iamv1.RevokeTokenResponse, error) {
	// TODO: write jti to Redis deny-list with TTL matching token expiry
	return &iamv1.RevokeTokenResponse{Success: true}, nil
}

func (s *Server) RefreshToken(ctx context.Context, req *iamv1.RefreshTokenRequest) (*iamv1.RefreshTokenResponse, error) {
	// Validate refresh token signature
	claims, err := s.tokenSvc.Validate(req.RefreshToken)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	// Re-issue access token with fresh TTL
	newToken, _, err := s.tokenSvc.Sign(claims.Subject, claims.TenantID, claims.Role)
	if err != nil {
		return nil, status.Error(codes.Internal, "could not issue token")
	}

	return &iamv1.RefreshTokenResponse{
		AccessToken: newToken,
		ExpiresIn:   900,
	}, nil
}

func (s *Server) GetUser(ctx context.Context, req *iamv1.GetUserRequest) (*iamv1.User, error) {
	u, err := s.users.FindByID(req.UserId)
	if err != nil {
		return nil, status.Error(codes.NotFound, err.Error())
	}
	return domainUserToProto(u), nil
}

func (s *Server) ListUsers(ctx context.Context, req *iamv1.ListUsersRequest) (*iamv1.ListUsersResponse, error) {
	users, err := s.users.List(req.TenantId, int(req.PageSize), 0)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	proto := make([]*iamv1.User, len(users))
	for i, u := range users {
		proto[i] = domainUserToProto(u)
	}

	return &iamv1.ListUsersResponse{Users: proto}, nil
}

func (s *Server) CreateServiceAccount(_ context.Context, _ *iamv1.CreateServiceAccountRequest) (*iamv1.ServiceAccount, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

func (s *Server) GetServiceAccountToken(_ context.Context, _ *iamv1.ServiceAccountTokenRequest) (*iamv1.ServiceAccountTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// ── proto mappers ─────────────────────────────────────────────────────────────

func domainUserToProto(u *domain.User) *iamv1.User {
	return &iamv1.User{
		Id:        u.ID,
		TenantId:  u.TenantID,
		Email:     u.Email,
		Name:      u.Name,
		Role:      protoRole(u.Role),
		Active:    u.Active,
		CreatedAt: u.CreatedAt.Format(time.RFC3339),
	}
}

func domainTenantToProto(t *domain.Tenant) *iamv1.Tenant {
	return &iamv1.Tenant{
		Id:           t.ID,
		Name:         t.Name,
		BillingEmail: t.BillingEmail,
		Active:       t.Active,
		CreatedAt:    t.CreatedAt.Format(time.RFC3339),
	}
}

func protoRole(r domain.Role) iamv1.Role {
	switch r {
	case domain.RoleAdmin:
		return iamv1.Role_ROLE_ADMIN
	case domain.RoleDispatcher:
		return iamv1.Role_ROLE_DISPATCHER
	case domain.RoleDriver:
		return iamv1.Role_ROLE_DRIVER
	case domain.RoleFinance:
		return iamv1.Role_ROLE_FINANCE
	default:
		return iamv1.Role_ROLE_UNSPECIFIED
	}
}
