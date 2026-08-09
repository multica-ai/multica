// fake-tui is a deterministic ANSI terminal fixture for manual and automated
// PTY tests. It never launches an ambient agent CLI.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/creack/pty"
)

func main() {
	code := run()
	fmt.Print("\x1b[?1049l")
	os.Exit(code)
}

func run() int {
	fmt.Print("\x1b[?1049h\x1b[2J\x1b[H\x1b[1;36mMultica fake TUI\x1b[0m\r\n")
	fmt.Print("\x1b[3;1HType text, 'burst', 'size', 'sleep', 'fail', or 'exit'. Ctrl+C is reported.\r\n> ")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGWINCH)
	defer signal.Stop(sig)
	go func() {
		for received := range sig {
			if received == syscall.SIGWINCH {
				printSize()
				continue
			}
			fmt.Print("\r\n\x1b[33minterrupt:^C\x1b[0m\r\n> ")
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "exit":
			fmt.Print("bye\r\n")
			return 0
		case "fail":
			fmt.Print("intentional failure\r\n")
			return 23
		case "burst":
			for i := 0; i < 4096; i++ {
				fmt.Printf("\x1b[3%dm%04d fake output\x1b[0m\r\n", i%8, i)
			}
		case "sleep":
			fmt.Print("sleeping\r\n")
			time.Sleep(30 * time.Second)
		case "size":
			printSize()
		default:
			fmt.Printf("echo: %s\r\n", line)
		}
		fmt.Print("> ")
	}
	if err := scanner.Err(); err != nil && !errorsIsEIO(err) {
		fmt.Fprintf(os.Stderr, "fake-tui: %v\r\n", err)
		return 1
	}
	return 0
}

func printSize() {
	size, err := pty.GetsizeFull(os.Stdout)
	if err != nil {
		fmt.Print("\r\nsize:unknown\r\n> ")
		return
	}
	fmt.Print("\r\nsize:" + strconv.Itoa(int(size.Cols)) + "x" + strconv.Itoa(int(size.Rows)) + "\r\n> ")
}

// PTY masters commonly report EIO when their slave closes. Avoid importing
// platform-specific errno wrappers into this tiny fixture.
func errorsIsEIO(err error) bool {
	return err == syscall.EIO
}
