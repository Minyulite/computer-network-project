package realtime

import (
	"errors"
	"net/http"

	"example.com/computer-network/project/apps/api/internal/auth"
	"github.com/coder/websocket"
)

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origins := r.Header.Values("Origin")
	if len(origins) != 1 || !h.allowedOrigins[origins[0]] {
		http.Error(w, "Origin is not allowed", http.StatusForbidden)
		return
	}
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}
	identity, err := h.auth.ValidateSession(r.Context(), cookie.Value)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, auth.ErrUnauthenticated) {
			status = http.StatusUnauthorized
		}
		http.Error(w, "Authentication failed", status)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(inboundLimit)
	client := newClient(h, conn, identity)
	if err := h.register(client); err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "Session unavailable")
		return
	}
	client.start()
	<-client.done
}
