package goserver

func (s *Server) Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.Logger.Printf("[async panic] %v", r)
			}
		}()
		fn()
	}()
}
