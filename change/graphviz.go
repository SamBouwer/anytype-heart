//go:build (!linux && !darwin) || android || ios || nographviz || (!arm64 && !amd64)
// +build !linux,!darwin android ios nographviz !arm64,!amd64

package change

import (
	"fmt"

	"github.com/anyproto/anytype-heart/pkg/lib/core"
)

func (t *Tree) Graphviz() (data string, err error) {
	return "", fmt.Errorf("not supported")
}

func CreateSvg(block core.SmartBlock, svgFilename string) (err error) {
	return fmt.Errorf("graphviz is not supported on the current platform")
}
