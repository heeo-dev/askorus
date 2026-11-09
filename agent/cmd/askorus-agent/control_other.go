//go:build !windows

package main

import "errors"

// macOS (LaunchAgent) and Linux (systemd --user) come with the agents for
// those systems (plan step D).

var errNotYet = errors.New("only supported on Windows for now")

func listenStop(int) <-chan struct{}                              { return make(chan struct{}) }
func signalStop(int) error                                        { return errNotYet }
func agentOnPort(int) bool                                        { return false }
func startDetached(string, []string, string, string) (int, error) { return 0, errNotYet }
func autostartCommand(exe string, _ bool) string                  { return exe + " start" }
func autostartSet(string, bool) error                             { return errNotYet }
func autostartRemove() error                                      { return nil }
func autostartGet() string                                        { return "" }
func userPathHas(string) bool                                     { return false }
func userPathAdd(string) (bool, error)                            { return false, errNotYet }
func userPathRemove(string) (bool, error)                         { return false, nil }
func keepAwake(bool)                                              {}
func sleepAfterAC() int                                           { return -1 }
func controlSupported() bool                                      { return false }
func processRunning(int) bool                                     { return false }
func holdConsole() (restore func())                               { return func() {} } // QuickEdit is a Windows console thing (#37)
