package cmd

import (
	"errors"
	"testing"
)

func TestIsAlreadyStopped(t *testing.T) {
	trueErrs := []string{
		"container is not running",
		"is not running",
		"No such container: omnideck",
	}
	for _, msg := range trueErrs {
		if !isAlreadyStopped(errors.New(msg)) {
			t.Errorf("isAlreadyStopped(%q): expected true", msg)
		}
	}

	falseErrs := []string{
		"permission denied",
		"connection refused",
		"timeout",
	}
	for _, msg := range falseErrs {
		if isAlreadyStopped(errors.New(msg)) {
			t.Errorf("isAlreadyStopped(%q): expected false", msg)
		}
	}
}

func TestIsNotFound(t *testing.T) {
	if !isNotFound(errors.New("No such container: omnideck")) {
		t.Error("expected true for 'No such container' error")
	}
	if isNotFound(errors.New("permission denied")) {
		t.Error("expected false for unrelated error")
	}
}
