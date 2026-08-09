package protocol

// RuntimeRegistration is one Runtime advertised by a daemon registration.
// Capabilities is a pointer so legacy omission remains distinguishable from
// an upgraded daemon that explicitly advertises an empty capability set.
type RuntimeRegistration struct {
	Name         string    `json:"name"`
	Type         string    `json:"type"`
	Version      string    `json:"version"`
	Status       string    `json:"status"`
	ProfileID    string    `json:"profile_id,omitempty"`
	Capabilities *[]string `json:"capabilities,omitempty"`
}
