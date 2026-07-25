package store

import (
	"sync"
	"sync/atomic"
	"time"
)

// AsyncLogHealth is a safe snapshot of request-log persistence health.
type AsyncLogHealth struct {
	QueueDepth           int   `json:"queue_depth"`
	QueueCapacity        int   `json:"queue_capacity"`
	HighWatermark        int64 `json:"high_watermark"`
	SyncFallbacks        int64 `json:"sync_fallbacks"`
	Batches              int64 `json:"batches"`
	LogsWritten          int64 `json:"logs_written"`
	MaxBatchSize         int64 `json:"max_batch_size"`
	WriteFailures        int64 `json:"write_failures"`
	LastFailureUnixMilli int64 `json:"last_failure_unix_milli,omitempty"`
}

// AsyncLogStore wraps a Store and moves request-log writes off the request's
// critical path. A full queue falls back to synchronous persistence, so logs
// are never intentionally dropped.
type AsyncLogStore struct {
	Store
	ch     chan RequestLog
	mu     sync.Mutex
	wg     sync.WaitGroup
	closed chan struct{}
	once   sync.Once

	highWatermark atomic.Int64
	syncFallbacks atomic.Int64
	batches       atomic.Int64
	logsWritten   atomic.Int64
	maxBatchSize  atomic.Int64
	writeFailures atomic.Int64
	lastFailureMS atomic.Int64
}

func NewAsyncLogStore(st Store, bufferSize int) *AsyncLogStore {
	if bufferSize <= 0 {
		bufferSize = 1024
	}
	a := &AsyncLogStore{Store: st, ch: make(chan RequestLog, bufferSize), closed: make(chan struct{})}
	a.wg.Add(1)
	go a.worker()
	return a
}

func (a *AsyncLogStore) worker() {
	defer a.wg.Done()
	for first := range a.ch {
		batch := make([]RequestLog, 1, 64)
		batch[0] = first
		for len(batch) < cap(batch) {
			select {
			case log, ok := <-a.ch:
				if !ok {
					a.writeBatch(batch)
					return
				}
				batch = append(batch, log)
			default:
				a.writeBatch(batch)
				batch = nil
			}
			if batch == nil {
				break
			}
		}
		if batch != nil {
			a.writeBatch(batch)
		}
	}
}

func (a *AsyncLogStore) writeBatch(logs []RequestLog) {
	var err error
	if writer, ok := a.Store.(RequestLogBatchWriter); ok {
		err = writer.AddRequestLogs(logs)
	} else {
		for _, log := range logs {
			if writeErr := a.Store.AddRequestLog(log); writeErr != nil && err == nil {
				err = writeErr
			}
		}
	}
	a.batches.Add(1)
	a.logsWritten.Add(int64(len(logs)))
	size := int64(len(logs))
	for {
		previous := a.maxBatchSize.Load()
		if size <= previous || a.maxBatchSize.CompareAndSwap(previous, size) {
			break
		}
	}
	if err != nil {
		a.recordFailure()
	}
}

func (a *AsyncLogStore) AddRequestLog(log RequestLog) error {
	a.mu.Lock()
	select {
	case <-a.closed:
		a.mu.Unlock()
		return a.writeSync(log)
	default:
	}
	select {
	case a.ch <- log:
		a.recordQueueDepth()
		a.mu.Unlock()
		return nil
	default:
		a.syncFallbacks.Add(1)
		a.mu.Unlock()
		return a.writeSync(log)
	}
}

func (a *AsyncLogStore) writeSync(log RequestLog) error {
	err := a.Store.AddRequestLog(log)
	if err != nil {
		a.recordFailure()
	}
	return err
}

func (a *AsyncLogStore) recordQueueDepth() {
	depth := int64(len(a.ch))
	for {
		previous := a.highWatermark.Load()
		if depth <= previous || a.highWatermark.CompareAndSwap(previous, depth) {
			return
		}
	}
}

func (a *AsyncLogStore) recordFailure() {
	a.writeFailures.Add(1)
	a.lastFailureMS.Store(time.Now().UTC().UnixMilli())
}

func (a *AsyncLogStore) Health() AsyncLogHealth {
	return AsyncLogHealth{
		QueueDepth:           len(a.ch),
		QueueCapacity:        cap(a.ch),
		HighWatermark:        a.highWatermark.Load(),
		SyncFallbacks:        a.syncFallbacks.Load(),
		Batches:              a.batches.Load(),
		LogsWritten:          a.logsWritten.Load(),
		MaxBatchSize:         a.maxBatchSize.Load(),
		WriteFailures:        a.writeFailures.Load(),
		LastFailureUnixMilli: a.lastFailureMS.Load(),
	}
}

func (a *AsyncLogStore) Flush() {
	a.once.Do(func() {
		a.mu.Lock()
		close(a.closed)
		close(a.ch)
		a.mu.Unlock()
	})
	a.wg.Wait()
}

func (a *AsyncLogStore) Close() error {
	a.Flush()
	if closer, ok := a.Store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}
