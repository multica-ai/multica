package daemon

import (
	"context"
	"fmt"
	"time"
)

// freeDiskBytesFunc is swapped in tests so the pressure guard can be exercised
// without depending on the host filesystem state.
var freeDiskBytesFunc = freeDiskBytes

func (d *Daemon) diskPressureLoop(ctx context.Context) {
	if d.cfg.DiskPressureCheckInterval <= 0 || d.cfg.DiskPressureMinFreeBytes <= 0 {
		d.logger.Info("disk-pressure: disabled")
		return
	}
	d.logger.Info("disk-pressure: started",
		"interval", d.cfg.DiskPressureCheckInterval,
		"min_free_bytes", d.cfg.DiskPressureMinFreeBytes,
	)

	if err := d.guardDiskPressure(ctx, false); err != nil {
		d.logger.Warn("disk-pressure: initial check failed", "error", err)
	}

	ticker := time.NewTicker(d.cfg.DiskPressureCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.guardDiskPressure(ctx, false); err != nil {
				d.logger.Warn("disk-pressure: check failed", "error", err)
			}
		}
	}
}

func (d *Daemon) guardDiskPressure(ctx context.Context, failClosed bool) error {
	if d.cfg.DiskPressureMinFreeBytes <= 0 {
		return nil
	}
	freeBytes, totalBytes, err := freeDiskBytesFunc(d.cfg.WorkspacesRoot)
	if err != nil {
		d.logger.Warn("disk-pressure: measurement unavailable", "error", err)
		return nil
	}
	if freeBytes >= d.cfg.DiskPressureMinFreeBytes {
		return nil
	}

	d.logger.Warn("disk-pressure: free space below threshold; running GC",
		"free_bytes", freeBytes,
		"total_bytes", totalBytes,
		"threshold_bytes", d.cfg.DiskPressureMinFreeBytes,
	)
	d.runGC(ctx)

	recheckFree, recheckTotal, err := freeDiskBytesFunc(d.cfg.WorkspacesRoot)
	if err != nil {
		d.logger.Warn("disk-pressure: remeasure unavailable", "error", err)
		return nil
	}
	if recheckFree >= d.cfg.DiskPressureMinFreeBytes {
		d.logger.Info("disk-pressure: GC recovered enough space",
			"free_bytes", recheckFree,
			"total_bytes", recheckTotal,
			"threshold_bytes", d.cfg.DiskPressureMinFreeBytes,
		)
		return nil
	}

	err = fmt.Errorf("disk pressure still below threshold after GC: free_bytes=%d threshold_bytes=%d", recheckFree, d.cfg.DiskPressureMinFreeBytes)
	if failClosed {
		return err
	}
	d.logger.Error("disk-pressure: unable to recover enough space",
		"free_bytes", recheckFree,
		"total_bytes", recheckTotal,
		"threshold_bytes", d.cfg.DiskPressureMinFreeBytes,
	)
	return err
}
