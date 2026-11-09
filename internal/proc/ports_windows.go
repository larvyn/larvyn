//go:build windows

package proc

// ListeningPorts lists the TCP ports processes listen on; not on Windows
// yet.
func ListeningPorts(roots []int) (map[int][]int, bool) { return nil, false }
