package consensus

import (
    "github.com/Altcoinchain/go-altcoinchain/common"
)

// TransactionRecord keeps track of transaction activity for an address.
type TransactionRecord struct {
    Address          common.Address // Address of the participant
    TransactionCount uint64          // Number of transactions performed by this address
    LastTransaction  uint64          // Block number of the last transaction
}

// PoT manages transaction records for participants in the network.
type PoT struct {
    TransactionRecords map[common.Address]*TransactionRecord // Mapping of addresses to their transaction data
    TotalTransactions  uint64                                // Total number of transactions in the network
}

// NewPoT initializes a new PoT instance.
func NewPoT() *PoT {
    return &PoT{
        TransactionRecords: make(map[common.Address]*TransactionRecord),
        TotalTransactions:  0,
    }
}

// RecordTransaction records a transaction performed by an address.
func (pot *PoT) RecordTransaction(address common.Address, blockNumber uint64) {
    record, exists := pot.TransactionRecords[address]
    if !exists {
        record = &TransactionRecord{
            Address:         address,
            TransactionCount: 1,
            LastTransaction:  blockNumber,
        }
        pot.TransactionRecords[address] = record
        pot.TotalTransactions++
    } else {
        record.TransactionCount++
        record.LastTransaction = blockNumber
    }
}
