package goserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func browserTestClient(t *testing.T, roots *x509.CertPool, opts ...ClientOption) *HttpClient {
	t.Helper()
	base := []ClientOption{WithBrowserTLS(true), WithAllowInsecureSSRF(true),
		WithRandomDelayMin(0), WithRandomDelayMax(0), WithMaxRetries(0)}
	c := NewHttpClient(append(base, opts...)...)
	c.client.Transport.(*sessionTransport).baseTransport.(*browserTransport).rootCAs = roots
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestBrowserTLSOptIn(t *testing.T) {
	for _, opts := range [][]ClientOption{nil, {WithBrowserTLS(false)}, {WithBrowserTLS(true), WithBrowserTLS(false)}} {
		c := NewHttpClient(opts...)
		if c.client.Transport.(*sessionTransport).baseTransport != sharedTransport {
			t.Fatal("disabled option changed the default transport")
		}
	}
	c := browserTestClient(t, nil)
	if c.client.Transport.(*sessionTransport).baseTransport.(*browserTransport).clients != nil {
		t.Fatal("browser client initialized before first request")
	}
	for i := 0; i < 32; i++ {
		c := browserTestClient(t, nil)
		session := c.client.Transport.(*sessionTransport)
		profile, ok := browserProfileForUserAgent(session.sessionHeaders["User-Agent"])
		if !ok || profile.GetClientHelloStr() != session.baseTransport.(*browserTransport).profile.GetClientHelloStr() {
			t.Fatal("random session UA does not match the selected TLS profile")
		}
	}
}

func TestBrowserTLSProtocolsAndSession(t *testing.T) {
	for _, ua := range userAgents {
		profile, supported := browserProfileForUserAgent(ua)
		if !supported {
			continue
		}
		for _, h2 := range []bool{false, true} {
			name := "HTTP1"
			if h2 {
				name = "HTTP2"
			}
			t.Run(profile.GetClientHelloStr()+"/"+name, func(t *testing.T) {
				var sawGREASE atomic.Bool
				server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirect" {
						http.SetCookie(w, &http.Cookie{Name: "session", Value: "yes", Path: "/"})
						http.Redirect(w, r, "/final", http.StatusFound)
						return
					}
					if h2 && r.ProtoMajor != 2 {
						t.Error("did not negotiate HTTP/2")
					}
					if !h2 && r.ProtoMajor != 1 {
						t.Error("did not fall back to HTTP/1.1")
					}
					if r.UserAgent() != ua {
						t.Errorf("unexpected UA: %s", r.UserAgent())
					}
					if strings.Contains(ua, "Chrome/") {
						expected, _, _ := generateSecChHeaders(ua)
						if r.Header.Get("Sec-Ch-Ua") != expected {
							t.Error("client hints/profile mismatch")
						}
					} else if r.Header.Get("Sec-Ch-Ua") != "" {
						t.Error("Firefox sent Chromium client hints")
					}
					if r.URL.Path == "/final" {
						if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "yes" {
							t.Error("redirect lost cookies")
						}
					}
					w.Header().Set("Trailer", "X-Checksum")
					_, _ = io.Copy(w, r.Body)
					w.Header().Set("X-Checksum", "done")
				}))
				server.EnableHTTP2 = h2
				server.TLS = &tls.Config{GetConfigForClient: func(info *tls.ClientHelloInfo) (*tls.Config, error) {
					for _, suite := range info.CipherSuites {
						if suite&0x0f0f == 0x0a0a && byte(suite>>8) == byte(suite) {
							sawGREASE.Store(true)
						}
					}
					return nil, nil
				}}
				server.StartTLS()
				defer server.Close()
				roots := x509.NewCertPool()
				roots.AddCert(server.Certificate())
				c := browserTestClient(t, roots)
				session := c.client.Transport.(*sessionTransport)
				session.baseTransport.(*browserTransport).profile = profile
				session.sessionHeaders["User-Agent"] = ua
				if strings.Contains(ua, "Chrome/") {
					ch, mobile, platform := generateSecChHeaders(ua)
					session.sessionHeaders["Sec-Ch-Ua"] = ch
					session.sessionHeaders["Sec-Ch-Ua-Mobile"] = mobile
					session.sessionHeaders["Sec-Ch-Ua-Platform"] = platform
				} else {
					delete(session.sessionHeaders, "Sec-Ch-Ua")
					delete(session.sessionHeaders, "Sec-Ch-Ua-Mobile")
					delete(session.sessionHeaders, "Sec-Ch-Ua-Platform")
				}
				resp, err := c.Get(context.Background(), server.URL+"/redirect")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.Request.URL.Path != "/final" {
					t.Fatal("redirect not followed by outer client")
				}
				if h2 && (resp.TLS == nil || len(resp.TLS.VerifiedChains) == 0) {
					t.Fatal("verified TLS metadata missing")
				}
				if strings.Contains(ua, "Chrome/") && !sawGREASE.Load() {
					t.Fatal("ClientHello did not use browser GREASE cipher suite")
				}
				var wg sync.WaitGroup
				for i := 0; i < 8; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						resp, err := c.Post(context.Background(), server.URL+"/echo", []byte("payload"), "text/plain")
						if err != nil {
							t.Error(err)
							return
						}
						defer resp.Body.Close()
						body, err := io.ReadAll(resp.Body)
						if err != nil || string(body) != "payload" {
							t.Errorf("body: %q, error: %v", body, err)
						}
						if resp.Trailer.Get("X-Checksum") != "done" {
							t.Error("lost response trailer")
						}
					}()
				}
				wg.Wait()
			})
		}
	}
}

func TestBrowserTLSVerificationAndSSRF(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	c := browserTestClient(t, nil)
	if resp, err := c.Get(context.Background(), server.URL); err == nil {
		resp.Body.Close()
		t.Fatal("accepted untrusted certificate")
	}
	c = browserTestClient(t, nil, WithAllowInsecureSSRF(false))
	for _, target := range []string{server.URL, strings.Replace(server.URL, "https:", "http:", 1)} {
		if _, err := c.Get(context.Background(), target); !errors.Is(err, ErrSSRFViolation) {
			t.Fatalf("SSRF not blocked: %v", err)
		}
	}
}

func TestBrowserTLSConnectProxy(t *testing.T) {
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Error("proxy lost browser HTTP/2 transport")
		}
		if r.Header.Get("Proxy-Authorization") != "" {
			t.Error("proxy credentials leaked to origin")
		}
		_, _ = io.WriteString(w, "proxied")
	}))
	origin.EnableHTTP2 = true
	origin.StartTLS()
	defer origin.Close()
	for _, secure := range []bool{false, true} {
		var connects atomic.Int32
		proxyServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect || r.Header.Get("Proxy-Authorization") != "Basic dXNlcjpwYXNz" {
				t.Error("missing authenticated CONNECT")
				http.Error(w, "bad CONNECT", 400)
				return
			}
			upstream, err := net.Dial("tcp", origin.Listener.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			downstream, buffered, err := w.(http.Hijacker).Hijack()
			if err != nil {
				upstream.Close()
				t.Error(err)
				return
			}
			connects.Add(1)
			_, _ = io.WriteString(downstream, "HTTP/1.1 200 Connection Established\r\n\r\n")
			go func() { defer upstream.Close(); _, _ = io.Copy(upstream, buffered) }()
			defer downstream.Close()
			_, _ = io.Copy(downstream, upstream)
		}))
		if secure {
			proxyServer.StartTLS()
		} else {
			proxyServer.Start()
		}
		p, _ := url.Parse(proxyServer.URL)
		p.User = url.UserPassword("user", "pass")
		roots := x509.NewCertPool()
		roots.AddCert(origin.Certificate())
		if secure {
			roots.AddCert(proxyServer.Certificate())
		}
		c := browserTestClient(t, roots, WithProxyURL(p))
		resp, err := c.Get(context.Background(), "https://example.com/proxy-test")
		if err != nil {
			proxyServer.Close()
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(body) != "proxied" || connects.Load() != 1 {
			t.Fatalf("proxy result: %q, %v", body, err)
		}
		c.CloseIdleConnections()
		blocked := browserTestClient(t, roots, WithProxyURL(p), WithAllowInsecureSSRF(false))
		if _, err := blocked.Get(context.Background(), "https://example.com/proxy-test"); !errors.Is(err, ErrSSRFViolation) {
			t.Errorf("private proxy bypassed SSRF policy: %v", err)
		}
		proxyServer.Close()
	}
}

func TestBrowserTLSCancellation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	c := browserTestClient(t, roots, WithTimeout(100*time.Millisecond))
	start := time.Now()
	_, err := c.Get(context.Background(), server.URL)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > time.Second {
		t.Fatalf("timeout not honored: %v", err)
	}
}
