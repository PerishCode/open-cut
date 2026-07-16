package domain

import (
	"encoding/hex"
	"math/big"
)

// RationalOrderKey maps every supported RationalTime into a fixed-width,
// lexicographically sortable key. 128 fractional bits exceed the precision
// needed to distinguish any two int64/int32 reduced rationals.
func RationalOrderKey(value RationalTime) (string, error) {
	if err := value.Validate(); err != nil {
		return "", err
	}
	numerator := big.NewInt(value.Value.Value())
	scaled := new(big.Int).Lsh(numerator, 128)
	scaled.Quo(scaled, big.NewInt(int64(value.Scale)))
	offset := new(big.Int).Lsh(big.NewInt(1), 191)
	scaled.Add(scaled, offset)
	if scaled.Sign() < 0 || scaled.BitLen() > 192 {
		return "", ErrTimeOverflow
	}
	bytes := scaled.Bytes()
	fixed := make([]byte, 24)
	copy(fixed[len(fixed)-len(bytes):], bytes)
	return hex.EncodeToString(fixed), nil
}
