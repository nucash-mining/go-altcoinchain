package consensus

import (
    "math/big"
    "github.com/Altcoinchain/go-altcoinchain/common"
)

// Validator represents a participant in the PoS mechanism.
type Validator struct {
    Address     common.Address // Validator's address
    Stake       *big.Int       // Amount staked by the validator
    LastReward  uint64         // Block number when the last reward was given
    Uptime      uint64         // Uptime percentage
    IsValidator bool           // Flag to indicate if currently active as a validator
}

// PoS manages the state of all validators in the network.
type PoS struct {
    Validators map[common.Address]*Validator // Mapping of validator addresses to their details
    TotalStake *big.Int                      // Total amount staked in the network
}

// NewPoS initializes a new PoS instance.
func NewPoS() *PoS {
    return &PoS{
        Validators: make(map[common.Address]*Validator),
        TotalStake: big.NewInt(0),
    }
}

// UpdateValidator updates or adds a new validator's stake and other details.
func (pos *PoS) UpdateValidator(address common.Address, stake *big.Int, blockNumber uint64) {
    validator, exists := pos.Validators[address]
    if !exists {
        validator = &Validator{
            Address: address,
            Stake:   stake,
            IsValidator: true,
        }
        pos.Validators[address] = validator
        pos.TotalStake.Add(pos.TotalStake, stake)
    } else {
        validator.Stake.Add(validator.Stake, stake)
    }
    validator.LastReward = blockNumber
}
