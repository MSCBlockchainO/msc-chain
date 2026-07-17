package main

import (
	"encoding/json"
	"strings"
)

type TxRecord struct {
	// `Tx` stores the transaction data handled by this operation.
	Tx        Transaction `json:"tx"`
	// `Height` stores the value associated with this record.
	Height    uint64      `json:"height"`
	// `Index` stores the current position in the related collection.
	Index     int         `json:"index"`
	// `BlockHash` stores the block data handled by this operation.
	BlockHash string      `json:"block_hash"`
}

// normalizeTxKey normalizes tx key.
func normalizeTxKey(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	id = strings.ToLower(strings.TrimPrefix(id, "0x"))
	if id == "" {
		return ""
	}
	return id
}

// txRecordKey implements the tx record key helper.
func txRecordKey(id string) []byte {
	// `norm` stores the value produced by this operation.
	norm := normalizeTxKey(id)
	if norm == "" {
		return nil
	}
	return []byte("tx:" + norm)
}

// StoreTxRecords stores tx records.
func (db *NodeDB) StoreTxRecords(block Block) error {
	if db == nil || db.Tx == nil {
		return nil
	}
	if len(block.Transactions) == 0 {
		return nil
	}
	// `height` stores the value produced by this operation.
	height := block.ID
	if height == 0 && block.Height != 0 {
		height = block.Height
	}
	return db.Tx.Update(func(txn *Txn) error {
		// `idx` and `tx` track the transaction data handled by this operation.
		for idx, tx := range block.Transactions {
			// `rec` stores the value produced by this operation.
			rec := TxRecord{
				Tx:        tx,
				Height:    height,
				Index:     idx,
				BlockHash: block.BlockHash,
			}
			// `data` and `err` store the error produced by this operation.
			data, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			// `enc` and `err` store the error produced by this operation.
			enc, err := encryptDBValue(data)
			if err != nil {
				return err
			}
			// `key` stores the key used to access the related value.
			key := txRecordKey(tx.ID)
			if len(key) > 0 {
				// `err` stores the error produced by this operation.
				if err := txn.Set(key, enc); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// loadTxRecord implements the load tx record helper.
func (n *Node) loadTxRecord(txID string) (TxRecord, bool) {
	if n == nil || n.DB == nil || n.DB.Tx == nil {
		return TxRecord{}, false
	}
	// `key` stores the key used to access the related value.
	key := txRecordKey(txID)
	if len(key) == 0 {
		return TxRecord{}, false
	}
	// `rec` stores the value used by this operation.
	var rec TxRecord
	// `err` stores the error produced by this operation.
	err := n.DB.Tx.View(func(txn *Txn) error {
		// `item` and `err` store the error produced by this operation.
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			// `plain` and `derr` store the error produced by this operation.
			plain, derr := decryptDBValue(val)
			if derr != nil {
				return derr
			}
			return json.Unmarshal(plain, &rec)
		})
	})
	if err != nil {
		return TxRecord{}, false
	}
	return rec, true
}
