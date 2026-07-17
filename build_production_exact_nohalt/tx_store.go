package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

type TxRecord struct {
	Tx        Transaction `json:"tx"`
	Height    uint64      `json:"height"`
	Index     int         `json:"index"`
	BlockHash string      `json:"block_hash"`
}

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

func txRecordKey(id string) []byte {
	norm := normalizeTxKey(id)
	if norm == "" {
		return nil
	}
	return []byte("tx:" + norm)
}

func (db *NodeDB) StoreTxRecords(block Block) error {
	if db == nil || db.Tx == nil {
		return nil
	}
	if len(block.Transactions) == 0 {
		return nil
	}
	height := block.ID
	if height == 0 && block.Height != 0 {
		height = block.Height
	}
	return db.Tx.Update(func(txn *Txn) error {
		for idx, tx := range block.Transactions {
			rec := TxRecord{
				Tx:        tx,
				Height:    height,
				Index:     idx,
				BlockHash: block.BlockHash,
			}
			data, err := json.Marshal(rec)
			if err != nil {
				return err
			}
			enc, err := encryptDBValue(data)
			if err != nil {
				return err
			}
			key := txRecordKey(tx.ID)
			if len(key) > 0 {
				if err := txn.Set(key, enc); err != nil {
					return err
				}
			}
			evmKey := txRecordKey(tx.EVMTxHash)
			if len(evmKey) > 0 && !bytes.Equal(evmKey, key) {
				if err := txn.Set(evmKey, enc); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (n *Node) loadTxRecord(txID string) (TxRecord, bool) {
	if n == nil || n.DB == nil || n.DB.Tx == nil {
		return TxRecord{}, false
	}
	key := txRecordKey(txID)
	if len(key) == 0 {
		return TxRecord{}, false
	}
	var rec TxRecord
	err := n.DB.Tx.View(func(txn *Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
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
