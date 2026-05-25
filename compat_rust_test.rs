// Cross-language compatibility test for the Go cuckoo filter.
//
// Copy this test into yellowstone-grpc-proto/src/cuckoo/ alongside filter.rs
// and run:
//
//   cargo test cross_compat
//
// All assertions must pass for the Go and Rust implementations to be
// considered wire-compatible. The expected constants are produced by the
// Go TestCrossCompatVectors and TestCrossCompatSingleInsert tests.

#[cfg(test)]
mod cross_compat {
    use siphasher::sip::SipHasher24;
    use std::hash::{Hash, Hasher};

    use crate::cuckoo::{
        constants::{DEFAULT_HASH_SEED, ENTRIES_PER_BUCKET},
        filter::CuckooFilter,
        hasher::YellowstoneHasherBuilder,
    };

    // Replicates Go's hashOf + altIndex for [u8; 32] keys.
    //
    // Go hashOf:
    //   buf  = len(key) as u64 LE  ++  key bytes
    //   hash = SipHash-2-4(k0, k1, buf)
    //   fp   = (hash >> 32) as u16, clamped to 1 if zero
    //   i1   = hash as usize & (m - 1)
    //
    // Rust's [u8; 32].hash() feeds len as u64 LE then each byte individually
    // (via write_usize + write_u8 per element), which is byte-equivalent to
    // Go's single write of the prepended buffer.
    //
    // Go altIndex:
    //   hash2 = SipHash-2-4(k0, k1, fp as u16 LE)
    //   i2    = i1 ^ (hash2 as usize & (m - 1))
    //
    // Rust's u16.hash() calls write_u16(fp) = write(fp.to_ne_bytes()),
    // which on LE platforms is identical to Go's LittleEndian.PutUint16.
    fn hash_key(key: &[u8; 32], m: usize) -> (u16, usize, usize) {
        let (k0, k1) = YellowstoneHasherBuilder::keys_from_seed(DEFAULT_HASH_SEED);

        // Primary hash — matches Go's length-prefixed buffer.
        let mut h = SipHasher24::new_with_keys(k0, k1);
        key.hash(&mut h);
        let hash = h.finish();

        let fp = {
            let f = (hash >> 32) as u16;
            if f == 0 { 1 } else { f }
        };
        let i1 = hash as usize & (m - 1);

        // Alt-index hash — matches Go's 2-byte LE fingerprint input.
        let mut h2 = SipHasher24::new_with_keys(k0, k1);
        fp.hash(&mut h2);
        let alt_hash = h2.finish();
        let i2 = i1 ^ (alt_hash as usize & (m - 1));

        (fp, i1, i2)
    }

    // capacity=256 → 128 buckets, matching Go's NewWithSeed(256, DefaultSeed).
    // Rust uses ceil(256 / (0.95 * 4)) = ceil(67.4) = 68 → next_power_of_two(68) = 128.
    const M: usize = 128;

    #[test]
    fn cross_compat_vectors() {
        let zeros32 = [0u8; 32];
        let ones32  = [1u8; 32];
        let ff32    = [0xffu8; 32];

        // Expected values produced by Go's TestCrossCompatVectors.
        assert_eq!(hash_key(&zeros32, M), (0xb402, 15,  35),  "zeros32 mismatch");
        assert_eq!(hash_key(&ones32,  M), (0xc55a, 17, 113),  "ones32 mismatch");
        assert_eq!(hash_key(&ff32,    M), (0x94b3, 36,  42),  "ff32 mismatch");
    }

    #[test]
    fn cross_compat_single_insert() {
        // Insert zeros32 into a capacity-256 filter and check the raw bytes.
        // zeros32 → fp=0xb402, i1=15.
        // Bucket 15 starts at byte offset 120 (15 * 4 slots * 2 bytes/slot).
        // Slot 0 = fp 0xb402 as LE u16 = [0x02, 0xb4]; slots 1-3 = [0x00, 0x00].
        // Expected by Go's TestCrossCompatSingleInsert.
        let mut filter = CuckooFilter::<[u8; 32]>::with_capacity(256).unwrap();
        filter.insert(&[0u8; 32]).unwrap();

        let proto = crate::geyser::CuckooFilter::from(&filter);
        let data = &proto.data;

        assert_eq!(data.len(), M * ENTRIES_PER_BUCKET * 2, "unexpected data length");

        let bucket_offset = 15 * ENTRIES_PER_BUCKET * 2; // byte 120
        let bucket_bytes  = &data[bucket_offset..bucket_offset + 8];
        assert_eq!(
            bucket_bytes,
            &[0x02, 0xb4, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00],
            "bucket 15 bytes mismatch"
        );

        // All other bytes must be zero.
        for (i, &b) in data.iter().enumerate() {
            if i >= bucket_offset && i < bucket_offset + 8 {
                continue;
            }
            assert_eq!(b, 0, "unexpected non-zero byte at offset {}", i);
        }
    }

    #[test]
    fn go_bytes_deserialized_and_looked_up() {
        // Bytes produced by Go's TestCrossCompatSingleInsert.
        // 128 buckets × 4 slots × 2 bytes = 1024 bytes, all zero except bucket 15 slot 0.
        let mut data = vec![0u8; M * ENTRIES_PER_BUCKET * 2];
        let bucket_offset = 15 * ENTRIES_PER_BUCKET * 2;
        data[bucket_offset]     = 0x02; // fp=0xb402 LE
        data[bucket_offset + 1] = 0xb4;

        let proto = crate::geyser::CuckooFilter {
            data,
            bucket_count:      M as u32,
            entries_per_bucket: ENTRIES_PER_BUCKET as u32,
            fingerprint_bits:  16,
            hash_seed:         DEFAULT_HASH_SEED,
            hash_algorithm:    crate::geyser::CuckooHashAlgorithm::SipHash as i32,
        };

        let filter = CuckooFilter::<[u8; 32]>::from(&proto);
        assert!(filter.contains(&[0u8; 32]), "zeros32 not found after deserializing Go bytes");
        assert!(!filter.contains(&[1u8; 32]), "ones32 found but was never inserted");
    }
}
