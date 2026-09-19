package trojan

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/metacubex/mihomo/transport/socks5"
)

func TestClientConnCloseWithoutWriteSendsNoHeader(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	got := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 256)
		n, err := server.Read(buf)
		if err != nil {
			got <- nil
			return
		}
		got <- buf[:n]
	}()

	addr := socks5.ParseAddr("example.com:443")
	if addr == nil {
		t.Fatal("parse socks addr")
	}
	conn := NewClientConn(client, Key("password"), CommandTCP, addr)
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case data := <-got:
		if len(data) != 0 {
			t.Fatalf("server received %d bytes before payload, want none", len(data))
		}
	case <-time.After(time.Second):
		// pipe Read blocked until Close, then returned EOF with n==0
	}
}

func TestClientConnWriteSendsHeaderWithPayload(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	addr := socks5.ParseAddr("example.com:443")
	if addr == nil {
		t.Fatal("parse socks addr")
	}
	key := Key("password")
	conn := NewClientConn(client, key, CommandTCP, addr)

	payload := []byte("hello")
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 512)
		n, err := io.ReadAtLeast(server, buf, len(key)+len(payload))
		if err != nil {
			done <- nil
			return
		}
		done <- buf[:n]
	}()

	n, err := conn.Write(payload)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("write n=%d want %d", n, len(payload))
	}

	select {
	case data := <-done:
		if len(data) < KeyLength+2+1+len(addr)+2+len(payload) {
			t.Fatalf("short header+payload: %d", len(data))
		}
		if string(data[len(data)-len(payload):]) != string(payload) {
			t.Fatalf("payload suffix mismatch: %q", data[len(data)-len(payload):])
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for header")
	}
}
