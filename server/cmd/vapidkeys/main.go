// vapidkeys mints a Web Push VAPID key pair and prints it as env-var lines.
//
// Run via: make vapid-keys
package main

import (
	"fmt"
	"os"

	"github.com/multica-ai/multica/server/internal/service"
)

func main() {
	priv, pub, err := service.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate VAPID keys: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", priv)
	fmt.Println("# Paste the lines above into your .env file (or set in your deploy environment).")
	fmt.Println("# Optionally also set VAPID_SUBJECT=mailto:you@example.com")
}
