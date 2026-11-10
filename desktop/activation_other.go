//go:build !darwin

package main

// setDockActivationPolicy is a no-op outside macOS.
func setDockActivationPolicy(bool) {}
