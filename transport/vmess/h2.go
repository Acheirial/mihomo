package vmess

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"sync"
	"time"

	N "github.com/metacubex/mihomo/common/net"

	"github.com/metacubex/http"
	"github.com/metacubex/randv2"
)

const h2IdleTimeout = 300 * time.Second

type h2Conn struct {
	pwriter *io.PipeWriter
	res     *http.Response
	cfg     *H2Config
	rt      http.RoundTripper
	once    sync.Once
	err     error
}

type h2Addr struct {
	network string
	addr    string
}

func (a h2Addr) Network() string { return a.network }
func (a h2Addr) String() string  { return a.addr }

type H2Config struct {
	Hosts []string
	Path  string
}

// NewH2Transport returns a reusable HTTP/2 Transport. DialTLSContext must
// already have completed the TLS handshake (h2c mode).
func NewH2Transport(dialTLS func(ctx context.Context) (net.Conn, error)) *http.Transport {
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialTLS(ctx)
		},
		Protocols:       protocols,
		IdleConnTimeout: h2IdleTimeout,
	}
}

func (hc *h2Conn) establishConn() error {
	preader, pwriter := io.Pipe()

	if len(hc.cfg.Hosts) == 0 {
		return errors.New("hosts is empty")
	}
	host := hc.cfg.Hosts[randv2.IntN(len(hc.cfg.Hosts))]
	path := hc.cfg.Path
	req := http.Request{
		Method: "PUT",
		Host:   host,
		URL: &url.URL{
			Scheme: "https",
			Host:   host,
			Path:   path,
		},
		Proto:      "HTTP/2",
		ProtoMajor: 2,
		ProtoMinor: 0,
		Body:       preader,
		Header: map[string][]string{
			"Accept-Encoding": {"identity"},
		},
	}

	res, err := hc.rt.RoundTrip(&req)
	if err != nil {
		_ = pwriter.Close()
		return err
	}

	hc.pwriter = pwriter
	hc.res = res
	return nil
}

func (hc *h2Conn) ensure() error {
	hc.once.Do(func() {
		hc.err = hc.establishConn()
	})
	return hc.err
}

func (hc *h2Conn) Read(b []byte) (int, error) {
	if err := hc.ensure(); err != nil {
		return 0, err
	}
	return hc.res.Body.Read(b)
}

func (hc *h2Conn) Write(b []byte) (int, error) {
	if err := hc.ensure(); err != nil {
		return 0, err
	}
	return hc.pwriter.Write(b)
}

func (hc *h2Conn) Close() error {
	var errs []error
	if hc.pwriter != nil {
		if err := hc.pwriter.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if hc.res != nil && hc.res.Body != nil {
		if err := hc.res.Body.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (hc *h2Conn) LocalAddr() net.Addr {
	return h2Addr{network: "tcp", addr: "h2:local"}
}

func (hc *h2Conn) RemoteAddr() net.Addr {
	return h2Addr{network: "tcp", addr: "h2:remote"}
}

func (hc *h2Conn) SetDeadline(t time.Time) error      { return nil }
func (hc *h2Conn) SetReadDeadline(t time.Time) error  { return nil }
func (hc *h2Conn) SetWriteDeadline(t time.Time) error { return nil }

func StreamH2Conn(ctx context.Context, rt http.RoundTripper, cfg *H2Config) (_ net.Conn, err error) {
	if rt == nil {
		return nil, errors.New("h2 transport is nil")
	}
	conn := &h2Conn{
		cfg: cfg,
		rt:  rt,
	}
	if ctx.Done() != nil {
		done := N.SetupContextForConn(ctx, conn)
		defer done(&err)
	}
	return conn, nil
}
