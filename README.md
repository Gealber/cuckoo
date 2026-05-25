# cuckoo

A cuckoo filter implementation in Go — a probabilistic data structure for set membership testing that supports deletion.

## Overview

A cuckoo filter answers "is this item in the set?" with a configurable false-positive rate and zero false negatives. Unlike Bloom filters, cuckoo filters support **deletion** of items.

This implementation uses:
- 16-bit fingerprints (4 per bucket)
- [xxhash v2](https://github.com/cespare/xxhash) for hashing
- MurmurHash2's multiply constant for the alternate-index calculation

False positive rate is ~0.012% or below at full capacity.

## Installation

```bash
go get github.com/Gealber/cuckoo
```

## Usage

```go
import "github.com/Gealber/cuckoo"

cf := cuckoo.New(10_000) // capacity hint

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
| `Insert(key []byte) bool` | Insert a key; returns `false` if the table is full |
| `Lookup(key []byte) bool` | Test membership; may return false positives |
| `Delete(key []byte) bool` | Remove a key; returns `false` if not found |
| `Count() uint` | Number of items currently stored |

## Benchmarks

Run on an Intel Core Ultra 7 255U:

```
BenchmarkInsert-14        29681710    252.6 ns/op    0 B/op    0 allocs/op
BenchmarkLookupHit-14     56597416    139.4 ns/op    0 B/op    0 allocs/op
BenchmarkLookupMiss-14   100000000    155.5 ns/op    0 B/op    0 allocs/op
BenchmarkDelete-14        56801613    124.6 ns/op    0 B/op    0 allocs/op
```

All operations are allocation-free after the initial filter creation.

To run benchmarks yourself:

```bash
go test -bench=. -benchmem ./...
```
