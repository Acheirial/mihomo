package tunnel

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/component/mitm/rewrite"
	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/log"
	"github.com/metacubex/mihomo/tunnel/statistic"

	"golang.org/x/net/http/httpguts"
)

// mitmProbeTimeout bounds the wait for the first plaintext bytes after a
// successful MITM handshake, mirroring the TCP sniffer's read-ahead budget.
const mitmProbeTimeout = time.Second

const mitmBadGateway = "Bad Gateway\n"

// handleMITMRewrite serves an already-decrypted plaintext connection through
// the rewrite rules engine. It returns true when it has taken over the
// connection: the caller must stop processing it (the connection is closed
// here or handed back only after every request has been answered). It returns
// false when the traffic is not HTTP/1.x; nothing was consumed and the normal
// pipeline continues with the buffered bytes intact.
//
// The connection is served with keep-alive: each request either short-circuits
// through HandleRequest (response written from the buffered response writer)
// or is forwarded upstream with an http.Transport whose DialContext routes
// through the regular rule engine (resolveMetadata + proxy.DialContext).
// Responses are re-framed with an exact Content-Length, so both paths can
// continue the loop safely.
func handleMITMRewrite(conn *N.BufferedConn, metadata *C.Metadata) bool {
	// Probe for an HTTP/1.x request line without consuming it. On decline the
	// bytes stay buffered for the normal pipeline (handshake proxying, sniffer).
	_ = conn.SetReadDeadline(time.Now().Add(mitmProbeTimeout))
	probe, err := conn.Peek(7)
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || !isHTTPTraffic(probe) {
		return false
	}

	rewrites := mitmDispatcher.Rewrites()

	// Per-connection transport: idle upstream connections are released as soon
	// as the client goes away, so tracked connections never outlive the session.
	transport := &http.Transport{
		DialContext:     mitmDialContext(metadata),
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	defer transport.CloseIdleConnections()

	// Defensive: nil rules (unconfigured dispatcher) must degrade to pure forwarding.
	if rewrites == nil {
		rewrites = &rewrite.Rules{}
	}

	for {
		req, err := http.ReadRequest(conn.Reader())
		if err != nil {
			// EOF, reset or non-HTTP garbage: the decrypted stream has been
			// consumed, never pass it through raw.
			log.Debugln("[MITM] read request from %s: %v", metadata.SourceDetail(), err)
			return true
		}

		rw := &bufferedResponseWriter{header: make(http.Header)}
		if rewrites.HandleRequest(rw, req) {
			// Short-circuited: the rule engine produced a complete buffered response.
			if !writeMITMBufferedResponse(conn, rw, req.Close) {
				return true
			}
			if req.Close {
				return true
			}
			continue
		}

		// Not hit or rewritten in place: forward the (possibly rewritten) request.
		req.RequestURI = "" // client requests must not carry the origin-form URI
		if req.URL.Host == "" {
			req.URL.Host = req.Host
		}
		if req.URL.Host == "" {
			req.URL.Host = metadata.Host
		}
		if req.URL.Scheme == "" {
			req.URL.Scheme = "http"
		}

		resp, err := transport.RoundTrip(req)
		if err != nil {
			log.Debugln("[MITM] forward %s --> %s: %v", metadata.SourceDetail(), req.URL.Host, err)
			writeMITMBadGateway(conn)
			return true
		}

		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if err := rewrites.HandleResponse(resp); err != nil {
			log.Debugln("[MITM] rewrite response: %v", err)
		}
		// The engine may have consumed and replaced the body; read back whatever it left.
		finalBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if !writeMITMResponse(conn, resp.StatusCode, resp.Status, resp.Header, finalBody, resp.Close || req.Close) {
			return true
		}
		if resp.Close || req.Close {
			return true
		}
	}
}

// mitmDialContext returns a DialContext that routes upstream dials through the
// regular rule engine: resolveMetadata matches rules (resolving DNS lazily) and
// the matched proxy performs the dial, so rewrites respect the user's policy.
func mitmDialContext(metadata *C.Metadata) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil, err
		}

		md := &C.Metadata{
			NetWork: C.TCP,
			// INNER marks tunnel-internal traffic: process lookup is skipped and
			// handleTCPConn's rewrite takeover explicitly declines INNER, so a
			// forwarded flow can never recurse back into MITM handling.
			Type:    C.INNER,
			SrcIP:   metadata.SrcIP,
			SrcPort: metadata.SrcPort,
			DstPort: uint16(port),
		}
		if ip, err := netip.ParseAddr(host); err != nil {
			md.Host = host
		} else {
			md.DstIP = ip.Unmap()
		}

		proxy, rule, err := resolveMetadata(md)
		if err != nil {
			return nil, err
		}
		conn, err := proxy.DialContext(ctx, md)
		if err != nil {
			logMetadataErr(md, rule, proxy, err)
			return nil, err
		}
		tracker := statistic.NewTCPTracker(conn, statistic.DefaultManager, md, rule, 0, 0, true)
		logMetadata(md, rule, tracker)
		return tracker, nil
	}
}

func writeMITMBufferedResponse(conn net.Conn, rw *bufferedResponseWriter, clientClose bool) bool {
	code := rw.code
	if code == 0 {
		code = http.StatusOK
	}
	header := rw.header
	// The whole body is buffered, so answer with exact framing to keep the
	// connection reusable regardless of what the handler declared.
	header.Del("Transfer-Encoding")
	if header.Get("Content-Length") == "" {
		header.Set("Content-Length", strconv.Itoa(rw.body.Len()))
	}
	if clientClose || header.Get("Connection") == "close" {
		header.Set("Connection", "close")
	}
	return writeMITMResponse(conn, code, "", header, rw.body.Bytes(), false)
}

func writeMITMResponse(conn net.Conn, code int, status string, header http.Header, body []byte, upstreamClose bool) bool {
	if status == "" {
		status = http.StatusText(code)
	}
	if upstreamClose {
		header.Set("Connection", "close")
	}
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\n", code, status)
	_ = header.Write(conn)
	_, _ = io.WriteString(conn, "\r\n")
	if len(body) > 0 {
		if _, err := conn.Write(body); err != nil {
			return false
		}
	}
	return true
}

func writeMITMBadGateway(conn net.Conn) {
	fmt.Fprintf(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(mitmBadGateway), mitmBadGateway)
}

// bufferedResponseWriter collects a short-circuited rewrite response so it can
// be framed and written back in one shot, keeping the keep-alive loop intact.
type bufferedResponseWriter struct {
	code   int
	header http.Header
	body   bytes.Buffer
}

func (w *bufferedResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(code int) {
	if w.code == 0 {
		w.code = code
	}
}

func (w *bufferedResponseWriter) Write(p []byte) (int, error) {
	if w.code == 0 {
		w.code = http.StatusOK
	}
	return w.body.Write(p)
}

// isHTTPTraffic reports whether the peeked bytes look like the start of an
// HTTP/1.x request line.
func isHTTPTraffic(buf []byte) bool {
	method, _, _ := strings.Cut(string(buf), " ")
	return validMethod(method)
}

func validMethod(method string) bool {
	return len(method) > 0 && strings.IndexFunc(method, func(r rune) bool {
		return !httpguts.IsTokenRune(r)
	}) == -1
}
