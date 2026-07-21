package cmd

import (
	"bufio"
	"strings"
	"testing"
)

func TestPromptYNReadsSequentialAnswers(t *testing.T) {
	scanner := bufio.NewScanner(strings.NewReader("yes\ny\nno\n"))

	if !promptYN(scanner, "") {
		t.Fatal("first answer = false, want true")
	}
	if !promptYN(scanner, "") {
		t.Fatal("second answer = false, want true")
	}
	if promptYN(scanner, "") {
		t.Fatal("third answer = true, want false")
	}
}
