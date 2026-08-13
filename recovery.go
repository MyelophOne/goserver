package goserver

import (
	"fmt"
	"net/http"
	"runtime/debug"
)

func (s *Server) RecoveryMiddleware(errorHandler http.HandlerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					stack := debug.Stack()
					errStr := fmt.Sprintf("%v", rec)
					fullMessage := fmt.Sprintf("PANIC recovered: %s\n%s", errStr, string(stack))

					if s.Logger != nil {
						s.Logger.Println(fullMessage)
					}

					if s.ErrorNotifier != nil {
						s.ErrorNotifier(http.StatusInternalServerError, r, fullMessage)
					}

					applyErrorHooks(w, r, fmt.Errorf("panic: %v", rec))

					if errorHandler != nil {
						errorHandler(w, r)
					} else {
						http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
