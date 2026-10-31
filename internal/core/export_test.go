package core

// SetBetweenReads sets the seam that TaskCurrent calls between its reads. A nil
// f clears it. It is for tests only (see Local.betweenReads).
func (l *Local) SetBetweenReads(f func()) { l.betweenReads = f }
