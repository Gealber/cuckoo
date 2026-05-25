# cuckoo

A Go implementation of a cuckoo filter — a probabilistic data structure for set
membership testing that supports deletion. This implementation is a port of the
[Rust cuckoo filter](https://github.com/rpcpool/yellowstone-grpc/tree/master/yellowstone-grpc-proto/src/cuckoo)
from [yellowstone-grpc](https://github.com/rpcpool/yellowstone-grpc), and is
wire-compatible with it.

## Overview

A cuckoo filter answers "is this item in the set?" with a configurable
false-positive rate and zero false negatives. Unlike Bloom filters, cuckoo
filters support **deletion** of items.

Both this implementation and the Rust original use:
- 16-bit fingerprints, 4 slots per bucket
- [SipHash-2-4](https://131002.net/siphash/) with seed-derived keys: `k0 = seed`, `k1 = seed.rotate_left(32)`
- Default seed `0x796c6c77_7374_6e21` ("yllwstn!")
- 95% load factor target, bucket count rounded up to the next power of 2
- Up to 500 cuckoo kicks before declaring the table full

False positive rate is ~0.012% or below at full capacity.

## Installation

```bash
go get github.com/Gealber/cuckoo
```

## Usage

```go
import "github.com/Gealber/cuckoo"

cf := cuckoo.New(10_000) // capacity hint, uses DefaultSeed

cf.Insert([]byte("hello"))

cf.Lookup([]byte("hello")) // true
cf.Lookup([]byte("world")) // false (probably)

cf.Delete([]byte("hello"))
cf.Lookup([]byte("hello")) // false

cf.Count() // number of items currently in the filter
```

### API

| Method | Description |
|---|---|
| `New(capacity uint) *Filter` | Create a filter sized for approximately `capacity` items |
| `NewWithSeed(capacity uint, seed uint64) *Filter` | Create a filter with an explicit seed, using SipHash |
| `NewWithOptions(capacity uint, seed uint64, algo HashAlgorithm) *Filter` | Create a filter with an explicit seed and hash algorithm |
| `Insert(key []byte) bool` | Insert a key; returns `false` if the table is full |
| `Lookup(key []byte) bool` | Test membership; may return false positives |
| `Delete(key []byte) bool` | Remove a key; returns `false` if not found |
| `Count() uint` | Number of items currently stored |
| `Seed() uint64` | Return the hash seed (needed for serialization) |
| `Algorithm() HashAlgorithm` | Return the hash algorithm (needed for serialization) |
| `Bytes() []byte` | Serialize buckets as little-endian u16 values |
| `FromBytes(data []byte, seed uint64, algo HashAlgorithm) (*Filter, error)` | Deserialize from raw bucket bytes, seed, and hash algorithm |

### Hash algorithms

| Constant | Value | Description |
|---|---|---|
| `SipHash` | `0` | SipHash-2-4 — default, wire-compatible with yellowstone-grpc |

## Wire compatibility with Rust

A filter built in Rust and serialized to the `CuckooFilter` proto message can
be deserialized and queried directly in Go:

```go
// proto is a deserialized CuckooFilter protobuf message from yellowstone-grpc
cf, err := cuckoo.FromBytes(proto.Data, proto.HashSeed)
if err != nil {
    // handle
}

cf.Lookup(pubkey[:]) // true if pubkey was in the Rust filter
```

The Rust proto wire format maps to Go as follows:

| Proto field | Go equivalent |
|---|---|
| `data` | raw bytes passed to `FromBytes` |
| `hash_seed` | seed passed to `FromBytes` / returned by `Seed()` |
| `bucket_count` | `len(cf.Bytes()) / 8` |
| `entries_per_bucket` | always 4 |
| `fingerprint_bits` | always 16 |

> **Key encoding:** Rust's `Hash` trait for `[u8; N]` prepends the slice length
> as a little-endian u64 before the bytes. This library replicates that encoding,
> so a 32-byte Solana pubkey `pk` must be passed as `pk[:]`.

Cross-language compatibility is verified by `TestCrossCompatVectors` and
`TestCrossCompatSingleInsert` in `cuckoo_test.go`, with a companion Rust test
in `compat_rust_test.rs` that asserts identical hash values and byte layouts.

## Benchmarks

Run on an Intel Core Ultra 7 255U (SipHash-2-4, all operations allocation-free):

```
BenchmarkInsert-14        22589847    483 ns/op    0 B/op    0 allocs/op
BenchmarkLookupHit-14     33781297    485 ns/op    0 B/op    0 allocs/op
BenchmarkLookupMiss-14    26480258    550 ns/op    0 B/op    0 allocs/op
BenchmarkDelete-14        30668930    499 ns/op    0 B/op    0 allocs/op
```

> **Note:** Performance is not the primary goal of this library. The hash
> function (SipHash-2-4), key encoding, and alternate-index formula are all
> chosen to match the Rust implementation exactly, not to maximise throughput.
> A Go-only filter using xxhash would be roughly 2× faster, but would not be
> wire-compatible with yellowstone-grpc.

To run benchmarks yourself:

```bash
go test -bench=. -benchmem ./...
```
