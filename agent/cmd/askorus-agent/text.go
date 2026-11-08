package main

import (
	"errors"
	"fmt"
	"io"
)

// Terminal text on the PC is shown in English and Korean together: the
// agent has no language setting, and the person at the PC may read either.
// The JSON log stays English only.

// msg is one message in both languages.
type msg struct{ en, ko string }

func m(en, ko string, args ...any) msg {
	return msg{fmt.Sprintf(en, args...), fmt.Sprintf(ko, args...)}
}

func (x msg) String() string { return x.en + "\n" + x.ko }

// say prints the message, one line per language.
func say(w io.Writer, x msg) { fmt.Fprintln(w, x.String()) }

// biError is an error with a Korean line.
type biError struct{ msg }

func (e biError) Error() string { return e.String() }

func fail(en, ko string, args ...any) error { return biError{m(en, ko, args...)} }

// printError writes "error: …" and "오류: …".
func printError(w io.Writer, err error) {
	var be biError
	if errors.As(err, &be) {
		fmt.Fprintf(w, "error: %s\n오류: %s\n", be.en, be.ko)
		return
	}
	fmt.Fprintf(w, "error / 오류: %v\n", err)
}
