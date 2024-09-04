// Copyright 2020 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package ethash

import (
	"math/big"
	"github.com/Altcoinchain/go-altcoinchain/core/types"
	"github.com/Altcoinchain/go-altcoinchain/params"
	"github.com/holiman/uint256"
)

const (
	// frontierDurationLimit is for Frontier:
	// The decision boundary on the blocktime duration used to determine
	// whether difficulty should go up or down.
	frontierDurationLimit = 13
	// minimumDifficulty The minimum that the difficulty may ever be.
	minimumDifficulty = 131072
	// expDiffPeriod is the exponential difficulty period
	expDiffPeriodUint = 100000
	// difficultyBoundDivisorBitShift is the bound divisor of the difficulty (2048),
	// This constant is the right-shifts to use for the division.
	difficultyBoundDivisor = 11
)

// Define new constants or variables for PoS, PoT, and PoTrust mechanisms
var (
	stakeWeight       = big.NewInt(50)  // Weight of PoS in difficulty calculation
	transactionWeight = big.NewInt(30)  // Weight of PoT in difficulty calculation
	trustWeight       = big.NewInt(20)  // Weight of PoTrust in difficulty calculation
	powWeight         = big.NewInt(100) // Weight of PoW in difficulty calculation (may vary)
	totalWeight       = big.NewInt(200) // Sum of all weights (PoW + PoS + PoT + PoTrust)
)

// CalcCustomDifficulty calculates the new difficulty by combining PoW, PoS, PoT, and PoTrust
func CalcCustomDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header, posFactor, potFactor, trustFactor *big.Int) *big.Int {
	// PoW difficulty calculation
	powDifficulty := CalcDifficulty(chain.Config(), time, parent)
	
	// Calculate PoS influence on difficulty
	posDifficulty := new(big.Int).Mul(powDifficulty, stakeWeight)
	posDifficulty.Mul(posDifficulty, posFactor)
	posDifficulty.Div(posDifficulty, totalWeight)
	
	// Calculate PoT influence on difficulty
	potDifficulty := new(big.Int).Mul(powDifficulty, transactionWeight)
	potDifficulty.Mul(potDifficulty, potFactor)
	potDifficulty.Div(potDifficulty, totalWeight)
	
	// Calculate Proof of Trust (PoTrust) influence on difficulty
	trustDifficulty := new(big.Int).Mul(powDifficulty, trustWeight)
	trustDifficulty.Mul(trustDifficulty, trustFactor)
	trustDifficulty.Div(trustDifficulty, totalWeight)
	
	// Combine PoW, PoS, PoT, and PoTrust to get the final difficulty
	finalDifficulty := new(big.Int).Add(powDifficulty, posDifficulty)
	finalDifficulty.Add(finalDifficulty, potDifficulty)
	finalDifficulty.Add(finalDifficulty, trustDifficulty)
	
	// Ensure the difficulty does not go below the minimum
	if finalDifficulty.Cmp(big.NewInt(minimumDifficulty)) < 0 {
		finalDifficulty.SetUint64(minimumDifficulty)
	}
	
	return finalDifficulty
}

// CalcDifficultyFrontierU256 is the difficulty adjustment algorithm. It returns the
// difficulty that a new block should have when created at time given the parent
// block's time and difficulty. The calculation uses the Frontier rules.
func CalcDifficultyFrontierU256(time uint64, parent *types.Header) *big.Int {
	pDiff, _ := uint256.FromBig(parent.Difficulty) // pDiff: pdiff
	adjust := pDiff.Clone()
	adjust.Rsh(adjust, difficultyBoundDivisor) // adjust: pDiff / 2048

	if time-parent.Time < frontierDurationLimit {
		pDiff.Add(pDiff, adjust)
	} else {
		pDiff.Sub(pDiff, adjust)
	}
	if pDiff.LtUint64(minimumDifficulty) {
		pDiff.SetUint64(minimumDifficulty)
	}

	if periodCount := (parent.Number.Uint64() + 1) / expDiffPeriodUint; periodCount > 1 {
		expDiff := adjust.SetOne()
		expDiff.Lsh(expDiff, uint(periodCount-2)) // expdiff: 2 ^ (periodCount -2)
		pDiff.Add(pDiff, expDiff)
	}
	return pDiff.ToBig()
}

// CalcDifficultyHomesteadU256 is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time given the
// parent block's time and difficulty. The calculation uses the Homestead rules.
func CalcDifficultyHomesteadU256(time uint64, parent *types.Header) *big.Int {
	pDiff, _ := uint256.FromBig(parent.Difficulty) // pDiff: pdiff
	adjust := pDiff.Clone()
	adjust.Rsh(adjust, difficultyBoundDivisor) // adjust: pDiff / 2048

	x := (time - parent.Time) / 10 // (time - ptime) / 10)
	var neg = true
	if x == 0 {
		x = 1
		neg = false
	} else if x >= 100 {
		x = 99
	} else {
		x = x - 1
	}
	z := new(uint256.Int).SetUint64(x)
	adjust.Mul(adjust, z) // adjust: (pdiff / 2048) * max((time - ptime) / 10 - 1, 99)
	if neg {
		pDiff.Sub(pDiff, adjust) // pdiff - pdiff / 2048 * max((time - ptime) / 10 - 1, 99)
	} else {
		pDiff.Add(pDiff, adjust) // pdiff + pdiff / 2048 * max((time - ptime) / 10 - 1, 99)
	}
	if pDiff.LtUint64(minimumDifficulty) {
		pDiff.SetUint64(minimumDifficulty)
	}

	if periodCount := (1 + parent.Number.Uint64()) / expDiffPeriodUint; periodCount > 1 {
		expFactor := adjust.Lsh(adjust.SetOne(), uint(periodCount-2))
		pDiff.Add(pDiff, expFactor)
	}
	return pDiff.ToBig()
}

// MakeDifficultyCalculatorU256 creates a difficultyCalculator with the given bomb-delay.
// the difficulty is calculated with Byzantium rules, which differs from Homestead in
// how uncles affect the calculation
func MakeDifficultyCalculatorU256(bombDelay *big.Int) func(time uint64, parent *types.Header) *big.Int {
	bombDelayFromParent := bombDelay.Uint64() - 1
	return func(time uint64, parent *types.Header) *big.Int {
		x := (time - parent.Time) / 9 // (block_timestamp - parent_timestamp) // 9
		c := uint64(1)                // if parent.unclehash == emptyUncleHashHash
		if parent.UncleHash != types.EmptyUncleHash {
			c = 2
		}
		xNeg := x >= c
		if xNeg {
			x = x - c // - ( (t-p)/p -( 2 or 1) )
		} else {
			x = c - x // (2 or 1) - (t-p)/9
		}
		if x > 99 {
			x = 99 // max(x, 99)
		}
		y := new(uint256.Int)
		y.SetFromBig(parent.Difficulty)    // y: p_diff
		pDiff := y.Clone()                 // pdiff: p_diff
		z := new(uint256.Int).SetUint64(x) //z : +-adj_factor (either pos or negative)
		y.Rsh(y, difficultyBoundDivisor)   // y: p__diff / 2048
		z.Mul(y, z)                        // z: (p_diff / 2048 ) * (+- adj_factor)

		if xNeg {
			y.Sub(pDiff, z) // y: parent_diff + parent_diff/2048 * adjustment_factor
		} else {
			y.Add(pDiff, z) // y: parent_diff + parent_diff/2048 * adjustment_factor
		}

		if y.LtUint64(minimumDifficulty) {
			y.SetUint64(minimumDifficulty)
		}

		var pNum = parent.Number.Uint64()
		if pNum >= bombDelayFromParent {
			if fakeBlockNumber := pNum - bombDelayFromParent; fakeBlockNumber >= 2*expDiffPeriodUint {
				z.SetOne()
				z.Lsh(z, uint(fakeBlockNumber/expDiffPeriodUint-2))
				y.Add(z, y)
			}
		}
		return y.ToBig()
	}
}
