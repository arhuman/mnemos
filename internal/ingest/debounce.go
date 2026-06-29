package ingest

import (
	"sync"
	"time"
)

// debouncer coalesces rapid events keyed by path into a single delayed callback.
// Each call to trigger (re)starts a per-key timer; the callback fires only once
// the key has been quiet for the configured delay. This absorbs the burst of
// filesystem events an editor's atomic save produces (write to temp, rename over
// the target → Create/Rename/Write in quick succession) into one reindex.
//
// The callback runs on its own goroutine. The zero value is not usable; build
// one with newDebouncer.
type debouncer struct {
	delay  time.Duration
	mu     sync.Mutex
	timers map[string]*entry
	wg     sync.WaitGroup
	closed bool
}

// entry is one key's live timer. gen disambiguates a fired callback from a newer
// reset: a callback runs only when its captured generation still matches the
// entry's current generation. cancelled marks an entry whose count Stop already
// released so the callback must not release it again.
type entry struct {
	timer     *time.Timer
	gen       uint64
	cancelled bool
}

// newDebouncer returns a debouncer that fires a key's callback after delay of
// quiet time.
func newDebouncer(delay time.Duration) *debouncer {
	return &debouncer{
		delay:  delay,
		timers: make(map[string]*entry),
	}
}

// trigger schedules fn to run after the debounce delay for key. A second trigger
// on the same key before the delay elapses resets the timer, so only the last
// trigger's fn runs — one callback per quiet burst. After Stop, trigger is a
// no-op so late events cannot resurrect a shutting-down watcher.
//
// The WaitGroup holds exactly one count per key that has a live entry. The count
// is released exactly once: by the callback that wins (its generation matches)
// or by Stop when it cancels the entry. Resetting reuses the same entry and
// count, so the counter never leaks.
func (d *debouncer) trigger(key string, fn func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}

	e, ok := d.timers[key]
	if !ok {
		e = &entry{}
		d.timers[key] = e
		d.wg.Add(1)
	} else {
		e.timer.Stop()
		e.gen++
	}

	gen := e.gen
	e.timer = time.AfterFunc(d.delay, func() { d.fire(key, gen, fn) })
}

// fire runs the callback for key if it is still the current generation and was
// not cancelled by Stop. It releases the key's wg count exactly once.
func (d *debouncer) fire(key string, gen uint64, fn func()) {
	d.mu.Lock()
	e, ok := d.timers[key]
	if !ok || e.gen != gen || e.cancelled {
		d.mu.Unlock()

		return
	}
	delete(d.timers, key)
	d.mu.Unlock()

	defer d.wg.Done()
	fn()
}

// Stop disables further triggers, cancels every pending timer, releases their
// outstanding counts, and waits for any callback already running to return. It
// is safe to call once during shutdown.
func (d *debouncer) Stop() {
	d.mu.Lock()
	d.closed = true
	for key, e := range d.timers {
		e.timer.Stop()
		e.cancelled = true
		delete(d.timers, key)
		d.wg.Done()
	}
	d.mu.Unlock()

	d.wg.Wait()
}
