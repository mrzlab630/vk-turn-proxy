package main

import (
	crand "crypto/rand"
	"math/big"
)

func secureIntn(max int) int {
	if max <= 0 {
		return 0
	}
	value, err := crand.Int(crand.Reader, big.NewInt(int64(max)))
	if err == nil {
		return int(value.Int64())
	}
	return 0
}

func secureChance(numerator, denominator int) bool {
	if denominator <= 0 || numerator <= 0 {
		return false
	}
	if numerator >= denominator {
		return true
	}
	return secureIntn(denominator) < numerator
}
