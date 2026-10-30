package web

import (
	"net/http"
	"time"
)

const (
	sessionCookie = "session"
	csrfCookie    = "csrf"
)

// cookieNames are __Host prefixed whenever cookies can be secure, which pins
// them to this exact host, path and https. Local http cannot use the prefix.
func (s *Server) sessionCookieName() string {
	if s.secure {
		return "__Host-" + sessionCookie
	}
	return sessionCookie
}

func (s *Server) csrfCookieName() string {
	if s.secure {
		return "__Host-" + csrfCookie
	}
	return csrfCookie
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.sessionCookieName(),
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// setCSRFCookie rotates the double submit token. It is httpOnly because the
// server renders the token into the form; nothing in the browser needs to read it.
func (s *Server) setCSRFCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.csrfCookieName(),
		Value:    token,
		Path:     "/",
		MaxAge:   int((12 * time.Hour).Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}
