package mitm

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

// Dialer handles connections to target servers, optionally through an upstream proxy.
type Dialer struct {
	UpstreamProxy string
	Timeout       time.Duration
}

// NewDialer creates a new dialer.
func NewDialer(upstreamProxy string) *Dialer {
	return &Dialer{
		UpstreamProxy: upstreamProxy,
		Timeout:       10 * time.Second,
	}
}

// Dial connects to the target address, optionally through an upstream proxy.
func (d *Dialer) Dial(network, addr string) (net.Conn, error) {
	if d.UpstreamProxy == "" {
		return net.DialTimeout(network, addr, d.Timeout)
	}

	proxyURL, err := url.Parse(d.UpstreamProxy)
	if err != nil {
		return nil, fmt.Errorf("parse upstream proxy: %w", err)
	}

	switch proxyURL.Scheme {
	case "http", "https":
		return d.dialHTTPProxy(proxyURL, addr)
	case "socks5", "socks":
		return d.dialSOCKS5Proxy(proxyURL, addr)
	default:
		return nil, fmt.Errorf("unsupported upstream proxy scheme: %s", proxyURL.Scheme)
	}
}

// dialHTTPProxy connects through an HTTP CONNECT proxy.
func (d *Dialer) dialHTTPProxy(proxyURL *url.URL, targetAddr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "8080")
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, d.Timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to http proxy: %w", err)
	}

	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", targetAddr, targetAddr)

	if proxyURL.User != nil {
		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()
		auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
		connectReq += fmt.Sprintf("Proxy-Authorization: Basic %s\r\n", auth)
	}

	connectReq += "\r\n"

	if _, err := conn.Write([]byte(connectReq)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send CONNECT request: %w", err)
	}

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read proxy response: %w", err)
	}

	var httpVersion string
	var statusCode int
	var statusText string
	_, err = fmt.Sscanf(statusLine, "%s %d %s", &httpVersion, &statusCode, &statusText)
	if err != nil || statusCode != 200 {
		conn.Close()
		return nil, fmt.Errorf("proxy CONNECT failed: %s", statusLine)
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("read proxy headers: %w", err)
		}
		if line == "\r\n" || line == "\n" {
			break
		}
	}

	if reader.Buffered() > 0 {
		return &bufferedConn{Conn: conn, reader: reader}, nil
	}

	return conn, nil
}

// dialSOCKS5Proxy connects through a SOCKS5 proxy.
// DNS resolution is performed by the proxy server, not locally.
func (d *Dialer) dialSOCKS5Proxy(proxyURL *url.URL, targetAddr string) (net.Conn, error) {
	proxyAddr := proxyURL.Host
	if proxyURL.Port() == "" {
		proxyAddr = net.JoinHostPort(proxyURL.Hostname(), "1080")
	}

	conn, err := net.DialTimeout("tcp", proxyAddr, d.Timeout)
	if err != nil {
		return nil, fmt.Errorf("connect to socks5 proxy: %w", err)
	}

	var authMethod byte = 0x00
	if proxyURL.User != nil {
		authMethod = 0x02
	}

	greeting := []byte{0x05, 0x01, authMethod}
	if _, err := conn.Write(greeting); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 greeting: %w", err)
	}

	response := make([]byte, 2)
	if _, err := conn.Read(response); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 greeting response: %w", err)
	}

	if response[0] != 0x05 {
		conn.Close()
		return nil, errors.New("socks5: invalid version")
	}

	if response[1] == 0xFF {
		conn.Close()
		return nil, errors.New("socks5: no acceptable auth method")
	}

	if response[1] == 0x02 {
		if proxyURL.User == nil {
			conn.Close()
			return nil, errors.New("socks5: auth required but no credentials")
		}

		username := proxyURL.User.Username()
		password, _ := proxyURL.User.Password()

		authReq := []byte{0x01}
		authReq = append(authReq, byte(len(username)))
		authReq = append(authReq, []byte(username)...)
		authReq = append(authReq, byte(len(password)))
		authReq = append(authReq, []byte(password)...)

		if _, err := conn.Write(authReq); err != nil {
			conn.Close()
			return nil, fmt.Errorf("socks5 auth request: %w", err)
		}

		authResp := make([]byte, 2)
		if _, err := conn.Read(authResp); err != nil {
			conn.Close()
			return nil, fmt.Errorf("socks5 auth response: %w", err)
		}

		if authResp[1] != 0x00 {
			conn.Close()
			return nil, errors.New("socks5: authentication failed")
		}
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse target address: %w", err)
	}

	var port int
	fmt.Sscanf(portStr, "%d", &port)

	connectReq := []byte{0x05, 0x01, 0x00} // VER, CMD (CONNECT), RSV

	// Address type: check if IP or domain
	// net.ParseIP only parses, does NOT perform DNS lookup
	ip := net.ParseIP(host)
	if ip != nil {
		if ip4 := ip.To4(); ip4 != nil {
			connectReq = append(connectReq, 0x01) // IPv4
			connectReq = append(connectReq, ip4...)
		} else {
			connectReq = append(connectReq, 0x04) // IPv6
			connectReq = append(connectReq, ip...)
		}
	} else {
		// Domain name - DNS will be resolved by SOCKS5 proxy
		connectReq = append(connectReq, 0x03) // Domain
		connectReq = append(connectReq, byte(len(host)))
		connectReq = append(connectReq, []byte(host)...)
	}

	connectReq = append(connectReq, byte(port>>8), byte(port&0xFF))

	if _, err := conn.Write(connectReq); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect request: %w", err)
	}

	respHeader := make([]byte, 4)
	if _, err := conn.Read(respHeader); err != nil {
		conn.Close()
		return nil, fmt.Errorf("socks5 connect response: %w", err)
	}

	if respHeader[0] != 0x05 {
		conn.Close()
		return nil, errors.New("socks5: invalid response version")
	}

	if respHeader[1] != 0x00 {
		conn.Close()
		return nil, fmt.Errorf("socks5: connect failed with code %d", respHeader[1])
	}

	var addrLen int
	switch respHeader[3] {
	case 0x01:
		addrLen = 4
	case 0x03:
		lenByte := make([]byte, 1)
		if _, err := conn.Read(lenByte); err != nil {
			conn.Close()
			return nil, err
		}
		addrLen = int(lenByte[0])
	case 0x04:
		addrLen = 16
	}

	remaining := make([]byte, addrLen+2)
	if _, err := conn.Read(remaining); err != nil {
		conn.Close()
		return nil, err
	}

	return conn, nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}
