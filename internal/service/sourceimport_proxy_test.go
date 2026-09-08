package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sourceProxyTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"cloudflare-dns.com", "article.example", "proxy.example"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

func sourceProxyTestTLSServer(t *testing.T, certificate tls.Certificate, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func sourceProxyTestTunnel(t *testing.T, certificate tls.Certificate, secure bool, routes map[string]string, observe func(*http.Request)) *url.URL {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy received %s rather than a pinned-IP tunnel", r.Method)
			http.Error(w, "CONNECT required", http.StatusBadRequest)
			return
		}
		if observe != nil {
			observe(r)
		}
		destination, ok := routes[r.Host]
		if !ok {
			t.Errorf("unvetted tunnel authority: %q", r.Host)
			http.Error(w, "unknown fixture route", http.StatusBadGateway)
			return
		}
		upstream, err := net.Dial("tcp", destination)
		if err != nil {
			t.Error(err)
			return
		}
		client, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			t.Error(err)
			return
		}
		t.Cleanup(func() { client.Close(); upstream.Close() })
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = buffer.Flush()
		go func() { _, _ = io.Copy(upstream, buffer); upstream.Close(); client.Close() }()
		go func() { _, _ = io.Copy(client, upstream); client.Close(); upstream.Close() }()
	}))
	if secure {
		server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
		server.StartTLS()
	} else {
		server.Start()
	}
	t.Cleanup(server.Close)
	proxy, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return proxy
}

func sourceProxyTestDNSReply(r *http.Request, answers any) map[string]any {
	typeOfQuestion := 1
	if r.URL.Query().Get("type") == "AAAA" {
		typeOfQuestion = 28
	}
	return map[string]any{
		"Status": 0, "TC": false,
		"Question": []any{map[string]any{"name": r.URL.Query().Get("name") + ".", "type": typeOfQuestion}},
		"Answer":   answers,
	}
}

func TestSourceImportProxyFakeDNSUsesPublicTunnelAndOriginalTLSHost(t *testing.T) {
	for _, secureProxy := range []bool{false, true} {
		t.Run(fmt.Sprint(secureProxy), func(t *testing.T) {
			certificate, roots := sourceProxyTestCertificate(t)
			var resolverRequests, articleRequests atomic.Int32
			dns := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) {
				resolverRequests.Add(1)
				if r.Host != "cloudflare-dns.com" || r.TLS.ServerName != "cloudflare-dns.com" || r.Header.Get("Proxy-Authorization") != "" {
					t.Errorf("resolver Host/TLS/credential boundary: host=%s, sni=%s", r.Host, r.TLS.ServerName)
				}
				answers := []any{}
				if r.URL.Query().Get("type") == "A" {
					answers = append(answers, map[string]any{"name": "article.example.", "type": 1, "data": "93.184.216.34"})
				}
				w.Header().Set("Content-Type", "application/dns-json")
				_ = json.NewEncoder(w).Encode(sourceProxyTestDNSReply(r, answers))
			})
			article := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) {
				articleRequests.Add(1)
				if r.Host != "article.example" || r.TLS.ServerName != "article.example" || r.Header.Get("Proxy-Authorization") != "" {
					t.Errorf("article Host/TLS/credential boundary: host=%s, sni=%s", r.Host, r.TLS.ServerName)
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, "<main>Public article through the configured proxy.</main>")
			})
			var mu sync.Mutex
			authorities := []string{}
			proxy := sourceProxyTestTunnel(t, certificate, secureProxy, map[string]string{"1.1.1.1:443": dns.Listener.Addr().String(), "93.184.216.34:443": article.Listener.Addr().String()}, func(r *http.Request) {
				mu.Lock()
				authorities = append(authorities, r.Host)
				mu.Unlock()
				wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("proxy-user:proxy-secret"))
				if r.Header.Get("Proxy-Authorization") != wantAuth {
					t.Error("configured credentials were not isolated to CONNECT")
				}
				if secureProxy && r.TLS.ServerName != "proxy.example" {
					t.Errorf("HTTPS proxy used destination SNI: %q", r.TLS.ServerName)
				}
			})
			proxyAddress := proxy.Host
			if secureProxy {
				proxy.Host = net.JoinHostPort("proxy.example", proxy.Port())
			}
			proxy.User = url.UserPassword("proxy-user", "proxy-secret")
			transport := &sourceHTTPTransport{
				rootCAs:     roots,
				proxyForURL: func(*url.URL) (*url.URL, error) { return proxy, nil },
				lookupIP: func(ctx context.Context, network, host string) ([]netip.Addr, error) {
					if host != "article.example" {
						t.Errorf("resolver bootstrap unexpectedly used OS DNS for %s", host)
					}
					return []netip.Addr{netip.MustParseAddr("198.18.0.119")}, nil
				},
				dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					if address != proxy.Host {
						t.Errorf("socket dial escaped the explicitly configured proxy: %s", address)
					}
					return (&net.Dialer{}).DialContext(ctx, network, proxyAddress)
				},
			}
			svc := NewImportServiceWithTasks(NewTaskService(nil))
			svc.sourceImports.httpClient = &http.Client{Transport: transport}
			workspace := t.TempDir()
			result := runSourceImport(t, svc, workspace, SourceImportRequest{SourceType: "url", Source: "https://article.example/docs", Kind: "topic", Name: "Proxied"})
			if result.Imported != 1 || resolverRequests.Load() != 2 || articleRequests.Load() != 1 {
				t.Fatalf("bounded proxy import: result=%+v, DNS=%d, article=%d", result, resolverRequests.Load(), articleRequests.Load())
			}
			mu.Lock()
			if strings.Join(authorities, ",") != "1.1.1.1:443,1.1.1.1:443,93.184.216.34:443" {
				t.Errorf("CONNECT must receive only the previously vetted numeric IPs: %v", authorities)
			}
			mu.Unlock()
			for _, name := range append(result.Files, "Resources/Proxied/sources.md") {
				content := sourceRead(t, workspace, name)
				if strings.Contains(content, "proxy-secret") || strings.Contains(content, "proxy-user") {
					t.Fatal("proxy credentials reached imported artifacts")
				}
			}
		})
	}
}

func TestSourceImportProxyDoesNotTreatPrivateDNSAsFakeDNS(t *testing.T) {
	for _, tc := range []struct {
		name      string
		addresses []string
		proxy     bool
	}{
		{"fake DNS without proxy", []string{"198.18.0.119"}, false},
		{"private with proxy", []string{"10.0.0.1"}, true},
		{"loopback with proxy", []string{"127.0.0.1"}, true},
		{"metadata with proxy", []string{"169.254.169.254"}, true},
		{"private IPv6 with proxy", []string{"fd00::1"}, true},
		{"mixed fake and public", []string{"198.18.0.119", "93.184.216.34"}, true},
		{"mixed fake and private", []string{"198.18.0.119", "10.0.0.1"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var proxy *url.URL
			if tc.proxy {
				proxy, _ = url.Parse("http://proxy-user:proxy-secret@127.0.0.1:1")
			}
			transport := &sourceHTTPTransport{
				lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
					addresses := []netip.Addr{}
					for _, address := range tc.addresses {
						addresses = append(addresses, netip.MustParseAddr(address))
					}
					return addresses, nil
				},
				dialContext: func(context.Context, string, string) (net.Conn, error) {
					t.Error("rejected DNS answers must not cause a socket connection")
					return nil, errors.New("proxy-secret")
				},
			}
			if _, err := transport.resolve(context.Background(), "article.example", proxy); err == nil || strings.Contains(err.Error(), "proxy-secret") {
				t.Fatalf("private/fake DNS was allowed or leaked credentials: %v", err)
			}
		})
	}
}

func TestSourceImportProxyRejectsMalformedOrPrivateDoHAnswers(t *testing.T) {
	for _, testCase := range []string{"malformed JSON", "private IP", "fake IP", "wrong family", "wrong question", "missing status", "too many answers", "oversized body"} {
		t.Run(testCase, func(t *testing.T) {
			certificate, roots := sourceProxyTestCertificate(t)
			dns := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/dns-json")
				if testCase == "malformed JSON" {
					_, _ = io.WriteString(w, "{broken")
					return
				}
				if testCase == "oversized body" {
					_, _ = io.WriteString(w, strings.Repeat(" ", 64<<10|1))
					return
				}
				ip := "93.184.216.34"
				switch testCase {
				case "private IP":
					ip = "127.0.0.1"
				case "fake IP":
					ip = "198.18.0.119"
				case "wrong family":
					ip = "2606:4700:4700::1111"
				}
				answers := []any{map[string]any{"name": "article.example.", "type": 1, "data": ip}}
				if testCase == "too many answers" {
					for len(answers) <= 32 {
						answers = append(answers, answers[0])
					}
				}
				response := sourceProxyTestDNSReply(r, answers)
				if testCase == "wrong question" {
					response["Question"] = []any{map[string]any{"name": "other.example.", "type": 1}}
				}
				if testCase == "missing status" {
					delete(response, "Status")
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			proxy := sourceProxyTestTunnel(t, certificate, false, map[string]string{"1.1.1.1:443": dns.Listener.Addr().String()}, nil)
			transport := &sourceHTTPTransport{rootCAs: roots, lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
				return []netip.Addr{netip.MustParseAddr("198.18.0.119")}, nil
			}}
			if _, err := transport.resolve(context.Background(), "article.example", proxy); err == nil {
				t.Fatal("invalid public DNS response was trusted")
			}
		})
	}
}

func TestSourceImportProxyConnectCancellationAndCredentialRedaction(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	}))
	defer proxyServer.Close()
	defer close(release)
	proxy, _ := url.Parse(proxyServer.URL)
	proxy.User = url.UserPassword("proxy-user", "proxy-secret")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		connection, err := (&sourceHTTPTransport{}).connect(ctx, proxy, "93.184.216.34:443")
		if connection != nil {
			connection.Close()
		}
		finished <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not receive CONNECT")
	}
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "proxy-secret") {
			t.Fatalf("CONNECT cancellation lost its context or leaked credentials: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CONNECT ignored cancellation while waiting for proxy headers")
	}
	transport := &sourceHTTPTransport{dialContext: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("proxy-secret") }}
	if _, err := transport.connect(context.Background(), proxy, "93.184.216.34:443"); err == nil || strings.Contains(err.Error(), "proxy-secret") {
		t.Fatalf("dial error leaked proxy credentials: %v", err)
	}
	if _, err := transport.connect(context.Background(), proxy, "127.0.0.1:443"); err == nil {
		t.Fatal("CONNECT accepted a private destination")
	}
}

func TestSourceImportProxyConfigurationHonorsExplicitTypesAndEnvironment(t *testing.T) {
	credentialProxy, credentialErr := sourceProxyFromSettings("http://proxy-user:proxy-secret=@proxy.example:8111", "https")
	if credentialErr != nil || credentialProxy == nil {
		t.Fatal("a credential '=' must not be interpreted as a system protocol assignment")
	}
	for _, tc := range []struct {
		settings, scheme, want string
	}{
		{"127.0.0.1:7890", "https", "http://127.0.0.1:7890"},
		{"http=127.0.0.1:7890;https=127.0.0.1:7891", "https", "http://127.0.0.1:7891"},
		{"http=https://proxy.example:9443;https=https://proxy.example:9443", "http", "https://proxy.example:9443"},
	} {
		proxy, err := sourceProxyFromSettings(tc.settings, tc.scheme)
		if err != nil || proxy == nil || proxy.String() != tc.want {
			t.Errorf("fixed proxy setting %q: got %v, %v", tc.settings, proxy, err)
		}
	}
	for _, settings := range []string{"socks=127.0.0.1:7890", "socks5://proxy-user:proxy-secret@127.0.0.1:7890", "http://proxy-user:proxy-secret@invalid:port", "http://proxy-user:proxy-secret@proxy.example/path"} {
		if _, err := sourceProxyFromSettings(settings, "https"); err == nil || strings.Contains(err.Error(), "proxy-secret") {
			t.Errorf("unsupported/malformed proxy did not fail safely: %v", err)
		}
	}
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:8111")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8222")
	t.Setenv("NO_PROXY", "bypass.example")
	t.Setenv("REQUEST_METHOD", "")
	for _, tc := range []struct{ raw, want string }{{"http://article.example/page", "http://127.0.0.1:8111"}, {"https://article.example/page", "http://127.0.0.1:8222"}, {"https://bypass.example/page", ""}} {
		u, _ := url.Parse(tc.raw)
		proxy, err := sourceConfiguredProxy(u)
		got := ""
		if proxy != nil {
			got = proxy.String()
		}
		if err != nil || got != tc.want {
			t.Errorf("proxy environment selection: got %q, %v; want %q", got, err, tc.want)
		}
	}
}

func TestSourceImportProxyRejectsMalformedEnvironmentInsteadOfGoingDirect(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://proxy-user:proxy-secret@invalid:port")
	t.Setenv("HTTPS_PROXY", "")
	t.Setenv("NO_PROXY", "")
	t.Setenv("REQUEST_METHOD", "")
	destination, _ := url.Parse("http://article.example/page")
	if _, err := sourceConfiguredProxy(destination); err == nil || strings.Contains(err.Error(), "proxy-secret") {
		t.Fatalf("malformed configured proxy must fail clearly without falling back to direct access: %v", err)
	}
}

func TestSourceImportProxyRevalidatesDNSOnRedirect(t *testing.T) {
	certificate, roots := sourceProxyTestCertificate(t)
	article := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://private.example/metadata")
		w.WriteHeader(http.StatusFound)
	})
	var connections atomic.Int32
	proxy := sourceProxyTestTunnel(t, certificate, false, map[string]string{"93.184.216.34:443": article.Listener.Addr().String()}, func(*http.Request) { connections.Add(1) })
	transport := &sourceHTTPTransport{
		rootCAs: roots, proxyForURL: func(*url.URL) (*url.URL, error) { return proxy, nil },
		lookupIP: func(ctx context.Context, network, host string) ([]netip.Addr, error) {
			ip := "93.184.216.34"
			if host == "private.example" {
				ip = "169.254.169.254"
			}
			return []netip.Addr{netip.MustParseAddr(ip)}, nil
		},
	}
	svc := NewImportService()
	svc.sourceImports.httpClient = &http.Client{Transport: transport}
	if _, err := svc.PreviewSourceImport(t.TempDir(), SourceImportRequest{SourceType: "url", Source: "https://article.example/page", Kind: "topic", Name: "Redirect"}); err == nil || connections.Load() != 1 {
		t.Fatalf("private redirected DNS must be rejected before another CONNECT: connections=%d, err=%v", connections.Load(), err)
	}
}

func TestSourceImportProxyRequiresTrustedCertificateForOriginalHostname(t *testing.T) {
	for _, testCase := range []string{"untrusted CA", "wrong hostname"} {
		t.Run(testCase, func(t *testing.T) {
			certificate, roots := sourceProxyTestCertificate(t)
			var requests atomic.Int32
			article := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) { requests.Add(1) })
			proxy := sourceProxyTestTunnel(t, certificate, false, map[string]string{"93.184.216.34:443": article.Listener.Addr().String()}, nil)
			raw := "https://article.example/page"
			if testCase == "untrusted CA" {
				roots = nil
			} else {
				raw = "https://wrong.example/page"
			}
			transport := &sourceHTTPTransport{
				rootCAs: roots, proxyForURL: func(*url.URL) (*url.URL, error) { return proxy, nil },
				lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
				},
			}
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			response, err := client.Get(raw)
			if response != nil {
				response.Body.Close()
			}
			if err == nil || requests.Load() != 0 {
				t.Fatalf("HTTPS destination certificate was not verified for its original name: %v", err)
			}
		})
	}
}

func TestSourceImportProxyDirectConnectionPinsTheFirstPublicDNSResult(t *testing.T) {
	certificate, roots := sourceProxyTestCertificate(t)
	article := sourceProxyTestTLSServer(t, certificate, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "article.example" || r.TLS.ServerName != "article.example" {
			t.Error("direct IP pinning changed HTTP Host or TLS SNI")
		}
		_, _ = io.WriteString(w, "direct public content")
	})
	var lookups atomic.Int32
	transport := &sourceHTTPTransport{
		rootCAs: roots, proxyForURL: func(*url.URL) (*url.URL, error) { return nil, nil },
		lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
			if lookups.Add(1) > 1 {
				return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
		dialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "93.184.216.34:443" {
				t.Errorf("direct dial did another unvetted hostname lookup: %s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, article.Listener.Addr().String())
		},
	}
	response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get("https://article.example/page")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "direct public content" || lookups.Load() != 1 {
		t.Fatalf("direct transport should use exactly the validated DNS result: body=%q, lookups=%d, err=%v", body, lookups.Load(), err)
	}
}

func TestSourceImportProxyHTTPStillUsesPinnedConnectAndOriginalHost(t *testing.T) {
	article := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "article.example" || r.Header.Get("Proxy-Authorization") != "" {
			t.Error("HTTP source lost Host or received proxy credentials")
		}
		_, _ = io.WriteString(w, "public HTTP content")
	}))
	defer article.Close()
	proxy := sourceProxyTestTunnel(t, tls.Certificate{}, false, map[string]string{"93.184.216.34:80": article.Listener.Addr().String()}, nil)
	transport := &sourceHTTPTransport{
		proxyForURL: func(*url.URL) (*url.URL, error) { return proxy, nil },
		lookupIP: func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		},
	}
	response, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get("http://article.example/page")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "public HTTP content" {
		t.Fatalf("HTTP tunnel result: %q, %v", body, err)
	}
}

func TestSourceImportProxyBoundsConnectResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\nX-Large: " + strings.Repeat("x", 65<<10) + "\r\n\r\n")
		_ = buffer.Flush()
	}))
	defer server.Close()
	proxy, _ := url.Parse(server.URL)
	connection, err := (&sourceHTTPTransport{}).connect(context.Background(), proxy, "93.184.216.34:443")
	if connection != nil {
		connection.Close()
	}
	if err == nil {
		t.Fatal("CONNECT accepted unbounded proxy response headers")
	}
}
