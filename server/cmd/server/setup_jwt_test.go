package main

import "os"

func init() {
	if os.Getenv("JWT_SECRET") == "" || len(os.Getenv("JWT_SECRET")) < 32 {
		os.Setenv("JWT_SECRET", "test-jwt-secret-padding-padding-padding-padding")
	}
}
