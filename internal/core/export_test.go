package core

import "time"

// SetBetweenReads sets the seam that TaskCurrent calls between its reads. A nil
// f clears it. It is for tests only (see Local.betweenReads).
func (l *Local) SetBetweenReads(f func()) { l.betweenReads = f }

// SetClock sets the seam that now reads the server's time from. A nil f restores
// the wall clock. It is for tests only (see Local.clock).
func (l *Local) SetClock(f func() time.Time) { l.clock = f }
