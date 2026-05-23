package main

import (
	"runtime"
	"sync"
)

type TxValidationResult struct {
	Index int    `json:"index"`
	TxID  string `json:"tx_id"`
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

func ParallelValidateTransactions(txs []Transaction, ledger Ledger, workers int) []TxValidationResult {
	results := make([]TxValidationResult, len(txs))
	if len(txs) == 0 {
		return results
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(txs) {
		workers = len(txs)
	}

	baseLedger := ledger.Clone()
	jobs := make(chan int)
	var wg sync.WaitGroup
	validator := &Mempool{}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				tx := txs[idx]
				ledgerCopy := baseLedger.Clone()
				err := validator.ValidateTransaction(tx, &ledgerCopy)
				result := TxValidationResult{
					Index: idx,
					TxID:  txID(tx),
					Valid: err == nil,
				}
				if err != nil {
					result.Error = err.Error()
				}
				results[idx] = result
			}
		}()
	}
	for idx := range txs {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	return results
}

func (m *Mempool) PriorityTransactions(ledger Ledger) []Transaction {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureIndexesLocked()
	if len(m.Transactions) == 0 {
		return nil
	}
	txs := append([]Transaction(nil), m.Transactions...)
	return selectTxBatch(txs, ledger)
}

type AccountStateCache struct {
	mu       sync.RWMutex
	balances map[string]int
	loader   func(coin string, address string) (int, bool)
}

func NewAccountStateCache(loader func(coin string, address string) (int, bool)) *AccountStateCache {
	return &AccountStateCache{
		balances: make(map[string]int),
		loader:   loader,
	}
}

func (c *AccountStateCache) GetBalance(coin string, address string) (int, bool) {
	if c == nil {
		return 0, false
	}
	key := balanceKey(coin, address)
	c.mu.RLock()
	value, ok := c.balances[key]
	c.mu.RUnlock()
	if ok {
		return value, true
	}
	if c.loader == nil {
		return 0, false
	}
	loaded, ok := c.loader(normalizeCoin(coin), address)
	if !ok {
		return 0, false
	}
	c.PutBalance(coin, address, loaded)
	return loaded, true
}

func (c *AccountStateCache) PutBalance(coin string, address string, value int) {
	if c == nil {
		return
	}
	key := balanceKey(coin, address)
	c.mu.Lock()
	if c.balances == nil {
		c.balances = make(map[string]int)
	}
	c.balances[key] = value
	c.mu.Unlock()
}

func (c *AccountStateCache) InvalidateBalance(coin string, address string) {
	if c == nil {
		return
	}
	key := balanceKey(coin, address)
	c.mu.Lock()
	delete(c.balances, key)
	c.mu.Unlock()
}

func (c *AccountStateCache) Size() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.balances)
}
