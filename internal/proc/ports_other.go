//go:build !windows

package proc

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// parents maps every process to its parent.
func parents() (map[int]int, bool) {
	out := map[int]int{}
	if runtime.GOOS == "linux" {
		dirs, err := os.ReadDir("/proc")
		if err != nil {
			return nil, false
		}
		for _, d := range dirs {
			pid, err := strconv.Atoi(d.Name())
			if err != nil {
				continue
			}
			b, err := os.ReadFile("/proc/" + d.Name() + "/stat")
			if err != nil {
				continue
			}
			// pid (comm) state ppid …: comm can hold spaces and parens.
			s := string(b)
			i := strings.LastIndexByte(s, ')')
			if i < 0 {
				continue
			}
			f := strings.Fields(s[i+1:])
			if len(f) > 1 {
				out[pid], _ = strconv.Atoi(f[1])
			}
		}
		return out, true
	}
	res, err := quick("ps", "-A", "-o", "pid=,ppid=")
	if err != nil {
		return nil, false
	}
	for _, line := range strings.Split(res, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			pid, _ := strconv.Atoi(f[0])
			ppid, _ := strconv.Atoi(f[1])
			out[pid] = ppid
		}
	}
	return out, true
}

// listening lists the TCP ports the processes of pids listen on.
func listening(pids map[int]int) (map[int][]int, bool) {
	out := map[int][]int{}
	if runtime.GOOS != "linux" {
		// Checked before anything is asked, so ListeningPorts(nil) tells
		// whether ports can be read at all.
		if _, err := exec.LookPath("lsof"); err != nil {
			return nil, false
		}
	}
	if len(pids) == 0 {
		return out, true
	}
	if runtime.GOOS == "linux" {
		// A listening socket's inode in /proc/net/tcp, found among the
		// process's open files as socket:[inode].
		port := map[string]int{}
		for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
			for inode, p := range listenInodes(f) {
				port[inode] = p
			}
		}
		for pid := range pids {
			dir := "/proc/" + strconv.Itoa(pid) + "/fd/"
			fds, _ := os.ReadDir(dir)
			for _, fd := range fds {
				l, err := os.Readlink(dir + fd.Name())
				if err != nil || !strings.HasPrefix(l, "socket:[") {
					continue
				}
				if p, ok := port[strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]")]; ok {
					out[pid] = append(out[pid], p)
				}
			}
		}
		return out, true
	}
	list := make([]string, 0, len(pids))
	for pid := range pids {
		list = append(list, strconv.Itoa(pid))
	}
	res, _ := quick("lsof", "-w", "-nP", "-a", "-iTCP", "-sTCP:LISTEN", "-Fpn", "-p", strings.Join(list, ","))
	pid := 0
	for _, line := range strings.Split(res, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(line[1:])
		case strings.HasPrefix(line, "n") && pid > 0:
			// n*:8000, n127.0.0.1:5173, n[::1]:5173
			if i := strings.LastIndexByte(line, ':'); i >= 0 {
				if p, err := strconv.Atoi(line[i+1:]); err == nil {
					out[pid] = append(out[pid], p)
				}
			}
		}
	}
	return out, true
}

// listenInodes reads a /proc/net/tcp file: the inode and port of each
// socket in the LISTEN state (0A).
func listenInodes(path string) map[string]int {
	out := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // the header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != "0A" {
			continue
		}
		local := fields[1]
		i := strings.LastIndexByte(local, ':')
		if i < 0 {
			continue
		}
		p, err := strconv.ParseInt(local[i+1:], 16, 32)
		if err != nil {
			continue
		}
		out[fields[9]] = int(p)
	}
	return out
}

func quick(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil && len(out) == 0 {
		return "", err
	}
	return string(out), nil
}
