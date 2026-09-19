package trojan

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/metacubex/mihomo/common/buf"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/pool"
	"github.com/metacubex/mihomo/transport/socks5"
)

const (
	// max packet length
	maxLength = 8192
)

var (
	DefaultALPN          = []string{"h2", "http/1.1"}
	DefaultWebsocketALPN = []string{"http/1.1"}

	crlf = []byte{'\r', '\n'}
)

type Command = byte

const (
	CommandTCP byte = 1
	CommandUDP byte = 3
	CommandMux byte = 0x7f

	KeyLength = 56
)

func WriteHeader(w io.Writer, hexPassword [KeyLength]byte, command Command, socks5Addr []byte) error {
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)

	buf.Write(hexPassword[:])
	buf.Write(crlf)

	buf.WriteByte(command)
	buf.Write(socks5Addr)
	buf.Write(crlf)

	_, err := w.Write(buf.Bytes())
	return err
}

const clientConnFrontHeadroom = KeyLength + 2 + 1 + socks5.MaxAddrLen + 2

var _ N.ExtendedConn = (*ClientConn)(nil)

// ClientConn delays the Trojan protocol header until the first Write.
// Closing without a Write sends nothing to the server.
type ClientConn struct {
	N.ExtendedConn
	key           [KeyLength]byte
	command       Command
	socks5Addr    []byte
	headerWritten bool
}

func NewClientConn(conn net.Conn, key [KeyLength]byte, command Command, socks5Addr []byte) *ClientConn {
	return &ClientConn{
		ExtendedConn: N.NewExtendedConn(conn),
		key:          key,
		command:      command,
		socks5Addr:   socks5Addr,
	}
}

func (c *ClientConn) NeedHandshake() bool {
	return !c.headerWritten
}

func (c *ClientConn) NeedHandshakeForWrite() bool {
	return !c.headerWritten
}

func (c *ClientConn) writeHeader(payload []byte) error {
	headerLen := KeyLength + 2 + 1 + len(c.socks5Addr) + 2
	buffer := buf.NewSize(headerLen + len(payload))
	defer buffer.Release()
	buf.Must(
		buf.Error(buffer.Write(c.key[:])),
		buf.Error(buffer.Write(crlf)),
		buffer.WriteByte(c.command),
		buf.Error(buffer.Write(c.socks5Addr)),
		buf.Error(buffer.Write(crlf)),
		buf.Error(buffer.Write(payload)),
	)
	_, err := c.ExtendedConn.Write(buffer.Bytes())
	return err
}

func (c *ClientConn) Write(p []byte) (n int, err error) {
	if c.headerWritten {
		return c.ExtendedConn.Write(p)
	}
	err = c.writeHeader(p)
	if err != nil {
		return
	}
	n = len(p)
	c.headerWritten = true
	return
}

func (c *ClientConn) WriteBuffer(buffer *buf.Buffer) error {
	if c.headerWritten {
		return c.ExtendedConn.WriteBuffer(buffer)
	}
	headerLen := KeyLength + 2 + 1 + len(c.socks5Addr) + 2
	if buffer.Start() >= headerLen {
		header := buffer.ExtendHeader(headerLen)
		copy(header, c.key[:])
		copy(header[KeyLength:], crlf)
		header[KeyLength+2] = c.command
		copy(header[KeyLength+3:], c.socks5Addr)
		copy(header[headerLen-2:], crlf)
		err := c.ExtendedConn.WriteBuffer(buffer)
		if err != nil {
			return err
		}
		c.headerWritten = true
		return nil
	}
	err := c.writeHeader(buffer.Bytes())
	if err != nil {
		return err
	}
	buffer.Advance(buffer.Len())
	c.headerWritten = true
	return nil
}

func (c *ClientConn) FrontHeadroom() int {
	if !c.headerWritten {
		return clientConnFrontHeadroom
	}
	return 0
}

func (c *ClientConn) Upstream() any {
	return c.ExtendedConn
}

func (c *ClientConn) ReaderReplaceable() bool {
	return c.headerWritten
}

func (c *ClientConn) WriterReplaceable() bool {
	return c.headerWritten
}

func writePacket(w io.Writer, socks5Addr, payload []byte) (int, error) {
	buf := pool.GetBuffer()
	defer pool.PutBuffer(buf)

	buf.Write(socks5Addr)
	binary.Write(buf, binary.BigEndian, uint16(len(payload)))
	buf.Write(crlf)
	buf.Write(payload)

	if _, err := w.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func WritePacket(w io.Writer, socks5Addr, payload []byte) (int, error) {
	if len(payload) <= maxLength {
		return writePacket(w, socks5Addr, payload)
	}

	offset := 0
	total := len(payload)
	for {
		cursor := offset + maxLength
		if cursor > total {
			cursor = total
		}

		n, err := writePacket(w, socks5Addr, payload[offset:cursor])
		if err != nil {
			return offset + n, err
		}

		offset = cursor
		if offset == total {
			break
		}
	}

	return total, nil
}

func ReadPacket(r io.Reader, payload []byte) (net.Addr, int, int, error) {
	addr, err := socks5.ReadAddr(r, payload)
	if err != nil {
		return nil, 0, 0, errors.New("read addr error")
	}
	uAddr := addr.UDPAddr()
	if uAddr == nil {
		return nil, 0, 0, errors.New("parse addr error")
	}

	if _, err = io.ReadFull(r, payload[:2]); err != nil {
		return nil, 0, 0, errors.New("read length error")
	}

	total := int(binary.BigEndian.Uint16(payload[:2]))
	if total > maxLength {
		return nil, 0, 0, errors.New("packet invalid")
	}

	// read crlf
	if _, err = io.ReadFull(r, payload[:2]); err != nil {
		return nil, 0, 0, errors.New("read crlf error")
	}

	length := len(payload)
	if total < length {
		length = total
	}

	if _, err = io.ReadFull(r, payload[:length]); err != nil {
		return nil, 0, 0, errors.New("read packet error")
	}

	return uAddr, length, total - length, nil
}

var _ N.EnhancePacketConn = (*PacketConn)(nil)

type PacketConn struct {
	net.Conn
	remain int
	rAddr  net.Addr
	mux    sync.Mutex
}

func (pc *PacketConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	return WritePacket(pc, socks5.ParseAddrToSocksAddr(addr), b)
}

func (pc *PacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	pc.mux.Lock()
	defer pc.mux.Unlock()
	if pc.remain != 0 {
		length := len(b)
		if pc.remain < length {
			length = pc.remain
		}

		n, err := pc.Conn.Read(b[:length])
		if err != nil {
			return 0, nil, err
		}

		pc.remain -= n
		addr := pc.rAddr
		if pc.remain == 0 {
			pc.rAddr = nil
		}

		return n, addr, nil
	}

	addr, n, remain, err := ReadPacket(pc.Conn, b)
	if err != nil {
		return 0, nil, err
	}

	if remain != 0 {
		pc.remain = remain
		pc.rAddr = addr
	}

	return n, addr, nil
}

func (pc *PacketConn) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	pc.mux.Lock()
	defer pc.mux.Unlock()

	destination, err := socks5.ReadAddr0(pc.Conn)
	if err != nil {
		return nil, nil, nil, err
	}
	udpAddr := destination.UDPAddr()
	if udpAddr == nil {
		return nil, nil, nil, errors.New("parse addr error")
	}
	addr = udpAddr

	data = pool.Get(pool.UDPBufferSize)
	put = func() {
		_ = pool.Put(data)
	}

	_, err = io.ReadFull(pc.Conn, data[:2+2]) // u16be length + CR LF
	if err != nil {
		if put != nil {
			put()
		}
		return nil, nil, nil, err
	}
	length := binary.BigEndian.Uint16(data)
	if length > maxLength {
		if put != nil {
			put()
		}
		return nil, nil, nil, errors.New("packet invalid")
	}

	if length > 0 {
		data = data[:length]
		_, err = io.ReadFull(pc.Conn, data)
		if err != nil {
			if put != nil {
				put()
			}
			return nil, nil, nil, err
		}
	} else {
		if put != nil {
			put()
		}
		return nil, nil, addr, nil
	}

	return
}

func NewPacketConn(conn net.Conn) *PacketConn {
	return &PacketConn{Conn: conn}
}

func Key(password string) (key [56]byte) {
	hash := sha256.Sum224([]byte(password))
	hex.Encode(key[:], hash[:])
	return
}
