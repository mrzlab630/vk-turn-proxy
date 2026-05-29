package main

import (
	crand "crypto/rand"
	"encoding/binary"
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

	var fallback [8]byte
	if _, readErr := crand.Read(fallback[:]); readErr == nil {
		return int(binary.BigEndian.Uint64(fallback[:]) % uint64(max))
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
