package agentconfig

import "fmt"

const (
	DefaultMaxConcurrentTasks int32 = 6
	MinMaxConcurrentTasks     int32 = 1
	MaxMaxConcurrentTasks     int32 = 50
)

func IsValidMaxConcurrentTasks(value int32) bool {
	return value >= MinMaxConcurrentTasks && value <= MaxMaxConcurrentTasks
}

func ValidateMaxConcurrentTasks(value int32) error {
	if !IsValidMaxConcurrentTasks(value) {
		return fmt.Errorf(
			"must be between %d and %d",
			MinMaxConcurrentTasks,
			MaxMaxConcurrentTasks,
		)
	}
	return nil
}
