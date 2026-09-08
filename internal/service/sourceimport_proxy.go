package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/http/httpproxy"
)

// Only fixed proxies explicitly configured by this user are trusted endpoints.
// They may be local/private, but every tunneled destination must still be a
// validated public numeric IP. PAC/WPAD and proxy-side hostname DNS are unused.
type sourceNetworkError string

func (err sourceNetworkError) Error() string { return string(err) }

func sourceParseProxy(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 4096 || strings.ContainsAny(raw, "\r\n\x00") {
		return nil, sourceNetworkError("固定代理配置为空或无效")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	proxy, err := url.Parse(raw)
	if err != nil || proxy.Hostname() == "" || proxy.RawQuery != "" || proxy.Fragment != "" || proxy.Path != "" && proxy.Path != "/" {
		return nil, sourceNetworkError("固定代理地址无效；请检查 HTTP/HTTPS 代理配置")
	}
	proxy.Scheme = strings.ToLower(proxy.Scheme)
	if proxy.Scheme != "http" && proxy.Scheme != "https" {
		return nil, sourceNetworkError("来源导入仅支持固定 HTTP/HTTPS 代理；不支持 SOCKS、PAC 或自动代理脚本")
	}
	if port := proxy.Port(); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return nil, sourceNetworkError("固定代理端口无效")
		}
	}
	proxy.Path = ""
	return proxy, nil
}

func sourceProxyFromSettings(settings, scheme string) (*url.URL, error) {
	assignment, urlScheme := strings.Index(settings, "="), strings.Index(settings, "://")
	if assignment < 0 || urlScheme >= 0 && urlScheme < assignment {
		return sourceParseProxy(settings)
	}
	if len(settings) > 4096 {
		return nil, sourceNetworkError("固定代理配置过长")
	}
	selected, socks := "", false
	for _, item := range strings.Split(settings, ";") {
		if strings.TrimSpace(item) == "" {
			continue
		}
		protocol, address, ok := strings.Cut(item, "=")
		if !ok || strings.TrimSpace(address) == "" {
			return nil, sourceNetworkError("系统固定代理协议配置无效")
		}
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol == scheme {
			selected = strings.TrimSpace(address)
		}
		if protocol == "socks" || protocol == "socks5" {
			socks = true
		}
	}
	if selected != "" {
		return sourceParseProxy(selected)
	}
	if socks {
		return nil, sourceNetworkError("来源导入仅支持固定 HTTP/HTTPS 代理；不支持 SOCKS、PAC 或自动代理脚本")
	}
	return nil, nil
}

func sourceConfiguredProxy(destination *url.URL) (*url.URL, error) {
	configuration := httpproxy.FromEnvironment()
	if configuration.HTTPProxy != "" || configuration.HTTPSProxy != "" {
		proxy, err := configuration.ProxyFunc()(destination)
		if err != nil {
			return nil, sourceNetworkError("环境中的固定代理配置无效")
		}
		if proxy == nil {
			return nil, nil
		}
		return sourceParseProxy(proxy.String())
	}
	settings, enabled, err := sourceSystemProxySettings()
	if err != nil || !enabled {
		return nil, err
	}
	proxy, err := sourceProxyFromSettings(settings, destination.Scheme)
	if err != nil || proxy == nil {
		return proxy, err
	}
	// NO_PROXY remains effective even when the fixed proxy comes from Windows.
	selection := &httpproxy.Config{HTTPProxy: proxy.String(), HTTPSProxy: proxy.String(), NoProxy: configuration.NoProxy}
	selected, err := selection.ProxyFunc()(destination)
	if err != nil {
		return nil, sourceNetworkError("固定代理绕过规则无效")
	}
	return selected, nil
}

type sourceHTTPTransport struct {
	proxyForURL func(*url.URL) (*url.URL, error)
	lookupIP    func(context.Context, string, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
	rootCAs     *x509.CertPool // nil uses the OS trust store; fixtures supply a CA.
}

func (transport *sourceHTTPTransport) dial(ctx context.Context, address string) (net.Conn, error) {
	if transport.dialContext != nil {
		return transport.dialContext(ctx, "tcp", address)
	}
	return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
}

func (transport *sourceHTTPTransport) resolve(ctx context.Context, host string, proxy *url.URL) ([]netip.Addr, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if !sourcePublicIP(address) {
			return nil, sourceNetworkError("来源主机不是公共网络地址")
		}
		return []netip.Addr{address.Unmap()}, nil
	}
	lookup := transport.lookupIP
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNetIP
	}
	lookupContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	addresses, err := lookup(lookupContext, "ip", host)
	cancel()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || len(addresses) == 0 || len(addresses) > 32 {
		return nil, sourceNetworkError("无法取得有界的来源 DNS 结果")
	}
	allPublic, allFake := true, true
	fakeRange := netip.MustParsePrefix("198.18.0.0/15")
	for _, address := range addresses {
		allPublic = allPublic && sourcePublicIP(address)
		allFake = allFake && fakeRange.Contains(address.Unmap())
	}
	if allPublic {
		return addresses, nil
	}
	if allFake && proxy != nil {
		return transport.resolveDoH(ctx, host, proxy)
	}
	if allFake {
		return nil, sourceNetworkError("来源 DNS 返回代理 fake-IP，但当前请求没有可用的固定 HTTP/HTTPS 代理")
	}
	return nil, sourceNetworkError("来源主机解析到私有或保留网络地址")
}

type sourceProxyConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (connection *sourceProxyConnection) Read(buffer []byte) (int, error) {
	return connection.reader.Read(buffer)
}

func (transport *sourceHTTPTransport) connect(ctx context.Context, proxy *url.URL, destination string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(destination)
	address, addressErr := netip.ParseAddr(host)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || addressErr != nil || portErr != nil || portNumber < 1 || portNumber > 65535 || !sourcePublicIP(address) || proxy == nil {
		return nil, sourceNetworkError("代理隧道必须连接已验证的公共 IP 与有效端口")
	}
	proxy, err = sourceParseProxy(proxy.String())
	if err != nil {
		return nil, err
	}
	proxyPort := proxy.Port()
	if proxyPort == "" {
		proxyPort = "80"
		if proxy.Scheme == "https" {
			proxyPort = "443"
		}
	}
	raw, err := transport.dial(ctx, net.JoinHostPort(proxy.Hostname(), proxyPort))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, sourceNetworkError("无法连接已配置的固定代理")
	}
	succeeded := false
	defer func() {
		if !succeeded {
			raw.Close()
		}
	}()
	stopCancellation := context.AfterFunc(ctx, func() { raw.Close() })
	defer stopCancellation()
	deadline := time.Now().Add(10 * time.Second)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := raw.SetDeadline(deadline); err != nil {
		return nil, sourceNetworkError("无法设置代理连接期限")
	}
	connection := raw
	if proxy.Scheme == "https" {
		secured := tls.Client(raw, &tls.Config{ServerName: proxy.Hostname(), RootCAs: transport.rootCAs, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
		if err := secured.HandshakeContext(ctx); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, sourceNetworkError("HTTPS 代理的 TLS 证书验证或握手失败")
		}
		connection = secured
	}
	request := &http.Request{Method: http.MethodConnect, URL: &url.URL{Opaque: destination}, Host: destination, Header: make(http.Header)}
	if proxy.User != nil {
		password, _ := proxy.User.Password()
		request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(proxy.User.Username()+":"+password)))
	}
	if err := request.Write(connection); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, sourceNetworkError("无法发送固定代理隧道请求")
	}
	limited := &io.LimitedReader{R: connection, N: 64 << 10}
	reader := bufio.NewReader(limited)
	response, err := http.ReadResponse(reader, request)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, sourceNetworkError("固定代理隧道响应无效、超时或超过 64 KiB 上限")
	}
	if response.StatusCode != http.StatusOK {
		return nil, sourceNetworkError(fmt.Sprintf("固定代理拒绝隧道请求（HTTP %d）", response.StatusCode))
	}
	// Keep bytes buffered beyond CONNECT headers, without limiting subsequent
	// TLS/application reads. Never drain an untrusted CONNECT response body.
	limited.N = 1<<63 - 1
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return nil, sourceNetworkError("无法完成固定代理隧道")
	}
	succeeded = true
	return &sourceProxyConnection{Conn: connection, reader: reader}, nil
}

func (transport *sourceHTTPTransport) transportFor(destination *url.URL, proxy *url.URL, addresses []netip.Addr) *http.Transport {
	port := destination.Port()
	if port == "" {
		port = "80"
		if destination.Scheme == "https" {
			port = "443"
		}
	}
	return &http.Transport{
		Proxy: nil, DisableKeepAlives: true, ForceAttemptHTTP2: true, MaxConnsPerHost: 1,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 1 << 20,
		TLSClientConfig: &tls.Config{RootCAs: transport.rootCAs, ServerName: destination.Hostname(), MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, network, originalAddress string) (net.Conn, error) {
			for _, ip := range addresses {
				if !sourcePublicIP(ip) {
					return nil, sourceNetworkError("连接目的地址未通过公共 IP 校验")
				}
				address := net.JoinHostPort(ip.Unmap().String(), port)
				var connection net.Conn
				var err error
				if proxy == nil {
					connection, err = transport.dial(ctx, address)
				} else {
					connection, err = transport.connect(ctx, proxy, address)
				}
				if err == nil {
					return connection, nil
				}
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
			}
			return nil, sourceNetworkError("无法连接已验证的公共来源地址")
		},
	}
}

func (transport *sourceHTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	ctx := request.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	destination, err := sourceParseURL(request.URL.String())
	if err != nil {
		return nil, sourceNetworkError(err.Error())
	}
	selectProxy := transport.proxyForURL
	if selectProxy == nil {
		selectProxy = sourceConfiguredProxy
	}
	proxy, err := selectProxy(destination)
	if err != nil {
		var safe sourceNetworkError
		if errors.As(err, &safe) {
			return nil, safe
		}
		return nil, sourceNetworkError("无法读取固定代理配置")
	}
	addresses, err := transport.resolve(ctx, destination.Hostname(), proxy)
	if err != nil {
		return nil, err
	}
	clientTransport := transport.transportFor(destination, proxy, addresses)
	defer clientTransport.CloseIdleConnections()
	forwarded := request.Clone(ctx)
	forwarded.Host = destination.Host
	forwarded.Header.Del("Proxy-Authorization")
	response, err := clientTransport.RoundTrip(forwarded)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		var safe sourceNetworkError
		if errors.As(err, &safe) {
			return nil, safe
		}
		return nil, sourceNetworkError("公共来源连接或 TLS 证书验证失败")
	}
	return response, nil
}

func (transport *sourceHTTPTransport) resolveDoH(ctx context.Context, host string, proxy *url.URL) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// Cloudflare is the fixed DNS trust choice for fake-IP recovery only. The
	// bootstrap address is public and pinned; OS DNS and proxy hostname DNS
	// cannot redirect the resolver. TLS authenticates cloudflare-dns.com.
	resolver, _ := url.Parse("https://cloudflare-dns.com/dns-query")
	clientTransport := transport.transportFor(resolver, proxy, []netip.Addr{netip.MustParseAddr("1.1.1.1")})
	clientTransport.MaxResponseHeaderBytes = 64 << 10
	clientTransport.ResponseHeaderTimeout = 5 * time.Second
	defer clientTransport.CloseIdleConnections()
	client := &http.Client{Transport: clientTransport, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	addresses := []netip.Addr{}
	seen := map[netip.Addr]bool{}
	for _, question := range []struct {
		name string
		kind int
	}{{"A", 1}, {"AAAA", 28}} {
		query := *resolver
		query.RawQuery = url.Values{"name": []string{host}, "type": []string{question.name}}.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, query.String(), nil)
		if err != nil {
			return nil, sourceNetworkError("无法构造公共 DNS 查询")
		}
		request.Header.Set("Accept", "application/dns-json")
		response, err := client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, sourceNetworkError("无法通过固定代理验证公共 DNS；请检查代理连接")
		}
		if response.StatusCode != http.StatusOK || response.ContentLength > 64<<10 {
			response.Body.Close()
			return nil, sourceNetworkError("公共 DNS 响应失败或超过 64 KiB 上限")
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 64<<10|1))
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || len(body) > 64<<10 {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, sourceNetworkError("公共 DNS 响应读取失败或超过 64 KiB 上限")
		}
		var message struct {
			Status    *int `json:"Status"`
			Truncated bool `json:"TC"`
			Question  []struct {
				Name string `json:"name"`
				Type int    `json:"type"`
			} `json:"Question"`
			Answer []struct {
				Type int    `json:"type"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		if json.Unmarshal(body, &message) != nil || message.Status == nil || *message.Status != 0 || message.Truncated || len(message.Question) != 1 || message.Question[0].Type != question.kind || !strings.EqualFold(strings.TrimSuffix(message.Question[0].Name, "."), strings.TrimSuffix(host, ".")) || len(message.Answer) > 32 {
			return nil, sourceNetworkError("公共 DNS 返回无效或不匹配的有界结果")
		}
		for _, answer := range message.Answer {
			if answer.Type == 5 { // The authenticated resolver already follows CNAME.
				continue
			}
			ip, err := netip.ParseAddr(answer.Data)
			if err != nil || !sourcePublicIP(ip) || answer.Type != question.kind || answer.Type == 1 && !ip.Is4() || answer.Type == 28 && (!ip.Is6() || ip.Is4In6()) {
				return nil, sourceNetworkError("公共 DNS 返回私有、保留或无效的目的地址")
			}
			if !seen[ip] {
				addresses = append(addresses, ip)
				seen[ip] = true
			}
			if len(addresses) > 32 {
				return nil, sourceNetworkError("公共 DNS 地址数量超过 32 条上限")
			}
		}
	}
	if len(addresses) == 0 {
		return nil, sourceNetworkError("公共 DNS 未返回可连接的公共地址")
	}
	return addresses, nil
}
