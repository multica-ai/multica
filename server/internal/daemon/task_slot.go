package daemon

import "context"

// taskSlotLease belongs to one task goroutine. A parked task keeps its task
// ownership but returns execution capacity, then may resume on a different index.
type taskSlotLease struct {
	slots  chan int
	wakeup chan struct{}
	index  int
	held   bool
}

func newTaskSlotLease(slots chan int, index int, wakeup chan struct{}) *taskSlotLease {
	return &taskSlotLease{slots: slots, wakeup: wakeup, index: index, held: true}
}

func (s *taskSlotLease) release() {
	if !s.held {
		return
	}
	s.held = false
	s.slots <- s.index
	signalPollerWakeup(s.wakeup)
}

func (s *taskSlotLease) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.held {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case index := <-s.slots:
		s.index = index
		s.held = true
		if err := ctx.Err(); err != nil {
			s.release()
			return err
		}
		return nil
	}
}
