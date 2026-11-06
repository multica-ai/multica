package agent

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestProbePiIdentitySignalling(t *testing.T) {
	for _, result := range []error{syscall.ESRCH, syscall.EINVAL, syscall.ENOSYS, syscall.EPERM, nil} {
		t.Run(fmt.Sprint(result), func(t *testing.T) {
			calls := 0
			err := probePiIdentitySignalling(func(pid int, version uint32, sig syscall.Signal) error {
				calls++
				if pid != 1<<31-1 || version != 0 || sig != syscall.SIGKILL {
					t.Fatalf("unsafe probe arguments: pid=%d version=%d signal=%v", pid, version, sig)
				}
				return result
			})
			if calls != 1 || (err == nil) != errors.Is(result, syscall.ESRCH) {
				t.Fatalf("result=%v: calls=%d error=%v", result, calls, err)
			}
			if result != nil && !errors.Is(result, syscall.ESRCH) && !errors.Is(err, result) {
				t.Fatalf("lost kernel error: %v", err)
			}
		})
	}
}
