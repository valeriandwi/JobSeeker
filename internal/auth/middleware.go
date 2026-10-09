package auth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
)

type userCtxKey struct{}

// WithUser stores the authenticated user on the context.
func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userCtxKey{}, u)
}

// UserFromContext returns the authenticated user, if the request was
// authenticated.
func UserFromContext(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userCtxKey{}).(User)
	return u, ok
}

// MustUser panics when no user is present; use only on routes behind Require.
func MustUser(ctx context.Context) User {
	u, ok := UserFromContext(ctx)
	if !ok {
		panic("auth: no user on context — route is missing Require middleware")
	}
	return u
}

// Require rejects unauthenticated requests with 401 and attaches the user.
func (s *Service) Require(logger *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := s.tokenFromRequest(r)
			user, err := s.Authenticate(r.Context(), raw)
			if err != nil {
				httpx.WriteError(w, r, logger, httpx.ErrUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithUser(r.Context(), user)))
		})
	}
}

// Optional attaches the user when a valid session exists but never rejects.
func (s *Service) Optional(logger *slog.Logger) httpx.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if raw := s.tokenFromRequest(r); raw != "" {
				if user, err := s.Authenticate(r.Context(), raw); err == nil {
					r = r.WithContext(WithUser(r.Context(), user))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// tokenFromRequest extracts the session token from the cookie only. Tokens are
// never accepted from headers or query strings, which keeps them out of logs
// and referrers.
func (s *Service) tokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(s.cfg.CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
