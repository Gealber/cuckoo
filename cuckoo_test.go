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
