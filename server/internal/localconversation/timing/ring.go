package timing

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultRingSize = 2000

// Ring keeps the newest records in memory for a device with no log file to read
// them from: a phone, whose app drains it with a cursor. Saving carries the
// records and the cursor's numbering across a stopped link, since iOS closes the
// link in the background while the app cannot drain it. The app's cursor does not
// survive a new process, so what it has reported is acknowledged and forgotten here;
// a cursor of 0 then starts after it.
type Ring struct {
	path string
	size int

	mu      sync.Mutex
	records []Record
	next    int64
	dropped int64
}

type ringFile struct {
	Records []Record `json:"records"`
	Next    int64    `json:"next"`
	Dropped int64    `json:"dropped"`
}

// OpenRing returns a ring of size records (0 uses the default) saved at path,
// resuming what an earlier one saved. An unreadable file starts empty.
func OpenRing(path string, size int) *Ring {
	if size <= 0 {
		size = defaultRingSize
	}
	r := &Ring{path: path, size: size}
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	var saved ringFile
	if json.Unmarshal(data, &saved) != nil || saved.Next < int64(len(saved.Records)) {
		return r
	}
	for _, record := range saved.Records {
		if clean, ok := Sanitize(record); ok {
			r.records = append(r.records, clean)
		} else {
			r.dropped++
		}
	}
	r.next, r.dropped = saved.Next, r.dropped+saved.Dropped
	r.trim()
	return r
}

// Write adds r, dropping the oldest record when full.
func (r *Ring) Write(record Record) {
	if r == nil {
		return
	}
	record.Version = Version
	if record.Source == "" {
		record.Source = SourceMobile
	}
	if record.At == "" {
		record.At = formatAt(time.Now())
	}
	clean, ok := Sanitize(record)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, clean)
	r.next++
	r.trim()
}

func (r *Ring) trim() {
	if extra := len(r.records) - r.size; extra > 0 {
		r.dropped += int64(extra)
		r.records = append(r.records[:0:0], r.records[extra:]...)
	}
}

// After returns up to limit records written after cursor and the cursor to pass
// next. A cursor from ahead of this ring, such as after a lost file, reads from
// the start. Dropped is how many records were lost before they could be read.
func (r *Ring) After(cursor int64, limit int) (records []Record, next, dropped int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cursor < 0 || cursor > r.next {
		cursor = 0
	}
	first := r.next - int64(len(r.records))
	if cursor > first {
		records = r.records[cursor-first:]
	} else {
		records = r.records
		cursor = first
	}
	if limit > 0 && len(records) > limit {
		records = records[:limit]
	}
	records = append([]Record(nil), records...)
	return records, cursor + int64(len(records)), r.dropped
}

// Acknowledge forgets the records before cursor and dropped of the recorded losses,
// which the app has reported, and saves at once since the app may be killed before
// the next Close. A cursor from ahead of this ring forgets no record.
func (r *Ring) Acknowledge(cursor, dropped int64) {
	if r == nil {
		return
	}
	r.mu.Lock()
	if first := r.next - int64(len(r.records)); cursor > first && cursor <= r.next {
		r.records = append(r.records[:0:0], r.records[cursor-first:]...)
	}
	r.dropped -= min(max(dropped, 0), r.dropped)
	r.mu.Unlock()
	r.Save()
}

// Save writes the ring where OpenRing finds it. Diagnostics never fail a caller,
// so a write that fails only loses these records.
func (r *Ring) Save() {
	if r == nil || r.path == "" {
		return
	}
	r.mu.Lock()
	data, err := json.Marshal(ringFile{Records: r.records, Next: r.next, Dropped: r.dropped})
	r.mu.Unlock()
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(r.path), 0o700) != nil {
		return
	}
	tmp := r.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, r.path)
	}
}
