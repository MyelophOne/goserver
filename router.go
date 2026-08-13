package goserver

import (
	"context"
	"net/http"
	"path"
	"strings"
)

type Router struct {
	static   map[string]map[string]http.Handler
	dyn      map[string][]*compiledRoute
	notFound http.Handler
}

type compiledRoute struct {
	handler  http.Handler
	pattern  string
	segments []segment
}

type segmentType int

type segment struct {
	lit  string
	name string
	typ  segmentType
}

type RouteGroup struct {
	server      *Server
	prefix      string
	middlewares []Middleware
}

const (
	segStatic segmentType = iota
	segParam
	segWildcard
)

var ctxParamsKey = &struct{}{}

func NewRouter() *Router {
	return &Router{
		static:   make(map[string]map[string]http.Handler),
		dyn:      make(map[string][]*compiledRoute),
		notFound: http.NotFoundHandler(),
	}
}

func (r *Router) SetNotFoundHandler(h http.HandlerFunc) {
	if h == nil {
		r.notFound = http.NotFoundHandler()
		return
	}
	r.notFound = http.HandlerFunc(h)
}

func (r *Router) Handle(method, pattern string, handler http.Handler) {
	m := strings.ToUpper(method)
	p := cleanPath(pattern)
	if p == "" {
		p = "/"
	}
	if !strings.Contains(p, ":") && !strings.Contains(p, "*") {
		if _, ok := r.static[m]; !ok {
			r.static[m] = make(map[string]http.Handler)
		}
		r.static[m][p] = handler
		return
	}
	cr := &compiledRoute{
		pattern:  p,
		segments: compilePattern(p),
		handler:  handler,
	}
	r.dyn[m] = append(r.dyn[m], cr)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	m := req.Method
	p := cleanPath(req.URL.Path)
	if p == "" {
		p = "/"
	}
	if mm, ok := r.static[m]; ok {
		if h, ok2 := mm[p]; ok2 {
			h.ServeHTTP(w, req)
			return
		}
	}
	if routes, ok := r.dyn[m]; ok {
		for _, cr := range routes {
			if params, ok2 := matchSegments(cr.segments, p); ok2 {
				if len(params) > 0 {
					req = req.WithContext(context.WithValue(req.Context(), ctxParamsKey, params))
				}
				cr.handler.ServeHTTP(w, req)
				return
			}
		}
	}
	if m == http.MethodOptions {
		if r.serveOptionsIfKnown(w, req, p) {
			return
		}
	}
	r.notFound.ServeHTTP(w, req)
}

func (r *Router) serveOptionsIfKnown(w http.ResponseWriter, _ *http.Request, p string) bool {
	allowed := make([]string, 0, 8)
	for method, mm := range r.static {
		if _, ok := mm[p]; ok {
			allowed = append(allowed, method)
		}
	}
	for method, list := range r.dyn {
		for _, cr := range list {
			if ok := fastMatchNoParams(cr.segments, p); ok {
				allowed = append(allowed, method)
				break
			}
		}
	}
	if len(allowed) == 0 {
		return false
	}
	w.Header().Set("Allow", strings.Join(allowed, ", "))
	w.WriteHeader(http.StatusNoContent)
	return true
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	if p[0] != '/' {
		p = "/" + p
	}
	cp := path.Clean(p)
	if cp != "/" {
		cp = strings.TrimRight(cp, "/")
		if cp == "" {
			cp = "/"
		}
	}
	return cp
}

func compilePattern(p string) []segment {
	if p == "/" {
		return []segment{{typ: segStatic, lit: "/"}}
	}
	parts := strings.Split(strings.Trim(p, "/"), "/")
	segs := make([]segment, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			segs = append(segs, segment{typ: segParam, name: part[1:]})
			continue
		}
		if strings.HasPrefix(part, "*") {
			name := ""
			if len(part) > 1 {
				name = part[1:]
			}
			segs = append(segs, segment{typ: segWildcard, name: name})
			break
		}
		segs = append(segs, segment{typ: segStatic, lit: part})
	}
	return segs
}

func splitPath(p string) []string {
	if p == "/" {
		return []string{"/"}
	}
	return strings.Split(strings.Trim(p, "/"), "/")
}

func matchSegments(segs []segment, p string) (map[string]string, bool) {
	params := make(map[string]string)
	if len(segs) == 1 && segs[0].typ == segStatic && segs[0].lit == "/" {
		if p == "/" {
			return params, true
		}
		return nil, false
	}
	parts := splitPath(p)
	si, pi := 0, 0
	for si < len(segs) && pi < len(parts) {
		s := segs[si]
		switch s.typ {
		case segStatic:
			if parts[pi] != s.lit {
				return nil, false
			}
			si++
			pi++
		case segParam:
			params[s.name] = parts[pi]
			si++
			pi++
		case segWildcard:
			if s.name != "" {
				params[s.name] = strings.Join(parts[pi:], "/")
			}
			return params, true
		}
	}
	if si == len(segs) && pi == len(parts) {
		return params, true
	}
	return nil, false
}

func fastMatchNoParams(segs []segment, p string) bool {
	if len(segs) == 1 && segs[0].typ == segStatic && segs[0].lit == "/" {
		return p == "/"
	}
	parts := splitPath(p)
	si, pi := 0, 0
	for si < len(segs) && pi < len(parts) {
		s := segs[si]
		switch s.typ {
		case segStatic:
			if parts[pi] != s.lit {
				return false
			}
			si++
			pi++
		case segParam:
			si++
			pi++
		case segWildcard:
			return true
		}
	}
	return si == len(segs) && pi == len(parts)
}

func joinPaths(prefix, p string) string {
	if prefix == "" || prefix == "/" {
		return cleanPath(p)
	}
	if p == "" || p == "/" {
		return cleanPath(prefix)
	}
	return cleanPath(prefix + "/" + strings.TrimLeft(p, "/"))
}

func chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}
