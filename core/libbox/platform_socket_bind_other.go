//go:build !darwin && !ios && (!linux || android)

package libbox

import "fmt"

func bindSocketToInterface(_ int, _ int) error {
	return fmt.Errorf("platform: Underlay socket binding is unavailable")
}
