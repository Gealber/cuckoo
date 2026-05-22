package cuckoo

import (
	"math/bits"
	"math/rand"

	"github.com/cespare/xxhash/v2"
)

const (
	bucketSize   = 4
	maxKicks     = 500
	murmurHash2c = uint32(0x5bd1e995) // multiply constant from MurmurHash2
)

type fingerprint = uint8

type bucket [bucketSize]fingerprint

type Filter struct {
	buckets []bucket
	count   uint
	m       uint // number of buckets, always a power of 2
}

func New(capacity uint) *Filter {
	m := nextPow2(capacity / bucketSize)
	return &Filter{
		buckets: make([]bucket, m),
		m:       m,
	}
}

func nextPow2(x uint) uint {
	if x <= 1 {
		return 1
	}
	return 1 << bits.Len(x-1)
}

func (cf *Filter) hashOf(key []byte) (uint, fingerprint) {
	h := xxhash.Sum64(key)
	// Top byte -> fingerprint, bottom bits -> bucket index.
	// Using opposite ends of the same hash avoids correlation between the two.
	f := fingerprint(h >> 56)
	if f == 0 {
		f = 1
	}
	return uint(h) & (cf.m - 1), f
}

// altIndex satisfies altIndex(altIndex(i, f), f) == i.
// A single multiply by the MurmurHash2 constant is enough to spread the
// displacement across the table — no second hash call needed.
func (cf *Filter) altIndex(i uint, f fingerprint) uint {
	return (i ^ uint(uint32(f)*murmurHash2c)) & (cf.m - 1)
}

func (b *bucket) insert(f fingerprint) bool {
	for j := range b {
		if b[j] == 0 {
			b[j] = f
			return true
		}
	}
	return false
}

func (b *bucket) contains(f fingerprint) bool {
	for j := range b {
		if b[j] == f {
			return true
		}
	}
	return false
}

func (b *bucket) delete(f fingerprint) bool {
	for j := range b {
		if b[j] == f {
			b[j] = 0
			return true
		}
	}
	return false
}

func (cf *Filter) Insert(key []byte) bool {
	i1, f := cf.hashOf(key)
	i2 := cf.altIndex(i1, f)

	if cf.buckets[i1].insert(f) {
		cf.count++
		return true
	}
	if cf.buckets[i2].insert(f) {
		cf.count++
		return true
	}

	i := i1
	if rand.Intn(2) == 1 {
		i = i2
	}

	for range maxKicks {
		e := rand.Intn(bucketSize)
		f, cf.buckets[i][e] = cf.buckets[i][e], f
		i = cf.altIndex(i, f)
		if cf.buckets[i].insert(f) {
			cf.count++
			return true
		}
	}

	return false // table is full
}

func (cf *Filter) Lookup(key []byte) bool {
	i1, f := cf.hashOf(key)
	i2 := cf.altIndex(i1, f)
	return cf.buckets[i1].contains(f) || cf.buckets[i2].contains(f)
}

func (cf *Filter) Delete(key []byte) bool {
	i1, f := cf.hashOf(key)
	i2 := cf.altIndex(i1, f)
	if cf.buckets[i1].delete(f) {
		cf.count--
		return true
	}
	if cf.buckets[i2].delete(f) {
		cf.count--
		return true
	}
	return false
}

func (cf *Filter) Count() uint { return cf.count }
