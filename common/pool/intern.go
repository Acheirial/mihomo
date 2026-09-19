package pool

import (
	"sync"
)

const (
	internShards     = 16
	internShardMask  = internShards - 1
	maxInternPerShard = 1024
)

type internShard struct {
	sync.RWMutex
	m map[string]string
}

type InternPool struct {
	shards [internShards]internShard
}

var defaultInternPool = NewInternPool()

func NewInternPool() *InternPool {
	p := &InternPool{}
	for i := 0; i < internShards; i++ {
		p.shards[i].m = make(map[string]string)
	}
	return p
}

// hashString returns a simple fnv32a hash of s for shard selection.
func hashString(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// Intern returns a canonicalized copy of s, reusing existing allocations.
func (p *InternPool) Intern(s string) string {
	if len(s) == 0 {
		return ""
	}

	shardIdx := hashString(s) & internShardMask
	shard := &p.shards[shardIdx]

	shard.RLock()
	interned, ok := shard.m[s]
	shard.RUnlock()
	if ok {
		return interned
	}

	shard.Lock()
	if interned, ok = shard.m[s]; ok {
		shard.Unlock()
		return interned
	}

	// Limit shard memory if an unbounded number of unique strings are passed
	if len(shard.m) >= maxInternPerShard {
		shard.Unlock()
		return s
	}

	shard.m[s] = s
	shard.Unlock()
	return s
}

// Intern returns a canonicalized version of s using the default intern pool.
func Intern(s string) string {
	return defaultInternPool.Intern(s)
}
