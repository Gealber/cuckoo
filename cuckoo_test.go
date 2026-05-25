package cuckoo

import (
	"fmt"
	"testing"
)

func TestInsertAndLookup(t *testing.T) {
	cf := New(1024)

	keys := [][]byte{
		[]byte("apple"),
		[]byte("banana"),
		[]byte("cherry"),
	}

	for _, k := range keys {
		if !cf.Insert(k) {
			t.Fatalf("Insert(%q) returned false", k)
		}
	}

	for _, k := range keys {
		if !cf.Lookup(k) {
			t.Errorf("Lookup(%q) = false, want true after insert", k)
		}
	}
}

func TestLookupMiss(t *testing.T) {
	cf := New(1024)
	cf.Insert([]byte("hello"))

	if cf.Lookup([]byte("world")) {
		t.Error("Lookup(\"world\") = true, want false (key was never inserted)")
	}
}

func TestInsertCount(t *testing.T) {
	cf := New(1024)
	n := 100

	for i := range n {
		cf.Insert([]byte(fmt.Sprintf("key-%d", i)))
	}

	if cf.Count() != uint(n) {
		t.Errorf("Count() = %d, want %d", cf.Count(), n)
	}
}

func TestDelete(t *testing.T) {
	cf := New(1024)
	key := []byte("delete-me")

	cf.Insert(key)
	if !cf.Delete(key) {
		t.Fatal("Delete returned false for an inserted key")
	}
	if cf.Lookup(key) {
		t.Error("Lookup returned true after Delete")
	}
	if cf.Count() != 0 {
		t.Errorf("Count() = %d, want 0 after delete", cf.Count())
	}
}

func TestDeleteMiss(t *testing.T) {
	cf := New(1024)

	if cf.Delete([]byte("never-inserted")) {
		t.Error("Delete returned true for a key that was never inserted")
	}
}

func TestDeleteOneOfDuplicates(t *testing.T) {
	cf := New(1024)
	key := []byte("dup")

	cf.Insert(key)
	cf.Insert(key) // two copies of the same fingerprint stored

	cf.Delete(key) // removes one copy
	if !cf.Lookup(key) {
		t.Error("Lookup returned false after deleting one of two copies")
	}
}

func TestFalsePositiveRate(t *testing.T) {
	const total = 10_000
	cf := New(total)

	for i := range total {
		cf.Insert([]byte(fmt.Sprintf("insert-%d", i)))
	}

	falsePositives := 0
	for i := range total {
		if cf.Lookup([]byte(fmt.Sprintf("query-%d", i))) {
			falsePositives++
		}
	}

	fpr := float64(falsePositives) / total
	// 16-bit fingerprints with b=4: upper bound ~0.012%
	if fpr > 0.001 {
		t.Errorf("false positive rate %.4f exceeds 0.1%% threshold", fpr)
	}
	t.Logf("false positive rate: %.4f (%d/%d)", fpr, falsePositives, total)
}

// --- Serialization / cross-language compatibility ---------------------------

func TestFromBytesRoundTrip(t *testing.T) {
	keys := [][]byte{
		make([]byte, 32),                              // all-zero 32-byte key (Solana pubkey style)
		{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
			17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
		[]byte("hello"),
		[]byte("world"),
		[]byte("cuckoo filter"),
	}

	cf := New(1024)
	for _, k := range keys {
		if !cf.Insert(k) {
			t.Fatalf("Insert(%x) failed", k)
		}
	}

	data := cf.Bytes()

	cf2, err := FromBytes(data, cf.Seed(), cf.Algorithm())
	if err != nil {
		t.Fatalf("FromBytes: %v", err)
	}

	for _, k := range keys {
		if !cf2.Lookup(k) {
			t.Errorf("Lookup(%x) = false after round-trip, want true", k)
		}
	}

	if cf2.m != cf.m {
		t.Errorf("bucket count after round-trip: got %d, want %d", cf2.m, cf.m)
	}
}

func TestFromBytesUnaligned(t *testing.T) {
	_, err := FromBytes([]byte{1, 2, 3}, DefaultSeed, SipHash)
	if err == nil {
		t.Error("FromBytes with unaligned data: want error, got nil")
	}
}

// TestCrossCompatVectors verifies that known 32-byte keys produce known
// (fingerprint, primary-index, alt-index) triples with DefaultSeed and
// capacity=256 (→ 128 buckets).
//
// These constants were produced by this Go implementation. The companion
// Rust test in compat_rust_test.rs verifies that Rust's SipHash-2-4 path
// yields identical values, confirming cross-language hash compatibility.
func TestCrossCompatVectors(t *testing.T) {
	zeros32 := make([]byte, 32)

	ones32 := make([]byte, 32)
	for i := range ones32 {
		ones32[i] = 1
	}

	ff32 := make([]byte, 32)
	for i := range ff32 {
		ff32[i] = 0xff
	}

	// capacity=256 → numBuckets=128 (matches Rust with_capacity(256))
	cf := NewWithSeed(256, DefaultSeed)

	vectors := []struct {
		name   string
		key    []byte
		wantFP uint16
		wantI1 uint
		wantI2 uint
	}{
		{"zeros32", zeros32, 0xb402, 15, 35},
		{"ones32", ones32, 0xc55a, 17, 113},
		{"ff32", ff32, 0x94b3, 36, 42},
	}

	for _, v := range vectors {
		i1, fp := cf.hashOf(v.key)
		i2 := cf.altIndex(i1, fp)

		if fp != v.wantFP || i1 != v.wantI1 || i2 != v.wantI2 {
			t.Errorf("%s: got (fp=0x%04x i1=%d i2=%d), want (fp=0x%04x i1=%d i2=%d)",
				v.name, fp, i1, i2, v.wantFP, v.wantI1, v.wantI2)
		}
	}
}

// TestCrossCompatSingleInsert verifies the raw serialized bytes for a single
// key insertion. With one key there are no cuckoo kicks, so the layout is
// fully deterministic and byte-comparable across languages.
//
// zeros32 → fp=0xb402, i1=15. Bucket 15 starts at byte offset 120 (15*8).
// Slot 0 holds fp=0xb402 as little-endian u16 (0x02 0xb4); remaining
// 3 slots are empty (0x0000). All other buckets are zero.
func TestCrossCompatSingleInsert(t *testing.T) {
	cf := NewWithSeed(256, DefaultSeed)
	cf.Insert(make([]byte, 32)) // zeros32

	data := cf.Bytes()

	const bucketOffset = 15 * bucketSize * 2 // bucket 15, 8 bytes

	gotBucket := fmt.Sprintf("%x", data[bucketOffset:bucketOffset+8])
	wantBucket := "02b4000000000000"
	if gotBucket != wantBucket {
		t.Errorf("bucket 15 bytes: got %s, want %s", gotBucket, wantBucket)
	}

	for i, b := range data {
		if i >= bucketOffset && i < bucketOffset+8 {
			continue
		}
		if b != 0 {
			t.Errorf("expected zero at offset %d, got 0x%02x", i, b)
		}
	}
}

// --- Benchmarks -------------------------------------------------------------

func prefillFilter(n int) (*Filter, [][]byte) {
	keys := make([][]byte, n)
	for i := range n {
		keys[i] = fmt.Appendf(nil, "key-%d", i)
	}
	cf := New(uint(n))
	for _, k := range keys {
		cf.Insert(k)
	}
	return cf, keys
}

func BenchmarkInsert(b *testing.B) {
	keys := make([][]byte, b.N)
	for i := range b.N {
		keys[i] = fmt.Appendf(nil, "key-%d", i)
	}
	cf := New(uint(b.N))

	b.ResetTimer()
	for _, k := range keys {
		cf.Insert(k)
	}
}

func BenchmarkLookupHit(b *testing.B) {
	cf, keys := prefillFilter(b.N)

	b.ResetTimer()
	for i := range b.N {
		cf.Lookup(keys[i])
	}
}

func BenchmarkLookupMiss(b *testing.B) {
	cf, _ := prefillFilter(b.N)
	misses := make([][]byte, b.N)
	for i := range b.N {
		misses[i] = fmt.Appendf(nil, "miss-%d", i)
	}

	b.ResetTimer()
	for _, k := range misses {
		cf.Lookup(k)
	}
}

func BenchmarkDelete(b *testing.B) {
	cf, keys := prefillFilter(b.N)

	b.ResetTimer()
	for _, k := range keys {
		cf.Delete(k)
	}
}
