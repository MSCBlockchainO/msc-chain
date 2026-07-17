package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"runtime"
	"sync"
)

const parallelMerkleParentThreshold = 64

type TxValidationResult struct {
	// `Index` stores the current position in the related collection.
	Index int `json:"index"`
	// `TxID` stores the transaction data handled by this operation.
	TxID string `json:"tx_id"`
	// `Valid` stores whether the related condition is satisfied.
	Valid bool `json:"valid"`
	// `Error` stores the error produced by this operation.
	Error string `json:"error,omitempty"`
}

type Ed25519VerificationJob struct {
	// `Index` stores the current position in the related collection.
	Index int `json:"index"`
	// `Label` stores an optional correlation id for diagnostics.
	Label string `json:"label,omitempty"`
	// `PublicKey` stores the public key used by this verification.
	PublicKey ed25519.PublicKey `json:"-"`
	// `Message` stores the signed payload.
	Message []byte `json:"-"`
	// `Signature` stores the ed25519 signature.
	Signature []byte `json:"-"`
}

type Ed25519VerificationResult struct {
	// `Index` stores the current position in the related collection.
	Index int `json:"index"`
	// `Label` stores the optional correlation id copied from the job.
	Label string `json:"label,omitempty"`
	// `Valid` stores whether the signature verified.
	Valid bool `json:"valid"`
	// `Error` stores the error produced by this operation.
	Error string `json:"error,omitempty"`
}

func signatureWorkerCount(total int, workers int) int {
	if total <= 0 {
		return 0
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers < 1 {
		workers = 1
	}
	if workers > total {
		workers = total
	}
	return workers
}

func executeParallelDTLExecutionPlans(
	state *DTLState,
	plans []parallelDTLExecutionPlan,
	workers int,
) []parallelDTLExecutionResult {
	results := make([]parallelDTLExecutionResult, len(plans))
	if len(plans) == 0 {
		return results
	}
	workers = signatureWorkerCount(len(plans), workers)
	work := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				patch, err := executeParallelDTLPlan(state, plans[idx])
				results[idx] = parallelDTLExecutionResult{
					index: idx,
					patch: patch,
					err:   err,
				}
			}
		}()
	}
	for idx := range plans {
		work <- idx
	}
	close(work)
	wg.Wait()
	return results
}

// ParallelVerifyEd25519Signatures verifies independent ed25519 signatures using a worker pool.
func ParallelVerifyEd25519Signatures(jobs []Ed25519VerificationJob, workers int) []Ed25519VerificationResult {
	results := make([]Ed25519VerificationResult, len(jobs))
	if len(jobs) == 0 {
		return results
	}
	workers = signatureWorkerCount(len(jobs), workers)
	work := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				job := jobs[idx]
				result := Ed25519VerificationResult{
					Index: job.Index,
					Label: job.Label,
				}
				switch {
				case len(job.PublicKey) != ed25519.PublicKeySize:
					result.Error = "invalid public key"
				case len(job.Signature) != ed25519.SignatureSize:
					result.Error = "invalid signature"
				case len(job.Message) == 0:
					result.Error = "empty message"
				case ed25519.Verify(job.PublicKey, job.Message, job.Signature):
					result.Valid = true
				default:
					result.Error = "signature verification failed"
				}
				results[idx] = result
			}
		}()
	}
	for idx := range jobs {
		work <- idx
	}
	close(work)
	wg.Wait()
	return results
}

// ParallelVerifySignedBlockTransactions verifies block transaction signatures in parallel.
func ParallelVerifySignedBlockTransactions(txs []Transaction, workers int) []TxValidationResult {
	results := make([]TxValidationResult, len(txs))
	if len(txs) == 0 {
		return results
	}
	workers = signatureWorkerCount(len(txs), workers)
	work := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range work {
				tx := txs[idx]
				err := verifySignedBlockTransaction(tx)
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
		work <- idx
	}
	close(work)
	wg.Wait()
	return results
}

func merkleParentAt(level [][]byte, pairIndex int) []byte {
	leftIndex := pairIndex * 2
	left := level[leftIndex]
	right := left
	if leftIndex+1 < len(level) {
		right = level[leftIndex+1]
	}
	pair := make([]byte, 0, len(left)+len(right))
	pair = append(pair, left...)
	pair = append(pair, right...)
	sum := sha256.Sum256(pair)
	return append([]byte(nil), sum[:]...)
}

// ParallelMerkleParentLevel hashes one merkle tree level while preserving pair order.
func ParallelMerkleParentLevel(level [][]byte, workers int) [][]byte {
	if len(level) == 0 {
		return nil
	}
	parentCount := (len(level) + 1) / 2
	next := make([][]byte, parentCount)
	if parentCount < parallelMerkleParentThreshold {
		for i := 0; i < parentCount; i++ {
			next[i] = merkleParentAt(level, i)
		}
		return next
	}
	workers = signatureWorkerCount(parentCount, workers)
	work := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for pairIndex := range work {
				next[pairIndex] = merkleParentAt(level, pairIndex)
			}
		}()
	}
	for i := 0; i < parentCount; i++ {
		work <- i
	}
	close(work)
	wg.Wait()
	return next
}

// ParallelValidateTransactions implements the parallel validate transactions helper.
func ParallelValidateTransactions(txs []Transaction, ledger Ledger, workers int) []TxValidationResult {
	// `results` stores the result produced by this operation.
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

	// `baseLedger` stores the value produced by this operation.
	baseLedger := ledger.Clone()
	// `jobs` stores the current position in the related collection.
	jobs := make(chan int)
	// `wg` stores the value used by this operation.
	var wg sync.WaitGroup
	// `validator` stores whether the related condition is satisfied.
	validator := &Mempool{}
	// `i` stores the current position in the related collection.
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// `idx` tracks the current position in the related collection.
			for idx := range jobs {
				// `tx` stores the transaction data handled by this operation.
				tx := txs[idx]
				// `ledgerCopy` stores the value produced by this operation.
				ledgerCopy := baseLedger.Clone()
				// `err` stores the error produced by this operation.
				err := validator.ValidateTransaction(tx, &ledgerCopy)
				// `result` stores the result produced by this operation.
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
	// `idx` tracks the current position in the related collection.
	for idx := range txs {
		jobs <- idx
	}
	close(jobs)
	wg.Wait()
	return results
}

// PriorityTransactions implements the priority transactions helper.
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
	// `txs` stores the transaction data handled by this operation.
	txs := append([]Transaction(nil), m.Transactions...)
	return selectTxBatch(txs, ledger)
}

type AccountStateCache struct {
	// `mu` stores the synchronization state protecting shared data.
	mu sync.RWMutex
	// `balances` stores the value associated with this record.
	balances map[string]int
	// `loader` stores the value associated with this record.
	loader func(coin string, address string) (int, bool)
}

// NewAccountStateCache creates a new account state cache.
func NewAccountStateCache(loader func(coin string, address string) (int, bool)) *AccountStateCache {
	return &AccountStateCache{
		balances: make(map[string]int),
		loader:   loader,
	}
}

// GetBalance returns balance.
func (c *AccountStateCache) GetBalance(coin string, address string) (int, bool) {
	if c == nil {
		return 0, false
	}
	// `key` stores the key used to access the related value.
	key := balanceKey(coin, address)
	c.mu.RLock()
	// `value` and `ok` store whether the related condition is satisfied.
	value, ok := c.balances[key]
	c.mu.RUnlock()
	if ok {
		return value, true
	}
	if c.loader == nil {
		return 0, false
	}
	// `loaded` and `ok` store whether the related condition is satisfied.
	loaded, ok := c.loader(normalizeCoin(coin), address)
	if !ok {
		return 0, false
	}
	c.PutBalance(coin, address, loaded)
	return loaded, true
}

// PutBalance implements the put balance helper.
func (c *AccountStateCache) PutBalance(coin string, address string, value int) {
	if c == nil {
		return
	}
	// `key` stores the key used to access the related value.
	key := balanceKey(coin, address)
	c.mu.Lock()
	if c.balances == nil {
		c.balances = make(map[string]int)
	}
	c.balances[key] = value
	c.mu.Unlock()
}

// InvalidateBalance implements the invalidate balance helper.
func (c *AccountStateCache) InvalidateBalance(coin string, address string) {
	if c == nil {
		return
	}
	// `key` stores the key used to access the related value.
	key := balanceKey(coin, address)
	c.mu.Lock()
	delete(c.balances, key)
	c.mu.Unlock()
}

// Size implements the size helper.
func (c *AccountStateCache) Size() int {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.balances)
}
