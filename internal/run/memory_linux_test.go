// Copyright ThesmOS B.V. 2026
// SPDX-License-Identifier: MIT

package run_test

import (
	"testing"

	"go.dokimi.dev/mutate/internal/run"
)

func TestMemory(t *testing.T) {
	t.Parallel()
	t.Run("Run", func(t *testing.T) {
		t.Parallel()
		t.Run(
			"gives exhausted to a mutant whose run crosses the memory ceiling, with the running tests",
			func(t *testing.T) {
				t.Parallel()
				// TestGrow sleeps half a second in the control runs, so the
				// deadline of a mutant's run, above 7 seconds, leaves the mutants
				// that grow without end the time to cross the memory ceiling first,
				// also when other test binaries load the machine.
				dir := module(t, map[string]string{
					"grow.go": "package fixture\n\nfunc Grow(n int) int {\n\tvar buf []byte\n\tfor i := 0; i != n; i++ {\n" +
						"\t\tbuf = append(buf, make([]byte, 1<<20)...)\n\t}\n\treturn len(buf)\n}\n",
					"grow_test.go": "package fixture\n\nimport (\n\t\"os\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestGrow(t *testing.T) {\n" +
						"\tif os.Getenv(\"DOKIMI_MUTATE_MUTANT\") == \"0\" {\n\t\ttime.Sleep(500 * time.Millisecond)\n\t}\n" +
						"\tif Grow(3) != 3<<20 {\n\t\tt.Error(\"Grow(3) != 3 MiB\")\n\t}\n}\n",
				})
				rec := runIn(t, dir, run.Config{Workers: 2})
				want(t, verdicts(rec), `Grow sbr-delete 0: killed [TestGrow]
Grow ror-true 0: exhausted [TestGrow]
Grow ror-false 0: killed [TestGrow]
Grow uoi-incdec 0: exhausted [TestGrow]
Grow sbr-delete 1: killed [TestGrow]
Grow sbr-zero 0: killed [TestGrow]
`)
			},
		)
	})
}
