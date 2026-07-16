package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
)

var (
	ErrInvalidExactInteger  = errors.New("invalid canonical exact integer")
	ErrInvalidExactRational = errors.New("invalid exact rational")
	ErrRevisionOverflow     = errors.New("revision exceeds the positive SQLite integer range")
	ErrInvalidRationalTime  = errors.New("invalid rational time")
	ErrTimeOverflow         = errors.New("rational time result is outside the supported range")
)

// ExactRational is a reduced, dimensionless rational number. It deliberately
// does not reuse RationalTime: geometry and gain values must not acquire time
// semantics merely because their canonical wire shape is identical.
type ExactRational struct {
	Value Int64 `json:"value" format:"int64-decimal" pattern:"^(0|-[1-9][0-9]*|[1-9][0-9]*)$"`
	Scale int32 `json:"scale" minimum:"1"`
}

func NewExactRational(value int64, scale int32) (ExactRational, error) {
	if scale <= 0 {
		return ExactRational{}, ErrInvalidExactRational
	}
	if value == 0 {
		return ExactRational{Value: 0, Scale: 1}, nil
	}
	divisor := gcd(absInt64(value), uint64(scale))
	return ExactRational{
		Value: Int64(value / int64(divisor)),
		Scale: scale / int32(divisor),
	}, nil
}

func (value ExactRational) Validate() error {
	if value.Scale <= 0 {
		return ErrInvalidExactRational
	}
	if value.Value == 0 {
		if value.Scale != 1 {
			return ErrInvalidExactRational
		}
		return nil
	}
	if gcd(absInt64(value.Value.Value()), uint64(value.Scale)) != 1 {
		return ErrInvalidExactRational
	}
	return nil
}

func (value ExactRational) IsNegative() bool { return value.Value < 0 }

func (value ExactRational) IsPositive() bool { return value.Value > 0 }

func (value ExactRational) Compare(other ExactRational) (int, error) {
	if err := value.Validate(); err != nil {
		return 0, err
	}
	if err := other.Validate(); err != nil {
		return 0, err
	}
	left := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(int64(other.Scale)))
	right := new(big.Int).Mul(big.NewInt(other.Value.Value()), big.NewInt(int64(value.Scale)))
	return left.Cmp(right), nil
}

func (value ExactRational) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	type wire ExactRational
	return json.Marshal(wire(value))
}

func (value *ExactRational) UnmarshalJSON(data []byte) error {
	type wire ExactRational
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidExactRational
	}
	next := ExactRational(decoded)
	if err := next.Validate(); err != nil {
		return err
	}
	*value = next
	return nil
}

// Int64 is an exact signed 64-bit value encoded as a canonical decimal string
// on every JSON wire.
type Int64 int64

func NewInt64(value int64) Int64 {
	return Int64(value)
}

func (value Int64) Value() int64 {
	return int64(value)
}

func (value Int64) String() string {
	return strconv.FormatInt(int64(value), 10)
}

func (value Int64) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *Int64) UnmarshalJSON(data []byte) error {
	text, err := decodeJSONString(data)
	if err != nil {
		return fmt.Errorf("decode int64 decimal string: %w", err)
	}
	return value.UnmarshalText([]byte(text))
}

func (value Int64) MarshalText() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *Int64) UnmarshalText(text []byte) error {
	parsed, err := parseCanonicalInt64(string(text))
	if err != nil {
		return err
	}
	*value = Int64(parsed)
	return nil
}

// UInt64 is an exact non-negative value capped at MaxInt64 so it remains
// losslessly persistable as a SQLite INTEGER. JSON uses a canonical decimal
// string, matching other exact product scalars.
type UInt64 uint64

func NewUInt64(value uint64) (UInt64, error) {
	if value > math.MaxInt64 {
		return 0, ErrRevisionOverflow
	}
	return UInt64(value), nil
}

func (value UInt64) Value() uint64 {
	return uint64(value)
}

func (value UInt64) String() string {
	return strconv.FormatUint(uint64(value), 10)
}

func (value UInt64) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *UInt64) UnmarshalJSON(data []byte) error {
	text, err := decodeJSONString(data)
	if err != nil {
		return fmt.Errorf("decode uint64 decimal string: %w", err)
	}
	return value.UnmarshalText([]byte(text))
}

func (value UInt64) MarshalText() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *UInt64) UnmarshalText(text []byte) error {
	parsed, err := parseCanonicalUint64(string(text))
	if err != nil {
		return err
	}
	next, err := NewUInt64(parsed)
	if err != nil {
		return err
	}
	*value = next
	return nil
}

// Revision is semantically unsigned but deliberately capped at MaxInt64 so it
// remains an indexed SQLite INTEGER. JSON always carries a decimal string.
type Revision uint64

func NewRevision(value uint64) (Revision, error) {
	if value > math.MaxInt64 {
		return 0, ErrRevisionOverflow
	}
	return Revision(value), nil
}

func (value Revision) Value() uint64 {
	return uint64(value)
}

func (value Revision) String() string {
	return strconv.FormatUint(uint64(value), 10)
}

func (value Revision) Next() (Revision, error) {
	return NewRevision(uint64(value) + 1)
}

func (value Revision) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *Revision) UnmarshalJSON(data []byte) error {
	text, err := decodeJSONString(data)
	if err != nil {
		return fmt.Errorf("decode revision decimal string: %w", err)
	}
	return value.UnmarshalText([]byte(text))
}

func (value Revision) MarshalText() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *Revision) UnmarshalText(text []byte) error {
	parsed, err := parseCanonicalUint64(string(text))
	if err != nil {
		return err
	}
	next, err := NewRevision(parsed)
	if err != nil {
		return err
	}
	*value = next
	return nil
}

// Cursor has the same wire and persistence range as Revision but is a distinct
// scoped ordering type.
type Cursor uint64

func NewCursor(value uint64) (Cursor, error) {
	if value > math.MaxInt64 {
		return 0, ErrRevisionOverflow
	}
	return Cursor(value), nil
}

func (value Cursor) Value() uint64 {
	return uint64(value)
}

func (value Cursor) String() string {
	return strconv.FormatUint(uint64(value), 10)
}

func (value Cursor) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *Cursor) UnmarshalJSON(data []byte) error {
	text, err := decodeJSONString(data)
	if err != nil {
		return fmt.Errorf("decode cursor decimal string: %w", err)
	}
	parsed, err := parseCanonicalUint64(text)
	if err != nil {
		return err
	}
	next, err := NewCursor(parsed)
	if err != nil {
		return err
	}
	*value = next
	return nil
}

func (value Cursor) MarshalText() ([]byte, error) {
	return []byte(value.String()), nil
}

func (value *Cursor) UnmarshalText(text []byte) error {
	parsed, err := parseCanonicalUint64(string(text))
	if err != nil {
		return err
	}
	next, err := NewCursor(parsed)
	if err != nil {
		return err
	}
	*value = next
	return nil
}

// RationalTime represents exact seconds as Value / Scale. Values are always in
// reduced form and Scale is always positive.
type RationalTime struct {
	Value Int64 `json:"value" format:"int64-decimal" pattern:"^(0|-[1-9][0-9]*|[1-9][0-9]*)$"`
	Scale int32 `json:"scale" minimum:"1"`
}

func NewRationalTime(value int64, scale int32) (RationalTime, error) {
	if scale <= 0 {
		return RationalTime{}, ErrInvalidRationalTime
	}
	if value == 0 {
		return RationalTime{Value: 0, Scale: 1}, nil
	}
	divisor := gcd(absInt64(value), uint64(scale))
	return RationalTime{
		Value: Int64(value / int64(divisor)),
		Scale: scale / int32(divisor),
	}, nil
}

func (value RationalTime) Validate() error {
	if value.Scale <= 0 {
		return ErrInvalidRationalTime
	}
	if value.Value == 0 {
		if value.Scale != 1 {
			return ErrInvalidRationalTime
		}
		return nil
	}
	if gcd(absInt64(value.Value.Value()), uint64(value.Scale)) != 1 {
		return ErrInvalidRationalTime
	}
	return nil
}

func (value RationalTime) IsNegative() bool {
	return value.Value < 0
}

func (value RationalTime) IsPositive() bool {
	return value.Value > 0
}

func (value RationalTime) Compare(other RationalTime) (int, error) {
	if err := value.Validate(); err != nil {
		return 0, err
	}
	if err := other.Validate(); err != nil {
		return 0, err
	}
	left := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(int64(other.Scale)))
	right := new(big.Int).Mul(big.NewInt(other.Value.Value()), big.NewInt(int64(value.Scale)))
	return left.Cmp(right), nil
}

func (value RationalTime) Add(other RationalTime) (RationalTime, error) {
	if err := value.Validate(); err != nil {
		return RationalTime{}, err
	}
	if err := other.Validate(); err != nil {
		return RationalTime{}, err
	}
	left := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(int64(other.Scale)))
	right := new(big.Int).Mul(big.NewInt(other.Value.Value()), big.NewInt(int64(value.Scale)))
	numerator := new(big.Int).Add(left, right)
	denominator := new(big.Int).Mul(big.NewInt(int64(value.Scale)), big.NewInt(int64(other.Scale)))
	return rationalFromBig(numerator, denominator)
}

func (value RationalTime) Subtract(other RationalTime) (RationalTime, error) {
	if err := value.Validate(); err != nil {
		return RationalTime{}, err
	}
	if err := other.Validate(); err != nil {
		return RationalTime{}, err
	}
	left := new(big.Int).Mul(big.NewInt(value.Value.Value()), big.NewInt(int64(other.Scale)))
	right := new(big.Int).Mul(big.NewInt(other.Value.Value()), big.NewInt(int64(value.Scale)))
	numerator := new(big.Int).Sub(left, right)
	denominator := new(big.Int).Mul(big.NewInt(int64(value.Scale)), big.NewInt(int64(other.Scale)))
	return rationalFromBig(numerator, denominator)
}

func (value RationalTime) MarshalJSON() ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	type wire RationalTime
	return json.Marshal(wire(value))
}

func (value *RationalTime) UnmarshalJSON(data []byte) error {
	type wire RationalTime
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ErrInvalidRationalTime
	}
	next := RationalTime(decoded)
	if err := next.Validate(); err != nil {
		return err
	}
	*value = next
	return nil
}

type TimeRange struct {
	Start    RationalTime `json:"start"`
	Duration RationalTime `json:"duration"`
}

func NewTimeRange(start, duration RationalTime) (TimeRange, error) {
	if err := start.Validate(); err != nil {
		return TimeRange{}, err
	}
	if err := duration.Validate(); err != nil || duration.IsNegative() {
		return TimeRange{}, ErrInvalidRationalTime
	}
	return TimeRange{Start: start, Duration: duration}, nil
}

func (value TimeRange) End() (RationalTime, error) {
	return value.Start.Add(value.Duration)
}

func rationalFromBig(numerator, denominator *big.Int) (RationalTime, error) {
	if denominator.Sign() <= 0 {
		return RationalTime{}, ErrInvalidRationalTime
	}
	if numerator.Sign() == 0 {
		return RationalTime{Value: 0, Scale: 1}, nil
	}
	absNumerator := new(big.Int).Abs(new(big.Int).Set(numerator))
	divisor := new(big.Int).GCD(nil, nil, absNumerator, denominator)
	numerator = new(big.Int).Quo(numerator, divisor)
	denominator = new(big.Int).Quo(denominator, divisor)
	if !numerator.IsInt64() || !denominator.IsInt64() || denominator.Int64() > math.MaxInt32 {
		return RationalTime{}, ErrTimeOverflow
	}
	return NewRationalTime(numerator.Int64(), int32(denominator.Int64()))
}

func parseCanonicalInt64(value string) (int64, error) {
	if !isCanonicalDecimal(value, true) {
		return 0, ErrInvalidExactInteger
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidExactInteger
	}
	return parsed, nil
}

func parseCanonicalUint64(value string) (uint64, error) {
	if !isCanonicalDecimal(value, false) {
		return 0, ErrInvalidExactInteger
	}
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, ErrInvalidExactInteger
	}
	return parsed, nil
}

func isCanonicalDecimal(value string, signed bool) bool {
	if value == "0" {
		return true
	}
	if value == "" {
		return false
	}
	start := 0
	if value[0] == '-' {
		if !signed || len(value) == 1 || value[1] == '0' {
			return false
		}
		start = 1
	} else if value[0] == '0' {
		return false
	}
	for index := start; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}

func decodeJSONString(data []byte) (string, error) {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	return value, nil
}

func absInt64(value int64) uint64 {
	if value >= 0 {
		return uint64(value)
	}
	return uint64(-(value + 1)) + 1
}

func gcd(left, right uint64) uint64 {
	for right != 0 {
		left, right = right, left%right
	}
	return left
}
