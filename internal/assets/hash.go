package assets

import (
	"encoding/binary"
	"image"
	"math/bits"

	"github.com/ajdnik/imghash/v2"
	"github.com/ajdnik/imghash/v2/hashtype"
)

var (
	phasher, _ = imghash.NewPHash()
	pdqer, _   = imghash.NewPDQ()
)

// Perceptual returns the 64-bit pHash (a cheap bucket key) and the 256-bit PDQ hash (the
// near-duplicate detector: Hamming distance ≤ 31 of 256 is the same picture) of img.
func Perceptual(img image.Image) (phash int64, pdq []byte, err error) {
	small := shrink(img, 512)
	ph, err := phasher.Calculate(small)
	if err != nil {
		return 0, nil, err
	}
	pq, err := pdqer.Calculate(small)
	if err != nil {
		return 0, nil, err
	}
	phash = int64(binary.BigEndian.Uint64(pad8(ph.(hashtype.Binary))))
	pdq = []byte(pq.(hashtype.Binary))
	return phash, pdq, nil
}

func pad8(b []byte) []byte {
	if len(b) >= 8 {
		return b[:8]
	}
	out := make([]byte, 8)
	copy(out, b)
	return out
}

// Hamming is the bit distance between two equal-length hashes.
func Hamming(a, b []byte) int {
	n := 0
	for i := 0; i < len(a) && i < len(b); i++ {
		n += bits.OnesCount8(a[i] ^ b[i])
	}
	return n
}

// PDQSame reports whether two PDQ hashes are the same picture (Meta's reference threshold).
func PDQSame(a, b []byte) bool { return len(a) == 32 && len(b) == 32 && Hamming(a, b) <= 31 }
