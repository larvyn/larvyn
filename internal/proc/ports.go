package proc

import "sort"

// ListeningPorts lists the TCP ports that each of roots, with its
// children, listens on: the dev server a terminal's npm run dev started.
// ok is false where they cannot be read: ListeningPorts(nil) tells whether
// they can.
func ListeningPorts(roots []int) (ports map[int][]int, ok bool) {
	parent, ok := parents()
	if !ok {
		return nil, false
	}
	// Each process's root: the first of roots up its chain of parents.
	isRoot := map[int]bool{}
	for _, r := range roots {
		if r > 1 {
			isRoot[r] = true
		}
	}
	rootOf := map[int]int{}
	for pid := range parent {
		for p, n := pid, 0; p > 1 && n < 64; p, n = parent[p], n+1 {
			if isRoot[p] {
				rootOf[pid] = p
				break
			}
		}
	}
	for r := range isRoot {
		rootOf[r] = r
	}
	byPid, ok := listening(rootOf)
	if !ok {
		return nil, false
	}
	ports = map[int][]int{}
	seen := map[[2]int]bool{}
	for pid, ps := range byPid {
		r := rootOf[pid]
		for _, p := range ps {
			if !seen[[2]int{r, p}] {
				seen[[2]int{r, p}] = true
				ports[r] = append(ports[r], p)
			}
		}
	}
	for r := range ports {
		sort.Ints(ports[r])
	}
	return ports, true
}
