package auth

import (
	"log/slog"
	"net"
	"net/http"

	"github.com/valeriandwipurnomo/jobseeker/internal/platform/httpx"
)

// Handlers exposes the HTTP surface for authentication.
type Handlers struct {
	Service *Service
	Logger  *slog.Logger
}

// NewHandlers builds the auth handlers.
func NewHandlers(svc *Service, logger *slog.Logger) *Handlers {
	return &Handlers{Service: svc, Logger: logger}
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	User User `json:"user"`
}

// Register handles POST /api/v1/auth/register.
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.Logger, err)
		return
	}
	user, token, err := h.Service.Register(r.Context(), req.Email, req.Password, req.DisplayName, metaFrom(r))
	if err != nil {
		httpx.WriteError(w, r, h.Logger, err)
		return
	}
	h.Service.SetSessionCookie(w, token)
	httpx.WriteJSON(w, http.StatusCreated, userResponse{User: user})
}

// Login handles POST /api/v1/auth/login.
func (h *Handlers) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, h.Logger, err)
		return
	}
	user, token, err := h.Service.Login(r.Context(), req.Email, req.Password, metaFrom(r))
	if err != nil {
		httpx.WriteError(w, r, h.Logger, err)
		return
	}
	h.Service.SetSessionCookie(w, token)
	httpx.WriteJSON(w, http.StatusOK, userResponse{User: user})
}

// Logout handles POST /api/v1/auth/logout.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	raw := ""
	if c, err := r.Cookie(h.Service.CookieName()); err == nil {
		raw = c.Value
	}
	if err := h.Service.Logout(r.Context(), raw); err != nil {
		httpx.WriteError(w, r, h.Logger, err)
		return
	}
	h.Service.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /api/v1/me.
func (h *Handlers) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, h.Logger, httpx.ErrUnauthorized)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, userResponse{User: user})
}

func metaFrom(r *http.Request) SessionMeta {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		ip = host
	}
	return SessionMeta{UserAgent: truncate(r.UserAgent(), 256), IP: ip}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
