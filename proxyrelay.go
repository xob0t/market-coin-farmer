package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	xproxy "golang.org/x/net/proxy"
)

// proxyRelay is a local, auth-free HTTP proxy that forwards every connection
// through a single upstream proxy, injecting the upstream's credentials.
//
// Chromium cannot supply proxy username/password from the --proxy-server flag
// (it pops a native auth dialog instead), so we point the browser at this
// local relay and let the relay speak to the authenticated upstream. Supported
// upstream schemes: http, https, socks5, socks5h.
type proxyRelay struct {
	listener net.Listener
	upstream *url.URL
}

// startProxyRelay parses an upstream proxy URL (e.g.
// "http://user:pass@host:8080" or "socks5://user:pass@host:1080") and starts a
// local relay listening on a random loopback port.
func startProxyRelay(upstreamRaw string) (*proxyRelay, error) {
	upstreamRaw = strings.TrimSpace(upstreamRaw)
	u, err := url.Parse(upstreamRaw)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy URL: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("proxy URL is missing host")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("failed to open local proxy port: %w", err)
	}

	relay := &proxyRelay{listener: listener, upstream: u}
	go relay.serve()
	return relay, nil
}

// addr returns the loopback host:port the browser should be pointed at.
func (r *proxyRelay) addr() string {
	return r.listener.Addr().String()
}

func (r *proxyRelay) close() {
	_ = r.listener.Close()
}

func (r *proxyRelay) serve() {
	for {
		conn, err := r.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go r.handle(conn)
	}
}

func (r *proxyRelay) handle(client net.Conn) {
	defer client.Close()

	reader := bufio.NewReader(client)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	target := req.Host
	if req.Method != http.MethodConnect {
		target = hostPortFromURL(req.URL)
	}
	if target == "" {
		return
	}

	upstream, err := r.dialTarget(target)
	if err != nil {
		_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer upstream.Close()

	if req.Method == http.MethodConnect {
		// Tunnel established to the target; tell the browser and pipe raw bytes.
		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
	} else {
		// Plain HTTP: rewrite to origin-form and send it down the tunnel.
		req.URL.Scheme = ""
		req.URL.Host = ""
		req.RequestURI = ""
		if err := req.Write(upstream); err != nil {
			return
		}
	}

	pipe(client, upstream, reader)
}

// dialTarget returns a raw TCP tunnel to target ("host:port") established
// through the upstream proxy.
func (r *proxyRelay) dialTarget(target string) (net.Conn, error) {
	scheme := strings.ToLower(r.upstream.Scheme)
	if scheme == "socks5" || scheme == "socks5h" {
		var auth *xproxy.Auth
		if r.upstream.User != nil {
			password, _ := r.upstream.User.Password()
			auth = &xproxy.Auth{User: r.upstream.User.Username(), Password: password}
		}
		dialer, err := xproxy.SOCKS5("tcp", r.upstream.Host, auth, xproxy.Direct)
		if err != nil {
			return nil, err
		}
		return dialer.Dial("tcp", target)
	}
	return r.dialViaHTTPProxy(target)
}

func (r *proxyRelay) dialViaHTTPProxy(target string) (net.Conn, error) {
	host := r.upstream.Host
	if r.upstream.Port() == "" {
		if strings.EqualFold(r.upstream.Scheme, "https") {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}

	conn, err := net.DialTimeout("tcp", host, 30*time.Second)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(r.upstream.Scheme, "https") {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: r.upstream.Hostname()})
		if err := tlsConn.Handshake(); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tlsConn
	}

	var b strings.Builder
	fmt.Fprintf(&b, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if r.upstream.User != nil {
		password, _ := r.upstream.User.Password()
		credentials := base64.StdEncoding.EncodeToString([]byte(r.upstream.User.Username() + ":" + password))
		fmt.Fprintf(&b, "Proxy-Authorization: Basic %s\r\n", credentials)
	}
	b.WriteString("\r\n")
	if _, err := conn.Write([]byte(b.String())); err != nil {
		conn.Close()
		return nil, err
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		conn.Close()
		return nil, fmt.Errorf("upstream proxy CONNECT failed: %s", resp.Status)
	}
	return conn, nil
}

// pipe bridges the client and upstream connections. clientReader carries any
// bytes already buffered off the client socket during request parsing.
func pipe(client net.Conn, upstream net.Conn, clientReader *bufio.Reader) {
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(upstream, clientReader)
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, upstream)
		done <- struct{}{}
	}()
	<-done
}

func hostPortFromURL(u *url.URL) string {
	if u == nil || u.Host == "" {
		return ""
	}
	if u.Port() != "" {
		return u.Host
	}
	if strings.EqualFold(u.Scheme, "https") {
		return net.JoinHostPort(u.Hostname(), "443")
	}
	return net.JoinHostPort(u.Hostname(), "80")
}
