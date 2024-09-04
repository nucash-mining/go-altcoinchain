// Copyright 2017 The go-ethereum Authors
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
    "bytes"
    "errors"
    "fmt"
    "math/big"
    "runtime"
    "time"

    mapset "github.com/deckarep/golang-set"
    "github.com/Altcoinchain/go-altcoinchain/common"
    "github.com/Altcoinchain/go-altcoinchain/go-ethereum/consensus"
    "github.com/Altcoinchain/go-altcoinchain/consensus/misc"
    "github.com/Altcoinchain/go-altcoinchain/core/state"
    "github.com/Altcoinchain/go-altcoinchain/core/types"
    "github.com/Altcoinchain/go-altcoinchain/params"
    "github.com/Altcoinchain/go-altcoinchain/rlp"
    "github.com/Altcoinchain/go-altcoinchain/trie"
    "golang.org/x/crypto/sha3"
)

// Ethash proof-of-work protocol constants.
var (
    FrontierBlockReward           = big.NewInt(5e+18) // Block reward in wei for successfully mining a block
    ByzantiumBlockReward          = big.NewInt(3e+18) // Block reward in wei for successfully mining a block upward from Byzantium
    ConstantinopleBlockReward     = big.NewInt(2e+18) // Block reward in wei for successfully mining a block upward from Constantinople
    maxUncles                     = 2                 // Maximum number of uncles allowed in a single block
    allowedFutureBlockTimeSeconds = int64(15)         // Max seconds from current time allowed for blocks, before they're considered future blocks

    // calcDifficultyEip5133 is the difficulty adjustment algorithm as specified by EIP 5133.
    // It offsets the bomb a total of 11.4M blocks.
    // Specification EIP-5133: https://eips.ethereum.org/EIPS/eip-5133
    calcDifficultyEip5133 = makeDifficultyCalculator(big.NewInt(11_400_000))

    // calcDifficultyEip4345 is the difficulty adjustment algorithm as specified by EIP 4345.
    // It offsets the bomb a total of 10.7M blocks.
    // Specification EIP-4345: https://eips.ethereum.org/EIPS/eip-4345
    calcDifficultyEip4345 = makeDifficultyCalculator(big.NewInt(10_700_000))

    // calcDifficultyEip3554 is the difficulty adjustment algorithm as specified by EIP 3554.
    // It offsets the bomb a total of 9.7M blocks.
    // Specification EIP-3554: https://eips.ethereum.org/EIPS/eip-3554
    calcDifficultyEip3554 = makeDifficultyCalculator(big.NewInt(9700000))

    // calcDifficultyEip2384 is the difficulty adjustment algorithm as specified by EIP 2384.
    // It offsets the bomb 4M blocks from Constantinople, so in total 9M blocks.
    // Specification EIP-2384: https://eips.ethereum.org/EIPS/eip-2384
    calcDifficultyEip2384 = makeDifficultyCalculator(big.NewInt(9000000))

    // calcDifficultyConstantinople is the difficulty adjustment algorithm for Constantinople.
    // It returns the difficulty that a new block should have when created at time given the
    // parent block's time and difficulty. The calculation uses the Byzantium rules, but with
    // bomb offset 5M.
    // Specification EIP-1234: https://eips.ethereum.org/EIPS/eip-1234
    calcDifficultyConstantinople = makeDifficultyCalculator(big.NewInt(5000000))

    // calcDifficultyByzantium is the difficulty adjustment algorithm. It returns
    // the difficulty that a new block should have when created at time given the
    // parent block's time and difficulty. The calculation uses the Byzantium rules.
    // Specification EIP-649: https://eips.ethereum.org/EIPS/eip-649
    calcDifficultyByzantium = makeDifficultyCalculator(big.NewInt(3000000))
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine-specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
    errOlderBlockTime    = errors.New("timestamp older than parent")
    errTooManyUncles     = errors.New("too many uncles")
    errDuplicateUncle    = errors.New("duplicate uncle")
    errUncleIsAncestor   = errors.New("uncle is ancestor")
    errDanglingUncle     = errors.New("uncle's parent is not ancestor")
    errInvalidDifficulty = errors.New("non-positive difficulty")
    errInvalidMixDigest  = errors.New("invalid mix digest")
    errInvalidPoW        = errors.New("invalid proof-of-work")
)

// EthashLachesis is a consensus engine that integrates Ethash PoW with the Lachesis consensus algorithm.
type EthashLachesis struct {
    ethash   *Ethash
    lachesis *Lachesis
    pos      *PoS
    pot      *PoT
    trust    *ProofOfTrust
}

// NewEthashLachesis returns a new EthashLachesis consensus engine.
func NewEthashLachesis(config *params.ChainConfig, dagDir string, cacheDir string, powMode Mode) *EthashLachesis {
    ethash := New(config, dagDir, cacheDir, powMode)
    lachesis := NewLachesisConsensus()
    pos := NewPoS()
    pot := NewPoT()
    trust := NewProofOfTrust()
    return &EthashLachesis{
        ethash:   ethash,
        lachesis: lachesis,
        pos:      pos,
        pot:      pot,
        trust:    trust,
    }
}

// Finalize implements the block finalization process, integrating PoS, PoT, and Proof of Trust rewards.
func (el *EthashLachesis) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
    // Call the base Ethash finalization
    el.ethash.Finalize(chain, header, state, txs, uncles)

    // Distribute rewards using the Calculate functions from misc.go
    posReward := misc.CalculatePoSReward(el.pos.TotalStake, el.pos.ValidatorStake, el.pos.Uptime, big.NewInt(1e18))
    potReward := misc.CalculatePoTReward(el.pot.TotalTransactions, el.pot.ValidatorTransactions, big.NewInt(1e18))
    trustReward := misc.CalculateTrustReward(el.trust.Uptime, big.NewInt(1e18))

    // Add balances to the state
    state.AddBalance(el.pos.ValidatorAddress, posReward)
    state.AddBalance(el.pot.ValidatorAddress, potReward)
    state.AddBalance(el.trust.ValidatorAddress, trustReward)
}

// Calculate PoS, PoT, and Proof of Trust rewards

func (ethash *Ethash) Author(header *types.Header) (common.Address, error) {
    return header.Coinbase, nil
}

func (ethash *Ethash) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
    if ethash.config.PowMode == ModeFullFake {
        return nil
    }

    number := header.Number.Uint64()
    if chain.GetHeader(header.Hash(), number) != nil {
        return nil
    }
    parent := chain.GetHeader(header.ParentHash, number-1)
    if parent == nil {
        return consensus.ErrUnknownAncestor
    }

    return ethash.verifyHeader(chain, header, parent, false, seal, time.Now().Unix())
}

func (ethash *Ethash) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
    if ethash.config.PowMode == ModeFullFake || len(headers) == 0 {
        abort, results := make(chan struct{}), make(chan error, len(headers))
        for i := 0; i < len(headers); i++ {
            results <- nil
        }
        return abort, results
    }

    workers := runtime.GOMAXPROCS(0)
    if len(headers) < workers {
        workers = len(headers)
    }

    var (
        inputs  = make(chan int)
        done    = make(chan int, workers)
        errors  = make([]error, len(headers))
        abort   = make(chan struct{})
        unixNow = time.Now().Unix()
    )
    for i := 0; i < workers; i++ {
        go func() {
            for index := range inputs {
                errors[index] = ethash.verifyHeaderWorker(chain, headers, seals, index, unixNow)
                done <- index
            }
        }()
    }

    errorsOut := make(chan error, len(headers))
    go func() {
        defer close(inputs)
        var (
            in, out = 0, 0
            checked = make([]bool, len(headers))
            inputs  = inputs
        )
        for {
            select {
            case inputs <- in:
                if in++; in == len(headers) {
                    inputs = nil
                }
            case index := <-done:
                for checked[index] = true; checked[out]; out++ {
                    errorsOut <- errors[out]
                    if out == len(headers)-1 {
                        return
                    }
                }
            case <-abort:
                return
            }
        }
    }()
    return abort, errorsOut
}

func (ethash *Ethash) verifyHeaderWorker(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool, index int, unixNow int64) error {
    var parent *types.Header
    if index == 0 {
        parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
    } else if headers[index-1].Hash() == headers[index].ParentHash {
        parent = headers[index-1]
    }
    if parent == nil {
        return consensus.ErrUnknownAncestor
    }
    return ethash.verifyHeader(chain, headers[index], parent, false, seals[index], unixNow)
}

func (ethash *Ethash) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
    if ethash.config.PowMode == ModeFullFake {
        return nil
    }

    if len(block.Uncles()) > maxUncles {
        return errTooManyUncles
    }
    if len(block.Uncles()) == 0 {
        return nil
    }

    uncles, ancestors := mapset.NewSet(), make(map[common.Hash]*types.Header)

    number, parent := block.NumberU64()-1, block.ParentHash()
    for i := 0; i < 7; i++ {
        ancestorHeader := chain.GetHeader(parent, number)
        if ancestorHeader == nil {
            break
        }
        ancestors[parent] = ancestorHeader

        if ancestorHeader.UncleHash != types.EmptyUncleHash {
            ancestor := chain.GetBlock(parent, number)
            if ancestor == nil {
                break
            }
            for _, uncle := range ancestor.Uncles() {
                uncles.Add(uncle.Hash())
            }
        }
        parent, number = ancestorHeader.ParentHash, number-1
    }
    ancestors[block.Hash()] = block.Header()
    uncles.Add(block.Hash())

    for _, uncle := range block.Uncles() {
        hash := uncle.Hash()
        if uncles.Contains(hash) {
            return errDuplicateUncle
        }
        uncles.Add(hash)

        if ancestors[hash] != nil {
            return errUncleIsAncestor
        }
        if ancestors[uncle.ParentHash] == nil || uncle.ParentHash == block.ParentHash() {
            return errDanglingUncle
        }
        if err := ethash.verifyHeader(chain, uncle, ancestors[uncle.ParentHash], true, true, time.Now().Unix()); err != nil {
            return err
        }
    }
    return nil
}

func (ethash *Ethash) verifyHeader(chain consensus.ChainHeaderReader, header, parent *types.Header, uncle bool, seal bool, unixNow int64) error {
    if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
        return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
    }

    if !uncle {
        if header.Time > uint64(unixNow+allowedFutureBlockTimeSeconds) {
            return consensus.ErrFutureBlock
        }
    }
    if header.Time <= parent.Time {
        return errOlderBlockTime
    }

    expected := ethash.CalcDifficulty(chain, header.Time, parent)
    if expected.Cmp(header.Difficulty) != 0 {
        return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
    }

    if header.GasLimit > params.MaxGasLimit {
        return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
    }

    if header.GasUsed > header.GasLimit {
        return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
    }

    if !chain.Config().IsLondon(header.Number) {
        if header.BaseFee != nil {
            return fmt.Errorf("invalid baseFee before fork: have %d, expected 'nil'", header.BaseFee)
        }
        if err := misc.VerifyGaslimit(parent.GasLimit, header.GasLimit); err != nil {
            return err
        }
    } else if err := misc.VerifyEip1559Header(chain.Config(), parent, header); err != nil {
        return err
    }

    if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
        return consensus.ErrInvalidNumber
    }

    if seal {
        if err := ethash.verifySeal(chain, header, false); err != nil {
            return err
        }
    }

    if err := misc.VerifyDAOHeaderExtraData(chain.Config(), header); err != nil {
        return err
    }
    if err := misc.VerifyForkHashes(chain.Config(), header, uncle); err != nil {
        return err
    }
    return nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (ethash *Ethash) SealHash(header *types.Header) (hash common.Hash) {
    hasher := sha3.NewLegacyKeccak256()

    enc := []interface{}{
        header.ParentHash,
        header.UncleHash,
        header.Coinbase,
        header.Root,
        header.TxHash,
        header.ReceiptHash,
        header.Bloom,
        header.Difficulty,
        header.Number,
        header.GasLimit,
        header.GasUsed,
        header.Time,
        header.Extra,
    }
    if header.BaseFee != nil {
        enc = append(enc, header.BaseFee)
    }
    rlp.Encode(hasher, enc)
    hasher.Sum(hash[:0])
    return hash
}

// Some weird constants to avoid constant memory allocs for them.
var (
    big8  = big.NewInt(8)
    big32 = big.NewInt(32)
)

// AccumulateRewards credits the coinbase of the given block with the mining
// reward. The total reward consists of the static block reward and rewards for
// included uncles. The coinbase of each uncle block is also rewarded.
func accumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
    // Select the correct block reward based on chain progression
    blockReward := big.NewInt(1e+18) // 1 ALT in wei (adjust according to your token's decimals)

    // Calculate PoW and PoS + PoT + PoT rewards
    powReward := new(big.Int).Set(blockReward) // 1 ALT for PoW Ethash miners
    posPotReward := new(big.Int).Set(blockReward) // 1 ALT for PoW + PoS + PoT + PoT participants

    // Distribute rewards to PoW miners (coinbase)
    reward := new(big.Int).Set(powReward)
    r := new(big.Int)
    for _, uncle := range uncles {
        r.Add(uncle.Number, big8)
        r.Sub(r, header.Number)
        r.Mul(r, blockReward)
        r.Div(r, big8)
        state.AddBalance(uncle.Coinbase, r)

        r.Div(powReward, big32)
        reward.Add(reward, r)
    }
    state.AddBalance(header.Coinbase, reward) // PoW reward to miner

    // Distribute rewards to PoS + PoT + PoT participants
    distributePoSPoTRewards(state, header, posPotReward)
}

// Custom function to distribute PoS + PoT + PoT rewards
func distributePoSPoTRewards(state *state.StateDB, header *types.Header, reward *big.Int) {
    // Logic to identify PoS validators, PoT participants, and PoT (Proof of Trust)
    for _, participant := range getPoSAndPoTParticipants() {
        // Calculate individual reward based on their contribution to PoW, PoS, PoT, and uptime
        individualReward := calculateIndividualReward(participant, reward)
        state.AddBalance(participant.Address, individualReward)
    }
}

// Calculate individual rewards for PoS + PoT + PoT participants
func calculateIndividualReward(participant Participant, totalReward *big.Int) *big.Int {
    // Implement logic to calculate reward based on PoW, PoS stake, PoT transaction volume, and PoT (Proof of Trust)
    // This could involve factors like stake size, number of 0.0004 ALT transactions, and node uptime

    // Example placeholder logic
    stakeFactor := new(big.Int).Set(participant.Stake)
    transactionFactor := new(big.Int).SetUint64(participant.TransactionCount)
    uptimeFactor := new(big.Int).SetUint64(participant.UptimePercentage)

    // Calculate the reward based on these factors (this is an example and should be adjusted to your logic)
    reward := new(big.Int).Mul(stakeFactor, transactionFactor)
    reward.Mul(reward, uptimeFactor)
    reward.Div(reward, big.NewInt(10000)) // Example divisor to normalize

    // Ensure reward does not exceed total available
    if reward.Cmp(totalReward) > 0 {
        reward.Set(totalReward)
    }

    return reward
}

// This function would return the list of participants eligible for PoS and PoT rewards
func getPoSAndPoTParticipants() []Participant {
    // Implement logic to get the list of PoS, PoT, and PoT (Proof of Trust) participants
    // A participant could be a structure containing their address, stake size, transaction count, and uptime
    return []Participant{}
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func (ethash *Ethash) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
    return CalcDifficulty(chain.Config(), time, parent)
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func CalcDifficulty(config *params.ChainConfig, time uint64, parent *types.Header) *big.Int {
    next := new(big.Int).Add(parent.Number, big1)
    switch {
    case config.IsEthPoWFork(next):
        if config.EthPoWForkBlock != nil && big.NewInt(0).Add(config.EthPoWForkBlock, big.NewInt(2048)).Cmp(next) == 0 {
            return params.ETHWStartDifficulty // Reset difficulty
        }

        if config.EthPoWForkBlock != nil && config.EthPoWForkBlock.Cmp(next) == 0 {
            return big.NewInt(1) // Reset
        }
        return calcDifficultyEthPoW(time, parent)
    case config.IsGrayGlacier(next):
        return calcDifficultyEip5133(time, parent)
    case config.IsArrowGlacier(next):
        return calcDifficultyEip4345(time, parent)
    case config.IsLondon(next):
        return calcDifficultyEip3554(time, parent)
    case config.IsMuirGlacier(next):
        return calcDifficultyEip2384(time, parent)
    case config.IsConstantinople(next):
        return calcDifficultyConstantinople(time, parent)
    case config.IsByzantium(next):
        return calcDifficultyByzantium(time, parent)
    case config.IsHomestead(next):
        return calcDifficultyHomestead(time, parent)
    default:
        return calcDifficultyFrontier(time, parent)
    }
}

// Some weird constants to avoid constant memory allocs for them.
var (
    expDiffPeriod = big.NewInt(100000)
    big1          = big.NewInt(1)
    big2          = big.NewInt(2)
    big9          = big.NewInt(9)
    big10         = big.NewInt(10)
    bigMinus99    = big.NewInt(-99)
)

// calcDifficultyEthPOW creates a difficultyCalculator with the original Proof-of-work (PoW).
// Remain old calculations & deleted fakeBlockNumber
func calcDifficultyEthPoW(time uint64, parent *types.Header) *big.Int {
    bigTime := new(big.Int).SetUint64(time)
    bigParentTime := new(big.Int).SetUint64(parent.Time)

    // holds intermediate values to make the algo easier to read & audit
    x := new(big.Int)
    y := new(big.Int)

    x.Sub(bigTime, bigParentTime)
    x.Div(x, big9)
    if parent.UncleHash == types.EmptyUncleHash {
        x.Sub(big1, x)
    } else {
        x.Sub(big2, x)
    }
    if x.Cmp(bigMinus99) < 0 {
        x.Set(bigMinus99)
    }
    y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
    x.Mul(y, x)
    x.Add(parent.Difficulty, x)

    if x.Cmp(params.MinimumDifficulty) < 0 {
        x.Set(params.MinimumDifficulty)
    }
    return x
}

// makeDifficultyCalculator creates a difficultyCalculator with the given bomb-delay.
// the difficulty is calculated with Byzantium rules, which differs from Homestead in
// how uncles affect the calculation.
func makeDifficultyCalculator(bombDelay *big.Int) func(time uint64, parent *types.Header) *big.Int {
    bombDelayFromParent := new(big.Int).Sub(bombDelay, big1)
    return func(time uint64, parent *types.Header) *big.Int {
        bigTime := new(big.Int).SetUint64(time)
        bigParentTime := new(big.Int).SetUint64(parent.Time)

        x := new(big.Int)
        y := new(big.Int)

        x.Sub(bigTime, bigParentTime)
        x.Div(x, big9)
        if parent.UncleHash == types.EmptyUncleHash {
            x.Sub(big1, x)
        } else {
            x.Sub(big2, x)
        }
        if x.Cmp(bigMinus99) < 0 {
            x.Set(bigMinus99)
        }
        y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
        x.Mul(y, x)
        x.Add(parent.Difficulty, x)

        if x.Cmp(params.MinimumDifficulty) < 0 {
            x.Set(params.MinimumDifficulty)
        }
        fakeBlockNumber := new(big.Int)
        if parent.Number.Cmp(bombDelayFromParent) >= 0 {
            fakeBlockNumber = fakeBlockNumber.Sub(parent.Number, bombDelayFromParent)
        }
        periodCount := fakeBlockNumber
        periodCount.Div(periodCount, expDiffPeriod)

        if periodCount.Cmp(big1) > 0 {
            y.Sub(periodCount, big2)
            y.Exp(big2, y, nil)
            x.Add(x, y)
        }
        return x
    }
}

// calcDifficultyHomestead is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time given the
// parent block's time and difficulty. The calculation uses the Homestead rules.
func calcDifficultyHomestead(time uint64, parent *types.Header) *big.Int {
    bigTime := new(big.Int).SetUint64(time)
    bigParentTime := new(big.Int).SetUint64(parent.Time)

    x := new(big.Int)
    y := new(big.Int)

    x.Sub(bigTime, bigParentTime)
    x.Div(x, big10)
    x.Sub(big1, x)

    if x.Cmp(bigMinus99) < 0 {
        x.Set(bigMinus99)
    }
    y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
    x.Mul(y, x)
    x.Add(parent.Difficulty, x)

    if x.Cmp(params.MinimumDifficulty) < 0 {
        x.Set(params.MinimumDifficulty)
    }
    periodCount := new(big.Int).Add(parent.Number, big1)
    periodCount.Div(periodCount, expDiffPeriod)

    if periodCount.Cmp(big1) > 0 {
        y.Sub(periodCount, big2)
        y.Exp(big2, y, nil)
        x.Add(x, y)
    }
    return x
}

// calcDifficultyFrontier is the difficulty adjustment algorithm. It returns the
// difficulty that a new block should have when created at time given the parent
// block's time and difficulty. The calculation uses the Frontier rules.
func calcDifficultyFrontier(time uint64, parent *types.Header) *big.Int {
    diff := new(big.Int)
    adjust := new(big.Int).Div(parent.Difficulty, params.DifficultyBoundDivisor)
    bigTime := new(big.Int)
    bigParentTime := new(big.Int)

    bigTime.SetUint64(time)
    bigParentTime.SetUint64(parent.Time)

    if bigTime.Sub(bigTime, bigParentTime).Cmp(params.DurationLimit) < 0 {
        diff.Add(parent.Difficulty, adjust)
    } else {
        diff.Sub(parent.Difficulty, adjust)
    }
    if diff.Cmp(params.MinimumDifficulty) < 0 {
        diff.Set(params.MinimumDifficulty)
    }

    periodCount := new(big.Int).Add(parent.Number, big1)
    periodCount.Div(periodCount, expDiffPeriod)
    if periodCount.Cmp(big1) > 0 {
        expDiff := periodCount.Sub(periodCount, big2)
        expDiff.Exp(big2, expDiff, nil)
        diff.Add(diff, expDiff)
        diff = math.BigMax(diff, params.MinimumDifficulty)
    }
    return diff
}

// verifySeal checks whether a block satisfies the PoW difficulty requirements,
// either using the usual ethash cache for it, or alternatively using a full DAG
// to make remote mining fast.
func (ethash *Ethash) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, fulldag bool) error {
    if ethash.config.PowMode == ModeFake || ethash.config.PowMode == ModeFullFake {
        time.Sleep(ethash.fakeDelay)
        if ethash.fakeFail == header.Number.Uint64() {
            return errInvalidPoW
        }
        return nil
    }
    if ethash.shared != nil {
        return ethash.shared.verifySeal(chain, header, fulldag)
    }
    if header.Difficulty.Sign() <= 0 {
        return errInvalidDifficulty
    }
    number := header.Number.Uint64()

    var (
        digest []byte
        result []byte
    )
    if fulldag {
        dataset := ethash.dataset(number, true)
        if dataset.generated() {
            digest, result = hashimotoFull(dataset.dataset, ethash.SealHash(header).Bytes(), header.Nonce.Uint64())
            runtime.KeepAlive(dataset)
        } else {
            fulldag = false
        }
    }
    if !fulldag {
        cache := ethash.cache(number)
        size := datasetSize(number)
        if ethash.config.PowMode == ModeTest {
            size = 32 * 1024
        }
        digest, result = hashimotoLight(size, cache.cache, ethash.SealHash(header).Bytes(), header.Nonce.Uint64())
        runtime.KeepAlive(cache)
    }
    if !bytes.Equal(header.MixDigest[:], digest) {
        return errInvalidMixDigest
    }
    target := new(big.Int).Div(two256, header.Difficulty)
    if new(big.Int).SetBytes(result).Cmp(target) > 0 {
        return errInvalidPoW
    }
    return nil
}

// Prepare implements consensus.Engine, initializing the difficulty field of a
// header to conform to the ethash protocol. The changes are done inline.
func (ethash *Ethash) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
    parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
    if parent == nil {
        return consensus.ErrUnknownAncestor
    }
    header.Difficulty = ethash.CalcDifficulty(chain, header.Time, parent)
    return nil
}

// Finalize implements consensus.Engine, accumulating the block and uncle rewards,
// setting the final state on the header
func (ethash *Ethash) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
    // Accumulate any block and uncle rewards and commit the final state root
    accumulateRewards(chain.Config(), state, header, uncles)
    header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
}

// FinalizeAndAssemble implements consensus.Engine, accumulating the block and
// uncle rewards, setting the final state and assembling the block.
func (ethash *Ethash) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
    ethash.Finalize(chain, header, state, txs, uncles)
    return types.NewBlock(header, txs, uncles, receipts, trie.NewStackTrie(nil)), nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (ethash *Ethash) SealHash(header *types.Header) (hash common.Hash) {
    hasher := sha3.NewLegacyKeccak256()

    enc := []interface{}{
        header.ParentHash,
        header.UncleHash,
        header.Coinbase,
        header.Root,
        header.TxHash,
        header.ReceiptHash,
        header.Bloom,
        header.Difficulty,
        header.Number,
        header.GasLimit,
        header.GasUsed,
        header.Time,
        header.Extra,
    }
    if header.BaseFee != nil {
        enc = append(enc, header.BaseFee)
    }
    rlp.Encode(hasher, enc)
    hasher.Sum(hash[:0])
    return hash
}

// Some weird constants to avoid constant memory allocs for them.
var (
    big8  = big.NewInt(8)
    big32 = big.NewInt(32)
)
