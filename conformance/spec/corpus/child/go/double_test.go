package fixture

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The variable that makes the test binary the child of TestDouble, and the
// prefix of the variables of a mutation run.
const (
	childVar  = "FIXTURE_CHILD"
	runPrefix = "DOKIMI_MUTATE_"
)

// TestMain prints double(21) and exits when the test binary runs as the
// child of TestDouble, and runs the tests otherwise.
func TestMain(m *testing.M) {
	if os.Getenv(childVar) == "1" {
		fmt.Print(double(21))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestDouble runs the test binary as a child whose environment contains
// childVar and each variable of the mutation run, and no other, as a sandbox
// starts its first process. It checks that the child prints 42.
func TestDouble(t *testing.T) {
	env := []string{childVar + "=1"}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, runPrefix) {
			env = append(env, kv)
		}
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "42" {
		t.Fatalf("the child printed %q, not 42", out)
	}
}
