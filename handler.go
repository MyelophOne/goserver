package goserver

import "net/http"

func (s *Server) addRoute(method, path string, handler http.HandlerFunc) {
	s.router.Handle(method, path, handler)
}

func (s *Server) GET(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodGet, path, handler)
}

func (s *Server) POST(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodPost, path, handler)
}

func (s *Server) PUT(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodPut, path, handler)
}

func (s *Server) DELETE(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodDelete, path, handler)
}

func (s *Server) HEAD(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodHead, path, handler)
}

func (s *Server) PATCH(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodPatch, path, handler)
}

func (s *Server) OPTIONS(path string, handler http.HandlerFunc) {
	s.addRoute(http.MethodOptions, path, handler)
}

func (s *Server) ANY(path string, handler http.HandlerFunc) {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodHead, http.MethodPatch, http.MethodOptions,
	} {
		s.addRoute(method, path, handler)
	}
}

func (s *Server) Group(prefix string) *RouteGroup {
	return &RouteGroup{
		server:      s,
		prefix:      prefix,
		middlewares: make([]Middleware, 0),
	}
}

func (g *RouteGroup) addRoute(method, p string, handler http.HandlerFunc) {
	full := joinPaths(g.prefix, p)
	h := http.Handler(http.HandlerFunc(handler))
	if len(g.middlewares) > 0 {
		h = chain(h, g.middlewares...)
	}
	g.server.router.Handle(method, full, h)
}

func (g *RouteGroup) GET(p string, handler http.HandlerFunc) { g.addRoute(http.MethodGet, p, handler) }

func (g *RouteGroup) POST(p string, handler http.HandlerFunc) {
	g.addRoute(http.MethodPost, p, handler)
}

func (g *RouteGroup) PUT(p string, handler http.HandlerFunc) { g.addRoute(http.MethodPut, p, handler) }

func (g *RouteGroup) DELETE(p string, handler http.HandlerFunc) {
	g.addRoute(http.MethodDelete, p, handler)
}

func (g *RouteGroup) HEAD(path string, handler http.HandlerFunc) {
	g.addRoute(http.MethodHead, path, handler)
}

func (g *RouteGroup) PATCH(path string, handler http.HandlerFunc) {
	g.addRoute(http.MethodPatch, path, handler)
}

func (g *RouteGroup) OPTIONS(path string, handler http.HandlerFunc) {
	g.addRoute(http.MethodOptions, path, handler)
}

func (g *RouteGroup) ANY(p string, handler http.HandlerFunc) {
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodHead, http.MethodPatch, http.MethodOptions,
	} {
		g.addRoute(method, p, handler)
	}
}

func (s *Server) rejectRequest(w http.ResponseWriter, r *http.Request, code int, msg string) {
	if mode := ParseClientIPLogMode(s.Config.LogClientIP); mode != LogClientIPOff {
		s.Logger.Printf("request rejected: %s (code %d) from %s", msg, code, formatLogClient(GetRealIP(r), mode))
	} else {
		s.Logger.Printf("request rejected: %s (code %d)", msg, code)
	}
	if s.ResponseMode == "json" {
		s.RenderErrorJSON(w, r, code, msg)
	} else {
		s.RenderError(w, r, code, msg)
	}
}
