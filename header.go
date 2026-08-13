package goserver

import "net/http"

func (s *Server) SetHeaders(w http.ResponseWriter, headers map[string]string) {
	for key, value := range headers {
		if key == "" {
			continue
		}
		w.Header().Set(key, value)
	}
}

func (s *Server) MergeHeaders(w http.ResponseWriter, headers map[string]string) {
	for key, value := range headers {
		if _, exists := w.Header()[key]; !exists {
			w.Header().Set(key, value)
		}
	}
}
