package goserver

import (
	"context"
	"net/http"
)

type Params map[string]string

func (p Params) Get(key string) string {
	if val, ok := p[key]; ok {
		return val
	}
	return ""
}

func (s *Server) GetParams(r *http.Request) Params {
	if r == nil {
		return Params{}
	}
	ctx := r.Context()
	if ctx == nil {
		return Params{}
	}
	if v := ctx.Value(ctxParamsKey); v != nil {
		if params, ok := v.(map[string]string); ok {
			return params
		}
	}
	return Params{}
}

func WithParams(r *http.Request, params map[string]string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxParamsKey, params)
	return r.WithContext(ctx)
}
