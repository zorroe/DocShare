//go:build !windows

package config

func protectPassword(password string) (string, error)   { return password, nil }
func unprotectPassword(password string) (string, error) { return password, nil }
