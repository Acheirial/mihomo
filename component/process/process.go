package process

import (
	"errors"
	"net/netip"
	"time"

	"github.com/metacubex/mihomo/common/lru"
	C "github.com/metacubex/mihomo/constant"
)

var (
	ErrInvalidNetwork     = errors.New("invalid network")
	ErrPlatformNotSupport = errors.New("not support on this platform")
	ErrNotFound           = errors.New("process not found")
)

const (
	TCP = "tcp"
	UDP = "udp"

	processCacheSize = 256
	processCacheTTL  = 200 * time.Millisecond
)

type processCacheKey struct {
	network string
	ip      netip.Addr
	port    int
}

type processCacheEntry struct {
	uid     uint32
	path    string
	err     error
	expires time.Time
}

var (
	processCache = lru.New[processCacheKey, processCacheEntry](
		lru.WithSize[processCacheKey, processCacheEntry](processCacheSize),
	)
	processLookup = findProcessName
)

func FindProcessName(network string, srcIP netip.Addr, srcPort int) (uint32, string, error) {
	key := processCacheKey{network: network, ip: srcIP, port: srcPort}
	now := time.Now()
	if e, ok := processCache.Get(key); ok && now.Before(e.expires) {
		return e.uid, e.path, e.err
	}
	uid, path, err := processLookup(network, srcIP, srcPort)
	processCache.Set(key, processCacheEntry{
		uid:     uid,
		path:    path,
		err:     err,
		expires: now.Add(processCacheTTL),
	})
	return uid, path, err
}

// PackageNameResolver
// never change type traits because it's used in CMFA
type PackageNameResolver func(metadata *C.Metadata) (string, error)

// DefaultPackageNameResolver
// never change type traits because it's used in CMFA
var DefaultPackageNameResolver PackageNameResolver

func FindPackageName(metadata *C.Metadata) (string, error) {
	if resolver := DefaultPackageNameResolver; resolver != nil {
		return resolver(metadata)
	}
	return "", ErrPlatformNotSupport
}
