package ethash_test

import (
	"math/big"
	"testing"

	"https://github.com/Altcoinchain/go-altcoinchain/core/types"
	"https://github.com/Altcoinchain/go-altcoinchain/consensus/ethash"
)

func TestCalcCustomDifficulty(t *testing.T) {
	parent := &types.Header{
		Difficulty: big.NewInt(1000),
		Time:       1000,
		Number:     big.NewInt(1),
	}

	posFactor := big.NewInt(50)
	potFactor := big.NewInt(30)
	trustFactor := big.NewInt(20)

	expectedDifficulty := big.NewInt(1600) // Adjust this based on your expectations
	result := ethash.CalcCustomDifficulty(nil, 2000, parent, posFactor, potFactor, trustFactor)

	if result.Cmp(expectedDifficulty) != 0 {
		t.Fatalf("Expected difficulty %v, but got %v", expectedDifficulty, result)
	}
}
