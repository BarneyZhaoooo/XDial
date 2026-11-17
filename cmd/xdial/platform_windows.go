//go:build windows

package main

// Children launched by this version have an OS-backed owner lifetime. An old
// process whose owner cannot be proven is not ours to kill by name or pattern.
// Recovery of older installations requires an explicit installation transaction.
func killOrphanSingBox(string) {}
