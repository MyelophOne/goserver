package goserver

import (
	"net/http"
)

func (s *Server) AddPreloadHeaders(w http.ResponseWriter, resources []string) {
	for _, res := range resources {
		w.Header().Add("Link", res)
	}
}
