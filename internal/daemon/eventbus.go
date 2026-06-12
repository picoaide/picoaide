package daemon

import (
  "container/ring"
  "encoding/json"
  "sync"
  "sync/atomic"
  "time"

  "github.com/picoaide/picoaide/internal/daemon/store"
)

type EventBus struct {
  taskID    string
  es        *store.EventStore
  ts        *store.TaskStore
  mu        sync.RWMutex
  seq       atomic.Int64
  ringBuf   *ring.Ring
  subs      map[string]chan *store.Event
  writeCh   chan *store.Event
  done      chan struct{}
  closed    atomic.Bool
}

const ringBufferSize = 1000
const writeChannelCap = 500

func NewEventBus(taskID string, es *store.EventStore, ts *store.TaskStore) *EventBus {
  bus := &EventBus{
    taskID:  taskID,
    es:      es,
    ts:      ts,
    ringBuf: ring.New(ringBufferSize),
    subs:    make(map[string]chan *store.Event),
    writeCh: make(chan *store.Event, writeChannelCap),
    done:    make(chan struct{}),
  }
  go bus.writeLoop()
  return bus
}

func (b *EventBus) Emit(typ string, data json.RawMessage) {
  if b.closed.Load() {
    return
  }
  evt := &store.Event{
    TaskID: b.taskID,
    Seq:    b.seq.Add(1),
    Type:   typ,
    Data:   data,
    Time:   time.Now().UTC().Format(time.RFC3339),
  }

  b.mu.Lock()
  b.ringBuf.Value = evt
  b.ringBuf = b.ringBuf.Next()
  for _, ch := range b.subs {
    select {
    case ch <- evt:
    default:
    }
  }
  b.mu.Unlock()

  select {
  case b.writeCh <- evt:
  default:
  }
}

func (b *EventBus) Subscribe(clientID string) chan *store.Event {
  b.mu.Lock()
  defer b.mu.Unlock()
  ch := make(chan *store.Event, 100)
  b.subs[clientID] = ch
  return ch
}

func (b *EventBus) Unsubscribe(clientID string) {
  b.mu.Lock()
  defer b.mu.Unlock()
  delete(b.subs, clientID)
}

func (b *EventBus) Close() {
  b.closed.Store(true)
  close(b.writeCh)
  <-b.done
  b.es.Close()
  b.mu.Lock()
  for id, ch := range b.subs {
    close(ch)
    delete(b.subs, id)
  }
  b.mu.Unlock()
}

func (b *EventBus) ReplayFromSeq(fromSeq int64) ([]*store.Event, error) {
  return b.es.ReadFromSeq(fromSeq)
}

func (b *EventBus) writeLoop() {
  defer close(b.done)
  ticker := time.NewTicker(200 * time.Millisecond)
  defer ticker.Stop()
  var batch []*store.Event

  for {
    select {
    case evt, ok := <-b.writeCh:
      if !ok {
        if len(batch) > 0 {
          b.persistBatch(batch)
        }
        return
      }
      batch = append(batch, evt)
      halfCap := writeChannelCap / 2
      if len(batch) >= halfCap {
        b.persistBatch(batch)
        batch = nil
      }
    case <-ticker.C:
      if len(batch) > 0 {
        b.persistBatch(batch)
        batch = nil
      }
    }
  }
}

func (b *EventBus) persistBatch(events []*store.Event) {
  for _, evt := range events {
    b.es.Append(evt)
  }
  b.es.Flush()
}
