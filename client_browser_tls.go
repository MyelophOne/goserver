package goserver

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/net/proxy"
)

func browserProfileForUserAgent(ua string) (profiles.ClientProfile, bool) {
	if strings.Contains(ua, "Edg/") || strings.Contains(ua, "CriOS/") {
		return profiles.ClientProfile{}, false
	}
	for _, browser := range []string{"Chrome", "Firefox"} {
		_, tail, found := strings.Cut(ua, browser+"/")
		if !found {
			continue
		}
		version, _, _ := strings.Cut(tail, ".")
		key := strings.ToLower(browser) + "_" + version
		profile, ok := profiles.MappedTLSClients[key]
		if !ok {
			profile, ok = profiles.MappedTLSClients[key+"_PSK"]
		}
		return profile, ok
	}
	return profiles.ClientProfile{}, false
}

func newBrowserTransport(allowInsecureSSRF bool) (string, *browserTransport) {
	r := rngPool.Get().(*rand.Rand)
	defer rngPool.Put(r)
	t := &browserTransport{allowInsecureSSRF: allowInsecureSSRF}
	var selected string
	count := 0
	for _, ua := range userAgents {
		if profile, ok := browserProfileForUserAgent(ua); ok {
			count++
			if r.Intn(count) == 0 {
				selected, t.profile = ua, profile
			}
		}
	}
	if count == 0 {
		t.initErr = fmt.Errorf("browser TLS: no supported browser/version in userAgents")
	}
	return selected, t
}

type browserTransport struct {
	mu                sync.Mutex
	clients           map[string]tlsclient.HttpClient
	allowInsecureSSRF bool
	rootCAs           *x509.CertPool
	profile           profiles.ClientProfile
	initErr           error
}

func (t *browserTransport) clientFor(p *url.URL) (tlsclient.HttpClient, error) {
	if t.initErr != nil {
		return nil, t.initErr
	}
	key := ""
	if p != nil {
		key = p.String()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if c := t.clients[key]; c != nil {
		return c, nil
	}
	idleTimeout := 120 * time.Second
	options := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(t.profile),
		tlsclient.WithDisableHttp3(),
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithTimeoutMilliseconds(0),
		tlsclient.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
			ctx = context.WithValue(ctx, ssrfCtxKey{}, t.allowInsecureSSRF)
			return dialBrowserProxy(ctx, network, addr, p, t.rootCAs)
		}),
		tlsclient.WithTransportOptions(&tlsclient.TransportOptions{
			RootCAs: t.rootCAs, IdleConnTimeout: &idleTimeout,
			MaxIdleConns: 2000, MaxIdleConnsPerHost: 500, MaxConnsPerHost: 100,
			WriteBufferSize: 32 * 1024, ReadBufferSize: 32 * 1024,
		}),
	}
	if strings.HasPrefix(t.profile.GetClientHelloStr(), "Chrome-") {
		options = append(options, tlsclient.WithRandomTLSExtensionOrder())
	}
	c, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		return nil, err
	}
	if t.clients == nil {
		t.clients = make(map[string]tlsclient.HttpClient)
	}
	if len(t.clients) >= 8 {
		for k, old := range t.clients {
			old.CloseIdleConnections()
			delete(t.clients, k)
			break
		}
	}
	t.clients[key] = c
	return c, nil
}

func (t *browserTransport) CloseIdleConnections() {
	sharedTransport.CloseIdleConnections()
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, c := range t.clients {
		c.CloseIdleConnections()
	}
}

func (t *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return sharedTransport.RoundTrip(req)
	}
	p, err := sharedTransport.Proxy(req)
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	c, err := t.clientFor(p)
	if err != nil {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, err
	}
	r := (&fhttp.Request{
		Method: req.Method, URL: req.URL, Host: req.Host,
		Header: fhttp.Header(req.Header.Clone()), Body: req.Body, GetBody: req.GetBody,
		ContentLength: req.ContentLength, TransferEncoding: req.TransferEncoding,
		Close: req.Close, Trailer: fhttp.Header(req.Trailer),
	}).WithContext(req.Context())
	if req.Body == http.NoBody {
		r.Body = fhttp.NoBody
	}
	if strings.Contains(t.profile.GetClientHelloStr(), "Chrome") {
		r.Header[fhttp.HeaderOrderKey] = []string{
			"host", "connection", "cache-control", "sec-ch-ua", "sec-ch-ua-mobile",
			"sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "accept",
			"sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest",
			"accept-encoding", "accept-language", "cookie",
		}
	} else {
		r.Header[fhttp.HeaderOrderKey] = []string{
			"host", "user-agent", "accept", "accept-language", "accept-encoding",
			"connection", "upgrade-insecure-requests", "sec-fetch-dest",
			"sec-fetch-mode", "sec-fetch-site", "sec-fetch-user", "cookie",
		}
	}
	resp, err := c.Do(r)
	if err != nil {
		return nil, err
	}
	out := &http.Response{
		Status: resp.Status, StatusCode: resp.StatusCode, Proto: resp.Proto,
		ProtoMajor: resp.ProtoMajor, ProtoMinor: resp.ProtoMinor,
		Header: http.Header(resp.Header), Body: resp.Body, ContentLength: resp.ContentLength,
		TransferEncoding: resp.TransferEncoding, Close: resp.Close,
		Uncompressed: resp.Uncompressed, Trailer: http.Header(resp.Trailer), Request: req,
	}
	if s := resp.TLS; s != nil {
		out.TLS = &tls.ConnectionState{
			Version: s.Version, HandshakeComplete: s.HandshakeComplete,
			DidResume: s.DidResume, CipherSuite: s.CipherSuite,
			NegotiatedProtocol: s.NegotiatedProtocol, ServerName: s.ServerName,
			PeerCertificates: s.PeerCertificates, VerifiedChains: s.VerifiedChains,
			SignedCertificateTimestamps: s.SignedCertificateTimestamps, OCSPResponse: s.OCSPResponse,
		}
	}
	return out, nil
}

type browserForwardDialer struct{ ctx context.Context }

func (d browserForwardDialer) Dial(network, addr string) (net.Conn, error) {
	return d.DialContext(d.ctx, network, addr)
}
func (d browserForwardDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return sharedTransport.DialContext(ctx, network, addr)
}

func dialBrowserProxy(ctx context.Context, network, addr string, p *url.URL, roots *x509.CertPool) (net.Conn, error) {
	if p == nil {
		return sharedTransport.DialContext(ctx, network, addr)
	}
	if p.Scheme == "socks5" || p.Scheme == "socks5h" {
		copyURL := *p
		copyURL.Scheme = "socks5"
		if copyURL.Port() == "" {
			copyURL.Host = net.JoinHostPort(copyURL.Hostname(), "1080")
		}
		d, err := proxy.FromURL(&copyURL, browserForwardDialer{ctx})
		if err != nil {
			return nil, err
		}
		return d.(proxy.ContextDialer).DialContext(ctx, network, addr)
	}
	if p.Scheme != "http" && p.Scheme != "https" {
		return nil, fmt.Errorf("browser TLS: unsupported proxy scheme %q", p.Scheme)
	}
	port := p.Port()
	if port == "" {
		port = "80"
		if p.Scheme == "https" {
			port = "443"
		}
	}
	conn, err := sharedTransport.DialContext(ctx, network, net.JoinHostPort(p.Hostname(), port))
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = conn.Close()
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	rawConn := conn
	cancelDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() { _ = rawConn.Close(); close(cancelDone) })
	defer func() {
		if !stop() {
			<-cancelDone
		}
	}()
	if p.Scheme == "https" {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: p.Hostname(), RootCAs: roots, NextProtos: []string{"http/1.1"}})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return nil, err
		}
		conn = tlsConn
	}
	connect := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: addr}, Host: addr, Header: make(http.Header)}
	if p.User != nil {
		password, _ := p.User.Password()
		connect.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(p.User.Username()+":"+password)))
	}
	if err := connect.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, connect)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser TLS: proxy CONNECT returned %s", resp.Status)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	return &browserTunnelConn{Conn: conn, reader: reader}, nil
}

type browserTunnelConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *browserTunnelConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
