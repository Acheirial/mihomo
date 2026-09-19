package statistic

import (
	"io"
	"net"
	"syscall"
	"time"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/common/buf"
	N "github.com/metacubex/mihomo/common/net"
	"github.com/metacubex/mihomo/common/utils"
	C "github.com/metacubex/mihomo/constant"

	"github.com/gofrs/uuid/v5"
	"github.com/metacubex/sing/common/network"
)

type Tracker interface {
	ID() string
	Close() error
	Info() *TrackerInfo
	C.Connection
}

type TrackerInfo struct {
	UUID          uuid.UUID    `json:"id"`
	Metadata      *C.Metadata  `json:"metadata"`
	UploadTotal   atomic.Int64 `json:"upload"`
	DownloadTotal atomic.Int64 `json:"download"`
	Start         time.Time    `json:"start"`
	Chain         C.Chain      `json:"chains"`
	ProviderChain C.Chain      `json:"providerChains"`
	Rule          string       `json:"rule"`
	RulePayload   string       `json:"rulePayload"`
}

type tcpTracker struct {
	C.Conn `json:"-"`
	*TrackerInfo
	manager *Manager

	pushToManager bool `json:"-"`
	joined        bool `json:"-"`
}

func (tt *tcpTracker) ID() string {
	return tt.UUID.String()
}

func (tt *tcpTracker) Info() *TrackerInfo {
	return tt.TrackerInfo
}

func (tt *tcpTracker) Read(b []byte) (int, error) {
	n, err := tt.Conn.Read(b)
	download := int64(n)
	if tt.pushToManager {
		tt.manager.PushDownloaded(download)
	}
	tt.DownloadTotal.Add(download)
	return n, err
}

func (tt *tcpTracker) ReadBuffer(buffer *buf.Buffer) (err error) {
	err = tt.Conn.ReadBuffer(buffer)
	download := int64(buffer.Len())
	if tt.pushToManager {
		tt.manager.PushDownloaded(download)
	}
	tt.DownloadTotal.Add(download)
	return
}

func (tt *tcpTracker) UnwrapReader() (io.Reader, []N.CountFunc) {
	return tt.Conn, []N.CountFunc{func(download int64) {
		if tt.pushToManager {
			tt.manager.PushDownloaded(download)
		}
		tt.DownloadTotal.Add(download)
	}}
}

func (tt *tcpTracker) Write(b []byte) (int, error) {
	n, err := tt.Conn.Write(b)
	upload := int64(n)
	if tt.pushToManager {
		tt.manager.PushUploaded(upload)
	}
	tt.UploadTotal.Add(upload)
	return n, err
}

func (tt *tcpTracker) WriteBuffer(buffer *buf.Buffer) (err error) {
	upload := int64(buffer.Len())
	err = tt.Conn.WriteBuffer(buffer)
	if tt.pushToManager {
		tt.manager.PushUploaded(upload)
	}
	tt.UploadTotal.Add(upload)
	return
}

func (tt *tcpTracker) UnwrapWriter() (io.Writer, []N.CountFunc) {
	return tt.Conn, []N.CountFunc{func(upload int64) {
		if tt.pushToManager {
			tt.manager.PushUploaded(upload)
		}
		tt.UploadTotal.Add(upload)
	}}
}

func (tt *tcpTracker) Close() error {
	if tt.joined {
		tt.manager.Leave(tt)
	}
	return tt.Conn.Close()
}

func (tt *tcpTracker) Upstream() any {
	return tt.Conn
}

func (tt *tcpTracker) ReaderReplaceable() bool {
	return true
}

func (tt *tcpTracker) WriterReplaceable() bool {
	return true
}

func (tt *tcpTracker) SyscallConn() (syscall.RawConn, error) {
	if sc, ok := tt.Conn.(syscall.Conn); ok {
		return sc.SyscallConn()
	}
	return nil, syscall.EINVAL
}

func NewTCPTracker(conn C.Conn, manager *Manager, metadata *C.Metadata, rule C.Rule, uploadTotal int64, downloadTotal int64, pushToManager bool) *tcpTracker {
	metadata.RemoteDst = conn.RemoteDestination()

	t := &tcpTracker{
		Conn:    conn,
		manager: manager,
		TrackerInfo: &TrackerInfo{
			UUID:          utils.NewUUIDV4(),
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			ProviderChain: conn.ProviderChains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(uploadTotal),
			DownloadTotal: atomic.NewInt64(downloadTotal),
		},
		pushToManager: pushToManager,
	}

	if pushToManager {
		if uploadTotal > 0 {
			manager.PushUploaded(uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloaded(downloadTotal)
		}
	}

	if rule != nil {
		t.TrackerInfo.Rule = rule.RuleType().String()
		t.TrackerInfo.RulePayload = rule.Payload()
	}

	if pushToManager {
		manager.Join(t)
		t.joined = true
	}
	return t
}

type udpTracker struct {
	C.PacketConn `json:"-"`
	*TrackerInfo
	manager *Manager

	pushToManager bool `json:"-"`
	joined        bool `json:"-"`
}

func (ut *udpTracker) ID() string {
	return ut.UUID.String()
}

func (ut *udpTracker) Info() *TrackerInfo {
	return ut.TrackerInfo
}

func (ut *udpTracker) ReadFrom(b []byte) (int, net.Addr, error) {
	n, addr, err := ut.PacketConn.ReadFrom(b)
	download := int64(n)
	if ut.pushToManager {
		ut.manager.PushDownloaded(download)
	}
	ut.DownloadTotal.Add(download)
	return n, addr, err
}

func (ut *udpTracker) WaitReadFrom() (data []byte, put func(), addr net.Addr, err error) {
	data, put, addr, err = ut.PacketConn.WaitReadFrom()
	download := int64(len(data))
	if ut.pushToManager {
		ut.manager.PushDownloaded(download)
	}
	ut.DownloadTotal.Add(download)
	return
}

func (ut *udpTracker) WriteTo(b []byte, addr net.Addr) (int, error) {
	n, err := ut.PacketConn.WriteTo(b, addr)
	upload := int64(n)
	if ut.pushToManager {
		ut.manager.PushUploaded(upload)
	}
	ut.UploadTotal.Add(upload)
	return n, err
}

func (ut *udpTracker) Close() error {
	if ut.joined {
		ut.manager.Leave(ut)
	}
	return ut.PacketConn.Close()
}

func (ut *udpTracker) Upstream() any {
	return ut.PacketConn
}

func (ut *udpTracker) ReaderReplaceable() bool {
	return true
}

func (ut *udpTracker) WriterReplaceable() bool {
	return true
}

func (ut *udpTracker) UnwrapPacketReader() (network.PacketReader, []N.CountFunc) {
	count := []N.CountFunc{func(download int64) {
		if ut.pushToManager {
			ut.manager.PushDownloaded(download)
		}
		ut.DownloadTotal.Add(download)
	}}
	if r, ok := ut.PacketConn.(network.PacketReader); ok {
		return r, count
	}
	return nil, count
}

func (ut *udpTracker) UnwrapPacketWriter() (network.PacketWriter, []N.CountFunc) {
	count := []N.CountFunc{func(upload int64) {
		if ut.pushToManager {
			ut.manager.PushUploaded(upload)
		}
		ut.UploadTotal.Add(upload)
	}}
	if w, ok := ut.PacketConn.(network.PacketWriter); ok {
		return w, count
	}
	return nil, count
}

func NewUDPTracker(conn C.PacketConn, manager *Manager, metadata *C.Metadata, rule C.Rule, uploadTotal int64, downloadTotal int64, pushToManager bool) *udpTracker {
	metadata.RemoteDst = conn.RemoteDestination()

	ut := &udpTracker{
		PacketConn: conn,
		manager:    manager,
		TrackerInfo: &TrackerInfo{
			UUID:          utils.NewUUIDV4(),
			Start:         time.Now(),
			Metadata:      metadata,
			Chain:         conn.Chains(),
			ProviderChain: conn.ProviderChains(),
			Rule:          "",
			UploadTotal:   atomic.NewInt64(uploadTotal),
			DownloadTotal: atomic.NewInt64(downloadTotal),
		},
		pushToManager: pushToManager,
	}

	if pushToManager {
		if uploadTotal > 0 {
			manager.PushUploaded(uploadTotal)
		}
		if downloadTotal > 0 {
			manager.PushDownloaded(downloadTotal)
		}
	}

	if rule != nil {
		ut.TrackerInfo.Rule = rule.RuleType().String()
		ut.TrackerInfo.RulePayload = rule.Payload()
	}

	if pushToManager {
		manager.Join(ut)
		ut.joined = true
	}
	return ut
}
