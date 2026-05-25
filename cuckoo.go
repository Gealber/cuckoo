package cuckoo

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"math/rand"

	"github.com/dchest/siphash"
)

var (
	ErrDataLengthNotAligned = errors.New("cuckoo: data length not aligned to bucket size")
)

const (
	bucketSize = 4
	maxKicks   = 500
	loadFactor = 0.95

	// DefaultSeed is "yllwstn!" in ASCII, matching Rust's DEFAULT_HASH_SEED.
	DefaultSeed = uint64(0x796c6c77_7374_6e21)
)

// HashAlgorithm identifies the hash function used by a filter.
// The zero value (SipHash) is the default and matches the Rust yellowstone-grpc implementation.
type HashAlgorithm uint8

const (
	SipHash HashAlgorithm = iota
)

type fingerprint = uint16

type bucket [bucketSize]fingerprint

// Filter is a cuckoo filter for approximate set membership testing with deletion support.
type Filter struct {
	buckets []bucket
	count   uint
	m       uint          // number of buckets, always a power of 2
	seed    uint64        // hash seed
	algo    HashAlgorithm // hash algorithm
}

// New creates a filter sized for approximately capacity items using DefaultSeed and SipHash.
func New(capacity uint) *Filter {
	return NewWithOptions(capacity, DefaultSeed, SipHash)
}

// NewWithSeed creates a filter with an explicit seed, using SipHash.
func NewWithSeed(capacity uint, seed uint64) *Filter {
	return NewWithOptions(capacity, seed, SipHash)
}

// NewWithOptions creates a filter with an explicit seed and hash algorithm.
func NewWithOptions(capacity uint, seed uint64, algo HashAlgorithm) *Filter {
	m := numBuckets(capacity)

	return &Filter{
		buckets: make([]bucket, m),
		m:       m,
		seed:    seed,
		algo:    algo,
	}
}

// numBuckets computes the bucket count for a given capacity.
// Matches Rust: ceil(capacity / (loadFactor * bucketSize)), rounded up to the next power of 2.
func numBuckets(capacity uint) uint {
	if capacity == 0 {
		return 1
	}

	needed := uint(math.Ceil(float64(capacity) / (loadFactor * bucketSize)))
	return nextPow2(needed)
}

func nextPow2(x uint) uint {
	if x <= 1 {
		return 1
	}
	return 1 << bits.Len(x-1)
}

// sipKeys derives the two SipHash-2-4 keys from the filter's seed.
// Matches Rust's YellowstoneHasherBuilder: k0 = seed, k1 = seed.rotate_left(32).
func (cf *Filter) sipKeys() (uint64, uint64) {
	return cf.seed, bits.RotateLeft64(cf.seed, 32)
}

// hashOf returns the primary bucket index and fingerprint for key.
func (cf *Filter) hashOf(key []byte) (uint, fingerprint) {
	switch cf.algo {
	case SipHash:
		k0, k1 := cf.sipKeys()

		buf := make([]byte, 8+len(key))
		binary.LittleEndian.PutUint64(buf[:8], uint64(len(key)))
		copy(buf[8:], key)

		h := siphash.Hash(k0, k1, buf)

		f := fingerprint(h >> 32)
		if f == 0 {
			f = 1
		}

		return uint(h) & (cf.m - 1), f
	default:
		panic(fmt.Sprintf("cuckoo: unsupported hash algorithm %d", cf.algo))
	}
}

// altIndex returns the alternate bucket index for (i, f).
// Satisfies altIndex(altIndex(i, f), f) == i.
func (cf *Filter) altIndex(i uint, f fingerprint) uint {
	switch cf.algo {
	case SipHash:
		k0, k1 := cf.sipKeys()

		var fpBuf [2]byte
		binary.LittleEndian.PutUint16(fpBuf[:], uint16(f))

		h := siphash.Hash(k0, k1, fpBuf[:])

		return i ^ (uint(h) & (cf.m - 1))
	default:
		panic(fmt.Sprintf("cuckoo: unsupported hash algorithm %d", cf.algo))
	}
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

	return false
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

// Seed returns the hash seed. Include this in any serialized form so
// the receiver can reconstruct a matching hasher (mirrors Rust's proto hash_seed field).
func (cf *Filter) Seed() uint64 { return cf.seed }

// Algorithm returns the hash algorithm used by this filter.
func (cf *Filter) Algorithm() HashAlgorithm { return cf.algo }


// Bytes serializes the filter's buckets as little-endian u16 values,
// matching the Rust proto wire format's data field.
func (cf *Filter) Bytes() []byte {
	data := make([]byte, len(cf.buckets)*bucketSize*2)

	for i, b := range cf.buckets {
		for j, fp := range b {
			binary.LittleEndian.PutUint16(data[(i*bucketSize+j)*2:], uint16(fp))
		}
	}

	return data
}

// FromBytes deserializes a filter from raw bucket bytes, a seed, and the hash algorithm
// used when the filter was built. Matches the Rust proto wire format (data + hash_seed + hash_algorithm).
func FromBytes(data []byte, seed uint64, algo HashAlgorithm) (*Filter, error) {
	const bytesPerBucket = bucketSize * 2

	if len(data)%bytesPerBucket != 0 {
		return nil, ErrDataLengthNotAligned
	}

	numBucks := len(data) / bytesPerBucket
	if numBucks == 0 {
		numBucks = 1
		data = make([]byte, bytesPerBucket)
	}

	buckets := make([]bucket, numBucks)
	for i := range buckets {
		for j := range bucketSize {
			buckets[i][j] = fingerprint(binary.LittleEndian.Uint16(data[(i*bucketSize+j)*2:]))
		}
	}

	return &Filter{
		buckets: buckets,
		m:       uint(numBucks),
		seed:    seed,
		algo:    algo,
	}, nil
}
