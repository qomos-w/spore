package bytecode

import (
	"math"

	"github.com/qomos-w/spore/internal/script/vm"
)

// --- Value helpers ---

func toFloat(v vm.Value) float32 {
	if vm.IsFloat(v) {
		return vm.DecodeFloat(v)
	}
	return float32(vm.DecodeInt(v))
}

// arithBinOp performs a binary arithmetic operation, dispatching to the
// correct domain (double > long > ulong > float > int) based on operand types.
func arithBinOp(v *vm.VM, a, b vm.Value,
	doubleFn func(float64, float64) float64,
	longFn func(int64, int64) int64,
	ulongFn func(uint64, uint64) uint64,
) vm.Value {
	// double wins over everything
	if vm.IsDouble(a) || vm.IsDouble(b) {
		aVal := toNumericFloat64(v, a)
		bVal := toNumericFloat64(v, b)
		return vm.EncodeDouble(doubleFn(aVal, bVal), v)
	}
	// long wins over ulong/int
	if vm.IsLong(a) || vm.IsLong(b) {
		aVal := toNumericInt64(v, a)
		bVal := toNumericInt64(v, b)
		return vm.EncodeLong(longFn(aVal, bVal), v)
	}
	// ulong wins over int
	if vm.IsULong(a) || vm.IsULong(b) {
		aVal := toNumericUInt64(v, a)
		bVal := toNumericUInt64(v, b)
		return vm.EncodeULong(ulongFn(aVal, bVal), v)
	}
	// float wins over int
	if vm.IsFloat(a) || vm.IsFloat(b) {
		aVal := toFloat(a)
		bVal := toFloat(b)
		return vm.EncodeFloat(float32(doubleFn(float64(aVal), float64(bVal))))
	}
	// int + int
	return vm.EncodeInt(int32(longFn(int64(vm.DecodeInt(a)), int64(vm.DecodeInt(b)))))
}

// toNumericFloat64 converts any numeric value to float64.
func toNumericFloat64(v *vm.VM, val vm.Value) float64 {
	if vm.IsDouble(val) {
		return v.DecodeDouble(val)
	}
	if vm.IsLong(val) {
		return float64(v.DecodeLong(val))
	}
	if vm.IsULong(val) {
		return float64(v.DecodeULong(val))
	}
	if vm.IsFloat(val) {
		return float64(vm.DecodeFloat(val))
	}
	return float64(vm.DecodeInt(val))
}

// toNumericInt64 converts any numeric value to int64.
func toNumericInt64(v *vm.VM, val vm.Value) int64 {
	if vm.IsLong(val) {
		return v.DecodeLong(val)
	}
	if vm.IsULong(val) {
		return int64(v.DecodeULong(val))
	}
	if vm.IsDouble(val) {
		return int64(v.DecodeDouble(val))
	}
	if vm.IsFloat(val) {
		return int64(vm.DecodeFloat(val))
	}
	return int64(vm.DecodeInt(val))
}

// toNumericUInt64 converts any numeric value to uint64.
func toNumericUInt64(v *vm.VM, val vm.Value) uint64 {
	if vm.IsULong(val) {
		return v.DecodeULong(val)
	}
	if vm.IsLong(val) {
		return uint64(v.DecodeLong(val))
	}
	if vm.IsDouble(val) {
		return uint64(v.DecodeDouble(val))
	}
	if vm.IsFloat(val) {
		return uint64(vm.DecodeFloat(val))
	}
	return uint64(vm.DecodeInt(val))
}

// isZero checks if a numeric value is zero (for div/mod safety).
func isZero(v *vm.VM, val vm.Value) bool {
	if vm.IsDouble(val) {
		return v.DecodeDouble(val) == 0
	}
	if vm.IsLong(val) {
		return v.DecodeLong(val) == 0
	}
	if vm.IsULong(val) {
		return v.DecodeULong(val) == 0
	}
	if vm.IsFloat(val) {
		return vm.DecodeFloat(val) == 0
	}
	return vm.DecodeInt(val) == 0
}

// compareValues compares two numeric values, returning -1, 0, or 1.
func compareValues(v *vm.VM, a, b vm.Value) int {
	// double wins
	if vm.IsDouble(a) || vm.IsDouble(b) {
		aVal := toNumericFloat64(v, a)
		bVal := toNumericFloat64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// long
	if vm.IsLong(a) || vm.IsLong(b) {
		aVal := toNumericInt64(v, a)
		bVal := toNumericInt64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// ulong
	if vm.IsULong(a) || vm.IsULong(b) {
		aVal := toNumericUInt64(v, a)
		bVal := toNumericUInt64(v, b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// float
	if vm.IsFloat(a) || vm.IsFloat(b) {
		aVal := toFloat(a)
		bVal := toFloat(b)
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
		return 0
	}
	// int
	aVal := vm.DecodeInt(a)
	bVal := vm.DecodeInt(b)
	if aVal < bVal {
		return -1
	}
	if aVal > bVal {
		return 1
	}
	return 0
}

func valuesEqual(v *vm.VM, a, b vm.Value) bool {
	if vm.IsNull(a) && vm.IsNull(b) {
		return true
	}
	if v.IsStringValue(a) || v.IsStringValue(b) {
		return v.DecodeString(a) == v.DecodeString(b)
	}
	// Check wide numeric types first
	if vm.IsDouble(a) || vm.IsDouble(b) {
		return toNumericFloat64(v, a) == toNumericFloat64(v, b)
	}
	if vm.IsLong(a) || vm.IsLong(b) {
		return toNumericInt64(v, a) == toNumericInt64(v, b)
	}
	if vm.IsULong(a) || vm.IsULong(b) {
		return toNumericUInt64(v, a) == toNumericUInt64(v, b)
	}
	if vm.IsFloat(a) || vm.IsFloat(b) {
		return toFloat(a) == toFloat(b)
	}
	if vm.IsBool(a) && vm.IsBool(b) {
		return vm.DecodeBool(a) == vm.DecodeBool(b)
	}
	// Enum values: same-tag raw equality (enum ID + member value). An enum
	// value never equals a plain int — the tag must match too.
	return a == b
}

// int32FromIntegral converts an int- or long-tagged value to int32, reporting
// whether the value fits.
func int32FromIntegral(v *vm.VM, val vm.Value) (int32, bool) {
	if vm.IsInt(val) {
		return vm.DecodeInt(val), true
	}
	if vm.IsLong(val) {
		i := v.DecodeLong(val)
		if i < math.MinInt32 || i > math.MaxInt32 {
			return 0, false
		}
		return int32(i), true
	}
	return 0, false
}
