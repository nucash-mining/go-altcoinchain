// Copyright 2018 The go-ethereum Authors
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
	"context"
	"errors"
	"math/big"

	"github.com/Altcoinchain/go-altcoinchain/common"
	"github.com/Altcoinchain/go-altcoinchain/common/hexutil"
	"github.com/Altcoinchain/go-altcoinchain/core/types"
	"github.com/Altcoinchain/go-altcoinchain/rpc"
)

var errEthashStopped = errors.New("ethash stopped")

// API provides an API to access the consensus related information.
type API struct {
	ethash *EthashLachesis
}

// NewAPI creates a new API instance for the EthashLachesis consensus engine.
func NewAPI(ethash *EthashLachesis) *API {
	return &API{ethash: ethash}
}

// GetWork returns a work package for external miner.
//
// The work package consists of 3 strings:
//   result[0] - 32 bytes hex encoded current block header pow-hash
//   result[1] - 32 bytes hex encoded seed hash used for DAG
//   result[2] - 32 bytes hex encoded boundary condition ("target"), 2^256/difficulty
//   result[3] - hex encoded block number
func (api *API) GetWork() ([4]string, error) {
	if api.ethash.ethash.remote == nil {
		return [4]string{}, errors.New("not supported")
	}

	var (
		workCh = make(chan [4]string, 1)
		errc   = make(chan error, 1)
	)
	select {
	case api.ethash.ethash.remote.fetchWorkCh <- &sealWork{errc: errc, res: workCh}:
	case <-api.ethash.ethash.remote.exitCh:
		return [4]string{}, errEthashStopped
	}
	select {
	case work := <-workCh:
		return work, nil
	case err := <-errc:
		return [4]string{}, err
	}
}

// SubmitWork can be used by external miner to submit their POW solution.
// It returns an indication if the work was accepted.
// Note either an invalid solution, a stale work a non-existent work will return false.
func (api *API) SubmitWork(nonce types.BlockNonce, hash, digest common.Hash) bool {
	if api.ethash.ethash.remote == nil {
		return false
	}

	var errc = make(chan error, 1)
	select {
	case api.ethash.ethash.remote.submitWorkCh <- &mineResult{
		nonce:     nonce,
		mixDigest: digest,
		hash:      hash,
		errc:      errc,
	}:
	case <-api.ethash.ethash.remote.exitCh:
		return false
	}
	err := <-errc
	return err == nil
}

// SubmitHashrate can be used for remote miners to submit their hash rate.
// This enables the node to report the combined hash rate of all miners
// which submit work through this node.
//
// It accepts the miner hash rate and an identifier which must be unique
// between nodes.
func (api *API) SubmitHashrate(rate hexutil.Uint64, id common.Hash) bool {
	if api.ethash.ethash.remote == nil {
		return false
	}

	var done = make(chan struct{}, 1)
	select {
	case api.ethash.ethash.remote.submitRateCh <- &hashrate{done: done, rate: uint64(rate), id: id}:
	case <-api.ethash.ethash.remote.exitCh:
		return false
	}

	// Block until hash rate submitted successfully.
	<-done
	return true
}

// GetHashrate returns the current hashrate for local CPU miner and remote miner.
func (api *API) GetHashrate() uint64 {
	return uint64(api.ethash.Hashrate())
}

// GetPoWDifficulty returns the current difficulty level based on the PoW mechanism.
func (api *API) GetPoWDifficulty(ctx context.Context) (*big.Int, error) {
	return api.ethash.ethash.Difficulty, nil
}

// GetPoSDifficulty returns the current difficulty level based on the PoS mechanism.
func (api *API) GetPoSDifficulty(ctx context.Context) (*big.Int, error) {
	// You might need to calculate or fetch the PoS difficulty
	return big.NewInt(0), nil // Replace with actual logic
}

// GetPoTDifficulty returns the current difficulty level based on the PoT mechanism.
func (api *API) GetPoTDifficulty(ctx context.Context) (*big.Int, error) {
	// You might need to calculate or fetch the PoT difficulty
	return big.NewInt(0), nil // Replace with actual logic
}

// GetPoTrustDifficulty returns the current difficulty level based on the PoTrust mechanism.
func (api *API) GetPoTrustDifficulty(ctx context.Context) (*big.Int, error) {
	// You might need to calculate or fetch the PoTrust difficulty
	return big.NewInt(0), nil // Replace with actual logic
}

// GetCustomDifficulty returns the combined difficulty level based on PoW, PoS, PoT, and PoTrust.
func (api *API) GetCustomDifficulty(ctx context.Context, posFactor, potFactor, trustFactor *big.Int) (*big.Int, error) {
	// Return the calculated difficulty using CalcCustomDifficulty
	return api.ethash.CalcCustomDifficulty(ctx, posFactor, potFactor, trustFactor)
}

// GetValidators returns the list of current validators participating in PoS.
func (api *API) GetValidators(ctx context.Context) ([]common.Address, error) {
	// You might need to fetch the list of validators
	return api.ethash.pos.Validators, nil
}

// GetTransactionRecords returns the list of transactions that contributed to PoT.
func (api *API) GetTransactionRecords(ctx context.Context) ([]types.Transaction, error) {
	// You might need to fetch the list of transactions
	return api.ethash.pot.TransactionRecords, nil
}

// GetTrustRecords returns the list of trust records contributing to PoTrust.
func (api *API) GetTrustRecords(ctx context.Context) ([]TrustRecord, error) {
	// You might need to fetch the trust records
	return api.ethash.trust.TrustRecords, nil
}

// GetUptime returns the current uptime percentage for PoTrust.
func (api *API) GetUptime(ctx context.Context, address common.Address) (int, error) {
	// Fetch the uptime of a particular validator or node
	return api.ethash.trust.GetUptime(address), nil
}

