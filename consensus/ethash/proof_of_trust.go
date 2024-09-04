package consensus

import (
    "github.com/Altcoinchain/go-altcoinchain/common"
)

// TrustRecord keeps track of the uptime and reliability of a node.
type TrustRecord struct {
    Address    common.Address // Address of the node operator
    Uptime     uint64          // Uptime percentage (0-100)
    LastUpdate uint64          // Block number of the last uptime update
}

// ProofOfTrust manages trust records for nodes in the network.
type ProofOfTrust struct {
    TrustRecords map[common.Address]*TrustRecord // Mapping of addresses to their trust data
}

// NewProofOfTrust initializes a new ProofOfTrust instance.
func NewProofOfTrust() *ProofOfTrust {
    return &ProofOfTrust{
        TrustRecords: make(map[common.Address]*TrustRecord),
    }
}

// UpdateTrust updates or adds a trust record for a node.
func (trust *ProofOfTrust) UpdateTrust(address common.Address, uptime uint64, blockNumber uint64) {
    record, exists := trust.TrustRecords[address]
    if !exists {
        record = &TrustRecord{
            Address:    address,
            Uptime:     uptime,
            LastUpdate: blockNumber,
        }
        trust.TrustRecords[address] = record
    } else {
        record.Uptime = uptime
        record.LastUpdate = blockNumber
    }
}
