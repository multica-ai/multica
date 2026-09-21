package daemon

type idleSleepAssertion interface {
	Acquire() error
	Release()
}

func (d *Daemon) taskStarted() {
	d.taskActivityMu.Lock()
	defer d.taskActivityMu.Unlock()

	if d.activeTasks.Add(1) != 1 || d.idleSleepAssertion == nil {
		return
	}
	if err := d.idleSleepAssertion.Acquire(); err != nil && d.logger != nil {
		d.logger.Warn("prevent idle system sleep failed", "error", err)
	}
}

func (d *Daemon) taskFinished() {
	d.taskActivityMu.Lock()
	defer d.taskActivityMu.Unlock()

	if d.activeTasks.Add(-1) == 0 && d.idleSleepAssertion != nil {
		d.idleSleepAssertion.Release()
	}
}

func (d *Daemon) releaseIdleSleepAssertion() {
	d.taskActivityMu.Lock()
	defer d.taskActivityMu.Unlock()

	if d.idleSleepAssertion != nil {
		d.idleSleepAssertion.Release()
	}
}
