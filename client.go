package goserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	sharedTransport *http.Transport
	transportOnce   sync.Once
	cbMap           sync.Map
	dnsCache        sync.Map
	dnsCacheTTL     = 5 * time.Minute
	bufferPool      = sync.Pool{
		New: func() interface{} {
			return bytes.NewBuffer(make([]byte, 0, 4096))
		},
	}
)

var ErrSSRFViolation = errors.New("security violation: SSRF blocked (attempt to access internal network)")

var defaultBrowserHeaders = map[string]string{
	"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"Connection":                "keep-alive",
	"Cache-Control":             "no-cache",
	"Pragma":                    "no-cache",
	"DNT":                       "1",
	"Upgrade-Insecure-Requests": "1",
	"Sec-Fetch-Dest":            "document",
	"Sec-Fetch-Mode":            "navigate",
	"Sec-Fetch-Site":            "none",
	"Sec-Fetch-User":            "?1",
}

type proxyCtxKey struct{}

func isSafeIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}

	if ip.IsUnspecified() {
		return false
	}

	if ip.IsMulticast() {
		return false
	}
	return true
}

type dnsCacheEntry struct {
	timestamp time.Time
	ips       []net.IPAddr
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}

	var ips []net.IPAddr
	now := time.Now()

	if entry, ok := dnsCache.Load(host); ok {
		cacheEntry := entry.(*dnsCacheEntry)
		if now.Sub(cacheEntry.timestamp) < dnsCacheTTL {
			ips = cacheEntry.ips
		}
	}

	if ips == nil {
		resolver := &net.Resolver{
			PreferGo: true,
		}

		ips, err = resolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("dns resolve failed: %w", err)
		}

		dnsCache.Store(host, &dnsCacheEntry{
			ips:       ips,
			timestamp: now,
		})
	}

	for _, ip := range ips {
		if !isSafeIP(ip.IP) {
			return nil, ErrSSRFViolation
		}
	}

	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 90 * time.Second,
	}

	ipIndex := atomic.AddUint64(&roundRobinCounter, 1) % uint64(len(ips))
	safeAddr := net.JoinHostPort(ips[ipIndex].IP.String(), port)

	return dialer.DialContext(ctx, network, safeAddr)
}

var roundRobinCounter uint64

func initSharedTransport() {
	transportOnce.Do(func() {
		sharedTransport = &http.Transport{
			Proxy: func(req *http.Request) (*url.URL, error) {
				if shouldBypassProxy(req.URL.Host) {
					return nil, nil
				}
				if p, ok := req.Context().Value(proxyCtxKey{}).(*url.URL); ok && p != nil {
					return p, nil
				}
				return nil, nil
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          2000,
			MaxIdleConnsPerHost:   500,
			MaxConnsPerHost:       100,
			IdleConnTimeout:       120 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 500 * time.Millisecond,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				allowInsecure, _ := ctx.Value(ssrfCtxKey{}).(bool)

				if allowInsecure {
					dialer := &net.Dialer{
						Timeout:   5 * time.Second,
						KeepAlive: 90 * time.Second,
					}
					return dialer.DialContext(ctx, network, addr)
				}

				return safeDialContext(ctx, network, addr)
			},
			DisableCompression: false,
			WriteBufferSize:    32 * 1024,
			ReadBufferSize:     32 * 1024,
		}
	})
}

type CircuitState string
type ssrfCtxKey struct{}

const (
	Closed   CircuitState = "closed"
	Open     CircuitState = "open"
	HalfOpen CircuitState = "halfopen"
)

var ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

type CircuitBreakerOpenError struct {
	Host  string
	Cause error
}

func (e *CircuitBreakerOpenError) Error() string {
	if e.Cause == nil {
		return fmt.Sprintf("%s for %s", ErrCircuitBreakerOpen, e.Host)
	}
	return fmt.Sprintf("%s for %s; last failure: %v", ErrCircuitBreakerOpen, e.Host, e.Cause)
}

func (e *CircuitBreakerOpenError) Unwrap() []error {
	if e.Cause == nil {
		return []error{ErrCircuitBreakerOpen}
	}
	return []error{ErrCircuitBreakerOpen, e.Cause}
}

type ClientConfig struct {
	ProxyURL              *url.URL
	ProxyFunc             func() *url.URL
	OnProxyError          func(*url.URL)
	MetricsCallback       func(Metrics)
	Timeout               time.Duration
	MaxRetries            int
	BaseBackoffDelay      time.Duration
	MaxBackoffDelay       time.Duration
	RandomDelayMin        time.Duration
	RandomDelayMax        time.Duration
	CBMaxFailures         int
	CBResetTimeout        time.Duration
	DNSCacheTTL           time.Duration
	AllowInsecureSSRF     bool
	DisableDNSCache       bool
	EnableMetrics         bool
	BrowserTLS            bool
	DisableCircuitBreaker bool
}

type Metrics struct {
	Host            string
	CircuitState    string
	RequestDuration time.Duration
	StatusCode      int
	RetryCount      int
	DNSCacheHit     bool
}

func DefaultClientConfig() ClientConfig {
	return ClientConfig{
		Timeout:          15 * time.Second,
		MaxRetries:       2,
		BaseBackoffDelay: 200 * time.Millisecond,
		MaxBackoffDelay:  2 * time.Second,
		RandomDelayMin:   200 * time.Millisecond,
		RandomDelayMax:   1000 * time.Millisecond,
		CBMaxFailures:    5,
		CBResetTimeout:   5 * time.Second,
		DNSCacheTTL:      5 * time.Minute,
	}
}

type ClientOption func(*ClientConfig)

func WithBrowserTLS(enable bool) ClientOption {
	return func(c *ClientConfig) { c.BrowserTLS = enable }
}

func WithMaxRetries(r int) ClientOption { return func(c *ClientConfig) { c.MaxRetries = r } }
func WithProxyFunc(fn func() *url.URL) ClientOption {
	return func(c *ClientConfig) {

		if fn == nil {
			c.ProxyFunc = nil
			return
		}

		c.ProxyFunc = func() *url.URL {
			p := fn()

			if p == nil || p.String() == "" {
				return nil
			}

			return p
		}
	}
}
func WithProxyURL(u *url.URL) ClientOption     { return func(c *ClientConfig) { c.ProxyURL = u } }
func WithTimeout(t time.Duration) ClientOption { return func(c *ClientConfig) { c.Timeout = t } }
func WithCBMaxFailures(f int) ClientOption     { return func(c *ClientConfig) { c.CBMaxFailures = f } }
func WithCBResetTimeout(t time.Duration) ClientOption {
	return func(c *ClientConfig) { c.CBResetTimeout = t }
}

func WithDisableCircuitBreaker(disable bool) ClientOption {
	return func(c *ClientConfig) { c.DisableCircuitBreaker = disable }
}
func WithRandomDelayMin(min time.Duration) ClientOption {
	return func(c *ClientConfig) { c.RandomDelayMin = min }
}
func WithRandomDelayMax(max time.Duration) ClientOption {
	return func(c *ClientConfig) { c.RandomDelayMax = max }
}
func WithOnProxyError(fn func(*url.URL)) ClientOption {
	return func(c *ClientConfig) { c.OnProxyError = fn }
}

func WithAllowInsecureSSRF(allow bool) ClientOption {
	return func(c *ClientConfig) { c.AllowInsecureSSRF = allow }
}

func WithDNSCacheTTL(ttl time.Duration) ClientOption {
	return func(c *ClientConfig) { c.DNSCacheTTL = ttl }
}

func WithDisableDNSCache(disable bool) ClientOption {
	return func(c *ClientConfig) { c.DisableDNSCache = disable }
}

func WithEnableMetrics(enable bool) ClientOption {
	return func(c *ClientConfig) { c.EnableMetrics = enable }
}

func WithMetricsCallback(callback func(Metrics)) ClientOption {
	return func(c *ClientConfig) { c.MetricsCallback = callback }
}

func WithMaxConcurrentRequests(max int) ClientOption {
	return func(c *ClientConfig) {

	}
}

type CircuitBreaker struct {
	lastFailureMu sync.RWMutex
	lastFailure   error
	lastAttempt   int64
	state         int32
	failures      int32
	MaxFailures   int
	ResetTimeout  time.Duration
}

func getCircuitBreaker(host string, cfg ClientConfig) *CircuitBreaker {
	if val, ok := cbMap.Load(host); ok {
		return val.(*CircuitBreaker)
	}
	cb := &CircuitBreaker{
		MaxFailures:  cfg.CBMaxFailures,
		ResetTimeout: cfg.CBResetTimeout,
		state:        0,
	}
	val, _ := cbMap.LoadOrStore(host, cb)
	return val.(*CircuitBreaker)
}

func (cb *CircuitBreaker) allowRequest() bool {
	state := atomic.LoadInt32(&cb.state)

	if state == 0 {
		return true
	}

	if state == 1 {
		lastAttempt := atomic.LoadInt64(&cb.lastAttempt)
		if time.Since(time.Unix(0, lastAttempt)) > cb.ResetTimeout {
			if atomic.CompareAndSwapInt32(&cb.state, 1, 2) {
				atomic.StoreInt32(&cb.failures, 0)
				return true
			}
		}
		return false
	}

	return true
}

func (cb *CircuitBreaker) success() {
	cb.lastFailureMu.Lock()
	defer cb.lastFailureMu.Unlock()
	cb.lastFailure = nil
	atomic.StoreInt32(&cb.failures, 0)
	atomic.StoreInt32(&cb.state, 0)
}

func (cb *CircuitBreaker) failure(cause error) {
	cb.lastFailureMu.Lock()
	defer cb.lastFailureMu.Unlock()
	cb.lastFailure = cause
	atomic.StoreInt64(&cb.lastAttempt, time.Now().UnixNano())
	failures := atomic.AddInt32(&cb.failures, 1)

	state := atomic.LoadInt32(&cb.state)
	if state == 2 || failures >= int32(cb.MaxFailures) {
		atomic.StoreInt32(&cb.state, 1)
	}
}

func (cb *CircuitBreaker) openError(host string) error {
	cb.lastFailureMu.RLock()
	defer cb.lastFailureMu.RUnlock()
	return &CircuitBreakerOpenError{Host: host, Cause: cb.lastFailure}
}

type sessionTransport struct {
	baseTransport  http.RoundTripper
	sessionHeaders map[string]string
}

func (st *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	reqCopy := req.Clone(req.Context())
	if reqCopy.Header == nil {
		reqCopy.Header = make(http.Header)
	}

	for k, v := range st.sessionHeaders {
		if _, exists := reqCopy.Header[http.CanonicalHeaderKey(k)]; !exists {
			if v == "" {
				reqCopy.Header.Del(k)
			} else {
				reqCopy.Header.Set(k, v)
			}
		}
	}

	return st.baseTransport.RoundTrip(reqCopy)
}

func (st *sessionTransport) CloseIdleConnections() {
	if t, ok := st.baseTransport.(interface{ CloseIdleConnections() }); ok {
		t.CloseIdleConnections()
	}
}

type HttpClient struct {
	client       *http.Client
	currentProxy *url.URL
	config       ClientConfig
	proxyMu      sync.Mutex
}

func GetBrowserSimpleHeaders() map[string]string {
	ua := randomUserAgent()
	lang := randomAcceptLanguage()

	return map[string]string{
		"User-Agent":      ua,
		"Accept-Language": lang,
	}
}

func NewHttpClient(opts ...ClientOption) *HttpClient {
	initSharedTransport()

	cfg := DefaultClientConfig()
	for _, opt := range opts {
		opt(&cfg)
	}

	var sessionUA string
	var browser *browserTransport
	if cfg.BrowserTLS {
		sessionUA, browser = newBrowserTransport(cfg.AllowInsecureSSRF)
	} else {
		sessionUA = randomUserAgent()
	}
	sessionLang := randomAcceptLanguage()
	secChUa, secChMobile, secChPlatform := generateSecChHeaders(sessionUA)

	sessionHeaders := make(map[string]string)
	for k, v := range defaultBrowserHeaders {
		sessionHeaders[k] = v
	}
	sessionHeaders["User-Agent"] = sessionUA
	sessionHeaders["Accept-Language"] = sessionLang
	sessionHeaders["Sec-Ch-Ua"] = secChUa
	sessionHeaders["Sec-Ch-Ua-Mobile"] = secChMobile
	sessionHeaders["Sec-Ch-Ua-Platform"] = secChPlatform

	var baseTransport http.RoundTripper = sharedTransport
	if cfg.BrowserTLS {
		baseTransport = browser
		if !strings.Contains(sessionUA, "Chrome/") {
			delete(sessionHeaders, "Sec-Ch-Ua")
			delete(sessionHeaders, "Sec-Ch-Ua-Mobile")
			delete(sessionHeaders, "Sec-Ch-Ua-Platform")
		}
		delete(sessionHeaders, "Connection")
	}
	transportWithHeaders := &sessionTransport{
		baseTransport:  baseTransport,
		sessionHeaders: sessionHeaders,
	}

	jar, _ := cookiejar.New(nil)

	return &HttpClient{
		client: &http.Client{
			Jar:       jar,
			Timeout:   cfg.Timeout,
			Transport: transportWithHeaders,
		},
		config: cfg,
	}
}

func (c *HttpClient) CloseIdleConnections() { c.client.CloseIdleConnections() }

func (c *HttpClient) Do(req *http.Request) (*http.Response, error) {
	startTime := time.Now()
	var metrics Metrics
	metrics.Host = req.URL.Host

	if c.config.RandomDelayMax > c.config.RandomDelayMin {
		delay := RandomDelay(c.config.RandomDelayMin, c.config.RandomDelayMax)
		select {
		case <-time.After(delay):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}

	var cb *CircuitBreaker
	if !c.config.DisableCircuitBreaker {
		cb = getCircuitBreaker(req.URL.Host, c.config)
	}
	retriesLeft := c.config.MaxRetries
	attempt := 0
	canReplay := req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
	var lastResponse *http.Response
	var lastError error
	defer func() {
		if lastResponse != nil && lastResponse.Body != nil {
			_ = lastResponse.Body.Close()
		}
	}()

	for {
		if err := req.Context().Err(); err != nil {
			return nil, err
		}
		if cb != nil && !cb.allowRequest() {
			if lastResponse != nil {
				resp := lastResponse
				lastResponse = nil
				if c.config.EnableMetrics && c.config.MetricsCallback != nil {
					c.config.MetricsCallback(metrics)
				}
				return resp, nil
			}
			if lastError != nil {
				if c.config.EnableMetrics && c.config.MetricsCallback != nil {
					c.config.MetricsCallback(metrics)
				}
				return nil, lastError
			}
			return nil, cb.openError(req.URL.Host)
		}
		if lastResponse != nil {
			if lastResponse.Body != nil {
				_ = lastResponse.Body.Close()
			}
			lastResponse = nil
		}

		pURL := c.getProxy()
		ctx := req.Context()

		if pURL != nil {
			ctx = context.WithValue(ctx, proxyCtxKey{}, pURL)
		}
		ctx = context.WithValue(ctx, ssrfCtxKey{}, c.config.AllowInsecureSSRF)

		reqToExecute := req.WithContext(ctx)
		if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
			body, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("recreate request body for retry: %w", err)
			}
			reqToExecute.Body = body
		}

		resp, err := c.client.Do(reqToExecute)
		metrics.RequestDuration = time.Since(startTime)
		metrics.RetryCount = c.config.MaxRetries - retriesLeft

		if err != nil {
			if req.Context().Err() != nil {
				return nil, req.Context().Err()
			}
			lastError = err
			metrics.StatusCode = 0
			if cb != nil {
				cb.failure(err)
			}
			if pURL != nil && c.config.OnProxyError != nil {
				c.config.OnProxyError(pURL)
			}
			c.rotateProxy()

			if retriesLeft <= 0 || !canReplay || (cb != nil && atomic.LoadInt32(&cb.state) == 1) {
				if c.config.EnableMetrics && c.config.MetricsCallback != nil {
					metrics.StatusCode = 0
					c.config.MetricsCallback(metrics)
				}
				return nil, err
			}
			retriesLeft--

			backoffDuration := exponentialBackoff(attempt, c.config.BaseBackoffDelay, c.config.MaxBackoffDelay)
			attempt++

			select {
			case <-time.After(backoffDuration):
				continue
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		statusCode := resp.StatusCode
		metrics.StatusCode = statusCode
		is429 := statusCode == http.StatusTooManyRequests
		isAuthErr := statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden

		if statusCode < 500 && !is429 && (!isAuthErr || c.config.ProxyFunc == nil) {
			if cb != nil {
				cb.success()
			}
			if c.config.EnableMetrics && c.config.MetricsCallback != nil {
				c.config.MetricsCallback(metrics)
			}
			return resp, nil
		}

		if cb != nil {
			cb.failure(fmt.Errorf("HTTP %d %s", statusCode, http.StatusText(statusCode)))
		}
		if pURL != nil && c.config.OnProxyError != nil {
			c.config.OnProxyError(pURL)
		}
		c.rotateProxy()

		if retriesLeft <= 0 || !canReplay || (cb != nil && atomic.LoadInt32(&cb.state) == 1) {
			if c.config.EnableMetrics && c.config.MetricsCallback != nil {
				c.config.MetricsCallback(metrics)
			}
			return resp, nil
		}
		lastResponse = resp
		lastError = nil
		retriesLeft--

		backoffDuration := exponentialBackoff(attempt, c.config.BaseBackoffDelay, c.config.MaxBackoffDelay)
		attempt++

		select {
		case <-time.After(backoffDuration):
			continue
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
}

func (c *HttpClient) Cookies(u *url.URL) []*http.Cookie {
	if c.client.Jar != nil {
		return c.client.Jar.Cookies(u)
	}
	return nil
}

func (c *HttpClient) ResetCookies() error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}
	c.client.Jar = jar
	return nil
}

func ClearDNSCache() {
	dnsCache.Range(func(key, value interface{}) bool {
		dnsCache.Delete(key)
		return true
	})
}

func ClearCircuitBreakers() {
	cbMap.Range(func(key, value interface{}) bool {
		cbMap.Delete(key)
		return true
	})
}

func GetCircuitBreakerStats() map[string]interface{} {
	stats := make(map[string]interface{})
	cbMap.Range(func(key, value interface{}) bool {
		cb := value.(*CircuitBreaker)
		stats[key.(string)] = map[string]interface{}{
			"state":       atomic.LoadInt32(&cb.state),
			"failures":    atomic.LoadInt32(&cb.failures),
			"lastAttempt": time.Unix(0, atomic.LoadInt64(&cb.lastAttempt)),
		}
		return true
	})
	return stats
}

func RandomDelay(min, max time.Duration) time.Duration {
	if max <= min {
		return min
	}
	return min + time.Duration(rand.Int63n(int64(max-min)))
}

func exponentialBackoff(attempt int, baseDelay, maxDelay time.Duration) time.Duration {
	if attempt > 62 {
		attempt = 62
	}
	backoff := baseDelay * time.Duration(1<<attempt)
	if backoff > maxDelay || backoff < 0 {
		backoff = maxDelay
	}
	jitterMultiplier := 0.75 + (rand.Float64() * 0.5)
	return time.Duration(float64(backoff) * jitterMultiplier)
}

func shouldBypassProxy(host string) bool {
	host = strings.ToLower(strings.Split(host, ":")[0])
	if host == "localhost" || host == "docker.internal" || strings.HasSuffix(host, ".local") {
		return true
	}
	ip := net.ParseIP(host)
	if ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		return true
	}
	return false
}

func (hc *HttpClient) Get(ctx context.Context, url string, headers ...map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	applyHeaders(req, headers)
	return hc.Do(req)
}

func (hc *HttpClient) Post(ctx context.Context, url string, body []byte, contentType string, headers ...map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", contentType)
	applyHeaders(req, headers)

	return hc.Do(req)
}

func (hc *HttpClient) PostJSON(ctx context.Context, url string, data any, headers ...map[string]string) (*http.Response, error) {
	buf := bufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufferPool.Put(buf)

	if err := json.NewEncoder(buf).Encode(data); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	applyHeaders(req, headers)

	return hc.Do(req)
}

func applyHeaders(req *http.Request, headerMaps []map[string]string) {
	for _, hMap := range headerMaps {
		for k, v := range hMap {
			req.Header.Set(k, v)
		}
	}
}

func (c *HttpClient) getProxy() *url.URL {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()

	if c.config.ProxyURL != nil {
		return c.config.ProxyURL
	}
	if c.config.ProxyFunc != nil {
		if c.currentProxy == nil {
			c.currentProxy = c.config.ProxyFunc()
		}
		return c.currentProxy
	}
	return nil
}

func (c *HttpClient) rotateProxy() {
	c.proxyMu.Lock()
	defer c.proxyMu.Unlock()

	if c.config.ProxyFunc != nil {
		c.currentProxy = c.config.ProxyFunc()
	}
}
