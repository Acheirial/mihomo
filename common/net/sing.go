package net

import (
	"io"
	"net"
	"syscall"

	"github.com/metacubex/mihomo/common/net/deadline"
	"github.com/metacubex/mihomo/common/pool"

	"github.com/metacubex/sing/common"
	"github.com/metacubex/sing/common/bufio"
	"github.com/metacubex/sing/common/network"
)

var NewExtendedConn = bufio.NewExtendedConn
var NewExtendedWriter = bufio.NewExtendedWriter
var NewExtendedReader = bufio.NewExtendedReader

type ExtendedConn = network.ExtendedConn
type ExtendedWriter = network.ExtendedWriter
type ExtendedReader = network.ExtendedReader

var WriteBuffer = bufio.WriteBuffer

type ReadWaitOptions = network.ReadWaitOptions

var NewReadWaitOptions = network.NewReadWaitOptions
var CalculateFrontHeadroom = network.CalculateFrontHeadroom
var CalculateRearHeadroom = network.CalculateRearHeadroom

type ReaderWithUpstream = network.ReaderWithUpstream
type WithUpstreamReader = network.WithUpstreamReader
type WriterWithUpstream = network.WriterWithUpstream
type WithUpstreamWriter = network.WithUpstreamWriter
type WithUpstream = common.WithUpstream

var UnwrapReader = network.UnwrapReader
var UnwrapWriter = network.UnwrapWriter

func NewDeadlineConn(conn net.Conn) ExtendedConn {
	if deadline.IsPipe(conn) || deadline.IsPipe(UnwrapReader(conn)) {
		return NewExtendedConn(conn) // pipe always have correctly deadline implement
	}
	if deadline.IsConn(conn) || deadline.IsConn(UnwrapReader(conn)) {
		return NewExtendedConn(conn) // was a *deadline.Conn
	}
	return deadline.NewConn(conn)
}

func NeedHandshake(conn any) bool {
	if earlyConn, isEarlyConn := common.Cast[network.EarlyConn](conn); isEarlyConn && earlyConn.NeedHandshake() {
		return true
	}
	return false
}

type CountFunc = network.CountFunc

var Pipe = deadline.Pipe

func closeWrite(writer io.Closer) error {
	if c, ok := common.Cast[network.WriteCloser](writer); ok {
		return c.CloseWrite()
	}
	return writer.Close()
}

// Relay copies between left and right bidirectionally.
// like [bufio.CopyConn] but remove unneeded [context.Context] handle and the cost of [task.Group]
func Relay(leftConn, rightConn net.Conn) {
	defer func() {
		_ = leftConn.Close()
		_ = rightConn.Close()
	}()

	ch := make(chan struct{})
	go func() {
		_, err := copyWithIncrease(leftConn, rightConn)
		if err == nil {
			_ = closeWrite(leftConn)
		} else {
			_ = leftConn.Close()
		}
		close(ch)
	}()

	_, err := copyWithIncrease(rightConn, leftConn)
	if err == nil {
		_ = closeWrite(rightConn)
	} else {
		_ = rightConn.Close()
	}
	<-ch
}

const copyIncreaseThreshold = 512 * 1024

func copyWithIncrease(dst io.Writer, src io.Reader) (int64, error) {
	originSrc := src
	var readCounters, writeCounters []network.CountFunc
	src, readCounters = collectCountReader(src, readCounters)
	dst, writeCounters = collectCountWriter(dst, writeCounters)

	var cachedN int64
	for {
		src, readCounters = network.UnwrapCountReader(src, readCounters)
		dst, writeCounters = network.UnwrapCountWriter(dst, writeCounters)
		cached, ok := src.(network.CachedReader)
		if !ok {
			break
		}
		buffer := cached.ReadCached()
		if buffer == nil {
			break
		}
		dataLen := buffer.Len()
		_, err := dst.Write(buffer.Bytes())
		buffer.Release()
		if err != nil {
			return cachedN, err
		}
		n := int64(dataLen)
		cachedN += n
		for _, counter := range readCounters {
			counter(n)
		}
		for _, counter := range writeCounters {
			counter(n)
		}
	}

	_, srcOK := src.(syscall.Conn)
	_, dstOK := dst.(syscall.Conn)
	if srcOK && dstOK {
		n, err := bufio.CopyWithCounters(dst, src, originSrc, readCounters, writeCounters)
		n += cachedN
		if err == nil {
			return n, io.EOF
		}
		return n, err
	}

	return copyPooledIncrease(dst, src, cachedN, readCounters, writeCounters)
}

// collectCountReader peels ReadCounter wrappers before replaceable unwrap.
// sing UnwrapCountReader calls UnwrapReader first, which would skip a
// ReaderReplaceable tracker and drop its CountFunc.
func collectCountReader(src io.Reader, counts []network.CountFunc) (io.Reader, []network.CountFunc) {
	for {
		c, ok := src.(network.ReadCounter)
		if !ok {
			break
		}
		var extra []network.CountFunc
		src, extra = c.UnwrapReader()
		counts = append(counts, extra...)
	}
	return src, counts
}

func collectCountWriter(dst io.Writer, counts []network.CountFunc) (io.Writer, []network.CountFunc) {
	for {
		c, ok := dst.(network.WriteCounter)
		if !ok {
			break
		}
		var extra []network.CountFunc
		dst, extra = c.UnwrapWriter()
		counts = append(counts, extra...)
	}
	return dst, counts
}

func copyPooledIncrease(dst io.Writer, src io.Reader, written int64, readCounters, writeCounters []network.CountFunc) (int64, error) {
	n := pool.RelayBufferSize
	buf := pool.Get(n)
	defer func() { pool.Put(buf) }()
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[:nr])
			if nw > 0 {
				wn := int64(nw)
				written += wn
				for _, counter := range readCounters {
					counter(wn)
				}
				for _, counter := range writeCounters {
					counter(wn)
				}
			}
			if ew != nil {
				return written, ew
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}
		if er != nil {
			if er == io.EOF {
				return written, io.EOF
			}
			return written, er
		}
		if n == pool.RelayBufferSize && written > copyIncreaseThreshold {
			n = 65535
			pool.Put(buf)
			buf = pool.Get(n)
		}
	}
}
