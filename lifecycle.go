package goserver

import (
	"context"
	"net/http"
)

func (s *Server) beginWork() bool {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	if s.stopping {
		return false
	}
	s.backgroundWg.Add(1)
	return true
}

func (s *Server) isStopping() bool {
	s.workMu.Lock()
	defer s.workMu.Unlock()
	return s.stopping
}

func (s *Server) shutdown(ctx context.Context, srv *http.Server, closeDependencies func()) error {
	s.workMu.Lock()
	s.stopping = true
	s.workMu.Unlock()
	done := make(chan error, 1)
	go func() {
		cronDone := make(chan struct{})
		go func() {
			defer close(cronDone)
			if s.Cron != nil {
				s.Cron.Stop()
			}
		}()
		err := srv.Shutdown(ctx)
		if err != nil {
			_ = srv.Close()
		}
		<-cronDone
		s.backgroundWg.Wait()
		closeDependencies()
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = srv.Close()
		return ctx.Err()
	}
}
