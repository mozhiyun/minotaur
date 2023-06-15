package maths

import (
	"github.com/kercylan98/minotaur/utils/generic"
)

// Pow 整数幂运算
func Pow(a, n int) int {
	if a == 0 {
		return 0
	}
	if n == 0 {
		return 1
	}
	if n == 1 {
		return a
	}
	var result int = 1
	factor := a
	for n != 0 {
		if n&1 != 0 {
			// 当前位是 1，需要乘进去
			result *= factor
		}
		factor *= factor
		n = n >> 1
	}
	return result
}

// PowInt64 整数幂运算
func PowInt64(a, n int64) int64 {
	if a == 0 {
		return 0
	}
	if n == 0 {
		return 1
	}
	if n == 1 {
		return a
	}
	var result int64 = 1
	factor := a
	for n != 0 {
		if n&1 != 0 {
			// 当前位是 1，需要乘进去
			result *= factor
		}
		factor *= factor
		n = n >> 1
	}
	return result
}

// Min 返回两个数之中较小的值
func Min[V generic.Number](a, b V) V {
	if a < b {
		return a
	}
	return b
}

// Max 返回两个数之中较大的值
func Max[V generic.Number](a, b V) V {
	if a > b {
		return a
	}
	return b
}

// MinMax 将两个数按照较小的和较大的顺序进行返回
func MinMax[V generic.Number](a, b V) (min, max V) {
	if a < b {
		return a, b
	}
	return b, a
}

// MaxMin 将两个数按照较大的和较小的顺序进行返回
func MaxMin[V generic.Number](a, b V) (max, min V) {
	if a > b {
		return a, b
	}
	return b, a
}
