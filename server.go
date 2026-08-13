package goserver

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/http2"
)

type Middleware func(http.Handler) http.Handler

type ServerStats struct {
	StartTime         time.Time
	ActiveConnections int64
	TotalRequests     int64
	Errors4xx         int64
	Errors5xx         int64
}

type Server struct {
	stats           ServerStats
	connPool        sync.Pool
	fileServer      http.Handler
	Cache           CacheStore
	srv             *http.Server
	router          *Router
	notFoundHandler http.HandlerFunc
	errorHandler    http.HandlerFunc
	Logger          *log.Logger
	errorTmpl       *template.Template
	ErrorNotifier   func(statusCode int, r *http.Request, message string)
	wsHub           *WebSocketHub
	I18n            *I18n
	JWT             *JWTManager
	Cron            *CronManager
	addr            string
	wsPort          string
	ResponseMode    string
	middlewares     []Middleware
	Config          Config
	backgroundWg    sync.WaitGroup
}

func NewServer(addr string) *Server {
	r := NewRouter()
	safeWriter := &SanitizedWriter{
		Target: os.Stdout,
	}
	s := &Server{
		addr:        addr,
		router:      r,
		fileServer:  http.FileServer(http.Dir("./assets")),
		middlewares: make([]Middleware, 0),
		Logger:      log.New(safeWriter, "[goserver] ", log.LstdFlags),
		stats: ServerStats{
			StartTime: time.Now(),
		},
		connPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, 4096)
			},
		},
		srv: &http.Server{
			Addr:              addr,
			MaxHeaderBytes:    GetEnvBytesInt("MaxHeaderBytes", 1<<20),
			ReadTimeout:       GetEnvDuration("READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      GetEnvDuration("WRITE_TIMEOUT", 15*time.Second),
			IdleTimeout:       GetEnvDuration("IDLE_TIMEOUT", 90*time.Second),
			ReadHeaderTimeout: GetEnvDuration("READ_HEADER_TIMEOUT", 500*time.Millisecond),
		},
		ResponseMode: GetResponseMode(),
		I18n:         nil,
	}
	s.notFoundHandler = s.defaultNotFound
	s.errorHandler = s.defaultInternalError
	s.router.SetNotFoundHandler(s.notFoundHandler)
	s.srv.SetKeepAlivesEnabled(true)

	s.loadConfig()

	s.Cron = NewCronManager(s.Logger)
	s.JWT = NewJWTManager(s.Config.JWTSecret)

	s.srv.Handler = s.router
	h2s := &http2.Server{
		MaxConcurrentStreams:         1000,
		MaxReadFrameSize:             1 << 20,
		PermitProhibitedCipherSuites: false,
		IdleTimeout:                  s.Config.IdleTimeout,
		PingTimeout:                  s.Config.PingTimeout,
		WriteByteTimeout:             s.Config.WriteTimeout,
		CountError:                   nil,
	}

	if err := http2.ConfigureServer(s.srv, h2s); err != nil {
		s.Logger.Printf("configure HTTP/2 server: %v", err)
		panic(err)
	}

	return s
}

func (s *Server) SetLogger(l *log.Logger) {
	s.Logger = l
}

func (s *Server) SetNotFoundHandler(h http.HandlerFunc) {
	s.notFoundHandler = h
	s.router.SetNotFoundHandler(h)
}

func (s *Server) SetErrorHandler(h http.HandlerFunc) {
	s.errorHandler = h
}

func (s *Server) Use(m Middleware) {
	s.middlewares = append(s.middlewares, m)
}

func (g *RouteGroup) Use(m Middleware) {
	g.middlewares = append(g.middlewares, m)
}

func (s *Server) GetStats() ServerStats {
	return ServerStats{
		ActiveConnections: atomic.LoadInt64(&s.stats.ActiveConnections),
		TotalRequests:     atomic.LoadInt64(&s.stats.TotalRequests),
		Errors4xx:         atomic.LoadInt64(&s.stats.Errors4xx),
		Errors5xx:         atomic.LoadInt64(&s.stats.Errors5xx),
		StartTime:         s.stats.StartTime,
	}
}

func (s *Server) Start() error {
	if err := s.loadTemplates(); err != nil {
		return err
	}

	s.Logger.Println("goserver is licensed under PolyForm Noncommercial 1.0.0. Copyright (c) 2026 Aliaksandr Ivanou. With ❤️  from @myeloph.one")

	s.Logger.Printf("%s server (ver: %s, %d CPU cores) starting on %s with config: %+v",
		AppEnv, AppVersion, runtime.NumCPU(), s.addr, s.Config)

	runtime.GOMAXPROCS(runtime.NumCPU())

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)

	var currentHTTP *http.Server

	httpSrv, _, err := s.bootServer(s.Config)
	if err != nil {
		return err
	}
	currentHTTP = httpSrv

	for {
		sig := <-stop
		switch sig {

		case os.Interrupt, syscall.SIGTERM:
			s.Logger.Println("shutting down server gracefully...")

			applyShutdownHooks()

			ctx, cancel := context.WithTimeout(context.Background(), s.Config.ShutdownTimeout)
			defer cancel()

			if err := currentHTTP.Shutdown(ctx); err != nil {
				s.Logger.Printf("http shutdown error: %v", err)
			}

			s.Cron.Stop()

			bgDone := make(chan struct{})
			go func() {
				s.backgroundWg.Wait()
				close(bgDone)
			}()

			select {
			case <-bgDone:
				s.Logger.Println("[background] tasks finished")
			case <-ctx.Done():
				s.Logger.Println("timeout waiting for background tasks (forced exit)")
			}

			stats := s.GetStats()
			uptime := time.Since(stats.StartTime)
			s.Logger.Printf("[stats] server uptime: %v, Total requests: %d, 4xx errors: %d, 5xx errors: %d",
				uptime, stats.TotalRequests, stats.Errors4xx, stats.Errors5xx)

			s.Logger.Println("server stopped")
			return nil

		case syscall.SIGHUP:
			s.Logger.Println("received SIGHUP — reloading server...")

			newHTTP, _, errHttp := s.bootServer(s.Config)
			if errHttp != nil {
				s.Logger.Printf("reload failed (start error): %v", errHttp)
				continue
			}

			oldHTTP := currentHTTP
			currentHTTP = newHTTP

			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), s.Config.ShutdownTimeout)
				defer cancel()

				s.Logger.Println("shutting down old server...")
				if err := oldHTTP.Shutdown(ctx); err != nil {
					s.Logger.Printf("old server shutdown error: %v", err)
				}
				s.Cron.Stop()
				s.Logger.Println("old server stopped gracefully")
			}()

			s.Logger.Println("reload completed successfully")
		}
	}
}

func (s *Server) bootServer(cfg Config) (*http.Server, net.Listener, error) {
	ln, err := newOptimizedListener(s.addr)
	if err != nil {
		return nil, nil, err
	}

	handler := s.buildHandler(cfg)

	cop := http.NewCrossOriginProtection()
	mainHandler := cop.Handler(handler)

	finalHandler := mainHandler

	if s.wsHub != nil {
		next := mainHandler

		finalHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/ws" {
				origin := r.Header.Get("Origin")

				if origin != "" {
					if !isValidOrigin(origin, r.Host) {
						s.rejectRequest(w, r, http.StatusForbidden, "Origin not allowed")
						s.Logger.Printf("WS blocked bad origin: %s", origin)
						return
					}
				}

				defer func() {
					if rec := recover(); rec != nil {
						atomic.AddInt64(&s.stats.Errors5xx, 1)
						s.Logger.Printf("ws panic recovered: %v", rec)
					}
				}()
				s.wsHub.HandleWebSocket(w, r)
				return
			}

			next.ServeHTTP(w, r)
		})
	}

	srv := &http.Server{
		Addr:              s.addr,
		Handler:           finalHandler,
		ReadTimeout:       cfg.ReadTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
		ConnState:         s.newConnStateCallback(cfg),
	}

	srv.SetKeepAlivesEnabled(true)

	h2s := &http2.Server{
		MaxConcurrentStreams:         1000,
		MaxReadFrameSize:             1 << 20,
		PermitProhibitedCipherSuites: false,
		IdleTimeout:                  cfg.IdleTimeout,
		PingTimeout:                  cfg.PingTimeout,
		WriteByteTimeout:             cfg.WriteTimeout,
		CountError:                   nil,
	}
	if err := http2.ConfigureServer(s.srv, h2s); err != nil {
		s.Logger.Printf("configure HTTP/2 server: %v", err)
		panic(err)
	}

	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			atomic.AddInt64(&s.stats.Errors5xx, 1)
			s.Logger.Printf("server error: %v", err)
		}
	}()

	return srv, ln, nil
}

func (s *Server) buildHandler(cfg Config) http.Handler {
	var handler http.Handler = s.router

	if cfg.APIPrefix != "" {
		s.Logger.Printf("using routes without API prefix: %s", cfg.APIPrefix)
		handler = http.StripPrefix(cfg.APIPrefix, handler)
	}

	compiledHandler := s.compileMiddlewareChain(handler)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&s.stats.TotalRequests, 1)

		if len(r.URL.String()) > cfg.MaxURLLength {
			s.rejectRequest(w, r, http.StatusRequestURITooLong, "URL too long")
			return
		}

		if len(r.Header) > cfg.MaxHeaders {
			s.rejectRequest(w, r, http.StatusRequestHeaderFieldsTooLarge, "Too many headers")
			return
		}

		if !isRequestMethod(r.Method) {
			s.rejectRequest(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
			return
		}

		if cfg.MaxBodySize > 0 && r.ContentLength > cfg.MaxBodySize {
			s.rejectRequest(w, r, http.StatusRequestEntityTooLarge, "Request body too large")
			return
		}

		applyBeforeHooks(w, r)
		compiledHandler.ServeHTTP(w, r)
		applyAfterHooks(w, r)
	})
}

func (s *Server) compileMiddlewareChain(final http.Handler) http.Handler {
	handler := final
	for i := len(s.middlewares) - 1; i >= 0; i-- {
		handler = s.middlewares[i](handler)
	}
	return handler
}

func (s *Server) newConnStateCallback(cfg Config) func(net.Conn, http.ConnState) {
	var activeConns int64

	return func(c net.Conn, cs http.ConnState) {
		switch cs {
		case http.StateNew:
			current := atomic.AddInt64(&activeConns, 1)
			atomic.StoreInt64(&s.stats.ActiveConnections, current)

			if current > cfg.MaxConnections {
				atomic.AddInt64(&activeConns, -1)
				atomic.StoreInt64(&s.stats.ActiveConnections, atomic.LoadInt64(&activeConns))

				if current%100 == 0 {
					s.Logger.Printf("connection limit reached: %d/%d", current, cfg.MaxConnections)
				}

				if err := c.Close(); err != nil {
					if !isIgnorableCloseError(err) {
						s.Logger.Printf("failed to close connection: %v", err)
					}
				}
				return
			}

			if cfg.ReadHeaderTimeout > 0 {
				_ = c.SetReadDeadline(time.Now().Add(cfg.ReadHeaderTimeout))
			}

		case http.StateActive:
			_ = c.SetReadDeadline(time.Time{})

		case http.StateIdle:
			if cfg.IdleTimeout > 0 {
				_ = c.SetReadDeadline(time.Now().Add(cfg.IdleTimeout))
			}

		case http.StateHijacked, http.StateClosed:
			atomic.AddInt64(&activeConns, -1)
			atomic.StoreInt64(&s.stats.ActiveConnections, atomic.LoadInt64(&activeConns))
		}
	}
}

func (s *Server) Run() {
	s.GET("/healthz", func(w http.ResponseWriter, r *http.Request) {
		stats := s.GetStats()
		uptime := time.Since(stats.StartTime).Seconds()

		token := r.Header.Get("X-Metrics-Token")
		isAuthorized := token == s.Config.metricsToken || IsDev()

		response := map[string]any{
			"status":  "ok",
			"env":     AppEnv,
			"version": AppVersion,
		}

		response["uptime_seconds"] = uptime

		if isAuthorized {
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			response["active_connections"] = stats.ActiveConnections
			response["total_requests"] = stats.TotalRequests
			response["errors_4xx"] = stats.Errors4xx
			response["errors_5xx"] = stats.Errors5xx
			response["goroutines"] = runtime.NumGoroutine()
			response["memory_alloc_mb"] = float64(m.Alloc) / 1024 / 1024
			response["memory_sys_mb"] = float64(m.Sys) / 1024 / 1024
			response["num_gc"] = m.NumGC
			response["metrics_authorized"] = true
		} else {
			response["metrics_authorized"] = false
			response["message"] = "Use X-Metrics-Token header for detailed metrics"
		}

		s.RespondJSON(w, r, response)
	})

	s.GET("/metricz", func(w http.ResponseWriter, r *http.Request) {
		if s.Config.metricsEnabled {
			token := r.Header.Get("X-Metrics-Token")
			if token != s.Config.metricsToken && !IsDev() {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			stats := s.GetStats()
			uptime := time.Since(stats.StartTime).Seconds()

			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			metricsText := "# HELP goserver_active_connections Current active connections\n" +
				"# TYPE goserver_active_connections gauge\n" +
				"goserver_active_connections " + strconv.FormatInt(stats.ActiveConnections, 10) + "\n\n" +

				"# HELP goserver_total_requests Total requests served\n" +
				"# TYPE goserver_total_requests counter\n" +
				"goserver_total_requests " + strconv.FormatInt(stats.TotalRequests, 10) + "\n\n" +

				"# HELP goserver_uptime_seconds Server uptime in seconds\n" +
				"# TYPE goserver_uptime_seconds gauge\n" +
				"goserver_uptime_seconds " + strconv.FormatFloat(uptime, 'f', 2, 64) + "\n\n" +

				"# HELP goserver_errors_4xx Total 4xx errors\n" +
				"# TYPE goserver_errors_4xx counter\n" +
				"goserver_errors_4xx " + strconv.FormatInt(stats.Errors4xx, 10) + "\n\n" +

				"# HELP goserver_errors_5xx Total 5xx errors\n" +
				"# TYPE goserver_errors_5xx counter\n" +
				"goserver_errors_5xx " + strconv.FormatInt(stats.Errors5xx, 10) + "\n\n" +

				"# HELP goserver_memory_alloc_bytes Memory allocated in bytes\n" +
				"# TYPE goserver_memory_alloc_bytes gauge\n" +
				"goserver_memory_alloc_bytes " + strconv.FormatUint(m.Alloc, 10) + "\n\n" +

				"# HELP goserver_goroutines Current number of goroutines\n" +
				"# TYPE goserver_goroutines gauge\n" +
				"goserver_goroutines " + strconv.Itoa(runtime.NumGoroutine()) + "\n"

			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			s.RespondText(w, metricsText)
		} else {
			http.Error(w, "Metrics disabled", http.StatusNotFound)
		}
	})

	s.Use(s.ServerContextMiddleware)

	if s.Config.EnableGzip {
		s.Use(s.GzipMiddleware)
	}

	if s.Config.RateLimiteSize > 0 {
		s.Use(s.RateLimitMiddleware)
	}

	if err := s.Start(); err != nil {
		log.Fatalf("[goserver] server start failed: %v", err)
	}
}
