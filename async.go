package goserver

func (s *Server) Go(fn func()) {
	if !s.beginWork() {
		return
	}
	go func() {
		defer s.backgroundWg.Done()
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Printf("[async panic] %v", r)
			}
		}()
		fn()
	}()
}
