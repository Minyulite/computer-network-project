package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"
	"unicode/utf8"

	"example.com/computer-network/project/apps/api/internal/auth"
	"example.com/computer-network/project/apps/api/internal/config"
	"example.com/computer-network/project/apps/api/internal/store"
)

const sessionCookieName = auth.SessionCookieName
const authBodyLimit = 8 * 1024

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func readCredentials(w http.ResponseWriter, r *http.Request) (credentials, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use application/json")
		return credentials{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, authBodyLimit)
	data, err := io.ReadAll(r.Body)
	if err != nil || !utf8.Valid(data) {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request")
		return credentials{}, false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var value credentials
	if err := decoder.Decode(&value); err != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request")
		return credentials{}, false
	}
	return value, true
}

func signupHandler(service *auth.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, ok := readCredentials(w, r)
		if !ok {
			return
		}
		result, err := service.Signup(r.Context(), value.Username, value.Password, remoteIP(r), cookieToken(r))
		if err != nil {
			authError(w, err)
			return
		}
		setSessionCookie(w, r, cfg, result.Token, result.ExpiresAt)
		writeJSON(w, http.StatusCreated, struct {
			User auth.User `json:"user"`
		}{result.User})
	}
}

func loginHandler(service *auth.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		value, ok := readCredentials(w, r)
		if !ok {
			return
		}
		result, err := service.Login(r.Context(), value.Username, value.Password, remoteIP(r), cookieToken(r))
		if err != nil {
			authError(w, err)
			return
		}
		setSessionCookie(w, r, cfg, result.Token, result.ExpiresAt)
		writeJSON(w, http.StatusOK, struct {
			User auth.User `json:"user"`
		}{result.User})
	}
}

func meHandler(service *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		identity, err := service.ValidateSession(r.Context(), cookieToken(r))
		if err != nil {
			authError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			User      auth.User `json:"user"`
			ExpiresAt time.Time `json:"expiresAt"`
		}{identity.User, identity.ExpiresAt})
	}
}

func logoutHandler(service *auth.Service, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := service.Logout(r.Context(), cookieToken(r)); err != nil {
			authError(w, err)
			return
		}
		clearSessionCookie(w, r, cfg)
		w.WriteHeader(http.StatusNoContent)
	}
}

func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, cfg config.Config, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: cfg.CookieSecure || r.TLS != nil,
		Expires: expires, MaxAge: int((7 * 24 * time.Hour).Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: cfg.CookieSecure || r.TLS != nil,
		Expires: time.Unix(0, 0), MaxAge: -1,
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid_input", "Invalid username or password")
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid credentials")
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Authentication required")
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "Username is already taken")
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", "Too many attempts")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "Request failed")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
