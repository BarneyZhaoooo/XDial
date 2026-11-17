//go:build windows

package libbox

import "os"

func openRuleSetFile(path string) (*os.File, error) { return os.Open(path) }
