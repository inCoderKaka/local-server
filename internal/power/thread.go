package power

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// Windows execution-state flags for SetThreadExecutionState.
const (
	esContinuous     uintptr = 0x80000000
	esSystemRequired uintptr = 0x00000001
)

// stateSetter calls SetThreadExecutionState(flags) and returns an error when it fails. On
// Windows it is the kernel32 call; tests pass a fake.
type stateSetter func(flags uintptr) error

// threadPower is the Power of an OS whose sleep lock is a flag on one OS thread (Windows). The
// flag belongs to the thread that set it, and a goroutine may move between threads, so each lock
// owns a goroutine pinned to its thread for as long as the lock is held.
type threadPower struct {
	set stateSetter
}

func newThreadPower(set stateSetter) *threadPower { return &threadPower{set: set} }

func (p *threadPower) Acquire(context.Context) (Lock, error) {
	l := &threadLock{release: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		defer close(l.done)
		runtime.LockOSThread()
		// The goroutine exits still locked, which ends the thread, and with it the execution
		// state if the clear below ever failed.
		if err := p.set(esContinuous | esSystemRequired); err != nil {
			started <- err
			return
		}
		started <- nil
		<-l.release
		_ = p.set(esContinuous)
	}()
	if err := <-started; err != nil {
		<-l.done
		return nil, fmt.Errorf("power: SetThreadExecutionState: %w", err)
	}
	return l, nil
}

type threadLock struct {
	release chan struct{}
	done    chan struct{}
	once    sync.Once
}

// Release clears the execution state on the owning thread and waits for it to end.
func (l *threadLock) Release() error {
	l.once.Do(func() { close(l.release) })
	<-l.done
	return nil
}

func (l *threadLock) Alive() bool {
	select {
	case <-l.release:
		return false
	default:
		return true
	}
}
