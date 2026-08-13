package goserver

import (
	"fmt"
	"net/http"
)

type CookieOptions struct {
	Path     string
	Domain   string
	MaxAge   int
	SameSite http.SameSite
	Secure   bool
	HttpOnly bool
}

func (s *Server) GetCookie(r *http.Request, name string) (*http.Cookie, error) {
	c, err := r.Cookie(name)
	if err != nil {
		return nil, ErrCookieNotFound
	}
	if name == "session_id" {
		dec, err := DecryptString(c.Value, s.Config.sessionKey)
		if err != nil {
			return nil, ErrSessionInvalid
		}
		c.Value = dec
	}
	return c, nil
}

func (s *Server) SetCookie(w http.ResponseWriter, name string, value any, options CookieOptions) {
	var val string
	switch v := value.(type) {
	case string:
		val = v
	default:
		val = fmt.Sprintf("%v", v)
	}
	if name == "session_id" {
		enc, err := EncryptString(val, s.Config.sessionKey)
		if err != nil {
			http.Error(w, "encryption error", http.StatusInternalServerError)
			return
		}
		val = enc
	}
	cookie := &http.Cookie{
		Name:     name,
		Value:    val,
		Path:     "/",
		MaxAge:   options.MaxAge,
		Secure:   options.Secure,
		HttpOnly: options.HttpOnly,
		SameSite: options.SameSite,
	}
	http.SetCookie(w, cookie)
}

func (s *Server) SetCookieOnce(w http.ResponseWriter, r *http.Request, name string, value any, options CookieOptions) {
	if _, err := r.Cookie(name); err == nil {
		return
	}
	s.SetCookie(w, name, value, options)
}

func (s *Server) SetSessionCookie(w http.ResponseWriter) (string, error) {
	newID := GenerateSessionID()
	plainID := string(newID)
	enc, err := EncryptString(plainID, s.Config.sessionKey)
	if err != nil {
		return "", fmt.Errorf("failed to encrypt session ID: %w", err)
	}
	cookie := &http.Cookie{
		Name:     "session_id",
		Value:    enc,
		Path:     "/",
		MaxAge:   3600,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
	http.SetCookie(w, cookie)
	return plainID, nil
}

func RemoveCookie(w http.ResponseWriter, name string) {
	cookie := &http.Cookie{
		Name:   name,
		Value:  "",
		MaxAge: -1,
		Path:   "/",
	}
	http.SetCookie(w, cookie)
}

func RemoveAllCookies(w http.ResponseWriter, r *http.Request) {
	for _, c := range r.Cookies() {
		http.SetCookie(w, &http.Cookie{
			Name:   c.Name,
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		if c.Domain != "" {
			http.SetCookie(w, &http.Cookie{
				Name:   c.Name,
				Value:  "",
				Path:   "/",
				Domain: c.Domain,
				MaxAge: -1,
			})
		}
		http.SetCookie(w, &http.Cookie{
			Name:   c.Name,
			Value:  "",
			Path:   r.URL.Path,
			MaxAge: -1,
		})
	}
}
