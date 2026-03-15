package errors

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Domain error sentinels.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrForbidden     = errors.New("forbidden")
	ErrInvalid       = errors.New("invalid argument")
	ErrInternal      = errors.New("internal error")
)

// NotFound wraps ErrNotFound with context.
func NotFound(entity, id string) error {
	return fmt.Errorf("%w: %s %s", ErrNotFound, entity, id)
}

// AlreadyExists wraps ErrAlreadyExists with context.
func AlreadyExists(entity, id string) error {
	return fmt.Errorf("%w: %s %s", ErrAlreadyExists, entity, id)
}

// Invalid wraps ErrInvalid with a field + reason.
func Invalid(field, reason string) error {
	return fmt.Errorf("%w: %s %s", ErrInvalid, field, reason)
}

// ToGRPC maps domain errors to gRPC status codes.
func ToGRPC(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, ErrUnauthorized):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, ErrInvalid):
		return status.Error(codes.InvalidArgument, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}

// FromGRPC maps a gRPC status error back to a domain error.
func FromGRPC(err error) error {
	if err == nil {
		return nil
	}
	st, ok := status.FromError(err)
	if !ok {
		return err
	}
	switch st.Code() {
	case codes.NotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, st.Message())
	case codes.AlreadyExists:
		return fmt.Errorf("%w: %s", ErrAlreadyExists, st.Message())
	case codes.Unauthenticated:
		return fmt.Errorf("%w: %s", ErrUnauthorized, st.Message())
	case codes.PermissionDenied:
		return fmt.Errorf("%w: %s", ErrForbidden, st.Message())
	case codes.InvalidArgument:
		return fmt.Errorf("%w: %s", ErrInvalid, st.Message())
	default:
		return fmt.Errorf("%w: %s", ErrInternal, st.Message())
	}
}
