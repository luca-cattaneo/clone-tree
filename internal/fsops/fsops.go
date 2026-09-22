package fsops

import (
	"fmt"
	"os"
)

func warnMissing(src string) {
	_, _ = fmt.Fprintf(os.Stderr, "ct: warning: %s does not exist, skipping\n", src)
}
