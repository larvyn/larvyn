//go:build windows

package proc

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// parents maps every process to its parent, from a Toolhelp snapshot.
func parents() (map[int]int, bool) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, false
	}
	defer windows.CloseHandle(snap)
	out := map[int]int{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[int(e.ProcessID)] = int(e.ParentProcessID)
	}
	return out, true
}

var getExtendedTcpTable = syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const (
	afInet                    = 2
	afInet6                   = 23
	tcpTableOwnerPidListener  = 3
	errorInsufficientBuffer   = 122
	tcpRowOwnerPidSize        = 24 // MIB_TCPROW_OWNER_PID: six DWORDs
	tcp6RowOwnerPidSize       = 56 // MIB_TCP6ROW_OWNER_PID
	tcp6RowPortOffset         = 20 // after ucLocalAddr[16] and dwLocalScopeId
	tcp6RowPidOffset          = 52
	tcpRowPortOffset          = 8 // after dwState and dwLocalAddr
	tcpRowPidOffset           = 20
	maxTcpTableTries          = 4
	tcpTableNumEntriesOffset  = 0
	tcpTableFirstRowOffset    = 4
	tcpTableInitialBufferSize = 16 << 10
)

// listening lists the TCP ports the processes of pids listen on, from the
// listener tables with their owning process, IPv4 and IPv6.
func listening(pids map[int]int) (map[int][]int, bool) {
	if getExtendedTcpTable.Find() != nil {
		return nil, false
	}
	out := map[int][]int{}
	for _, t := range []struct {
		af, size, portAt, pidAt uintptr
	}{
		{afInet, tcpRowOwnerPidSize, tcpRowPortOffset, tcpRowPidOffset},
		{afInet6, tcp6RowOwnerPidSize, tcp6RowPortOffset, tcp6RowPidOffset},
	} {
		buf, ok := tcpTable(t.af)
		if !ok {
			return nil, false
		}
		if len(buf) < tcpTableFirstRowOffset {
			continue
		}
		n := uintptr(*(*uint32)(unsafe.Pointer(&buf[tcpTableNumEntriesOffset])))
		for i := uintptr(0); i < n; i++ {
			row := tcpTableFirstRowOffset + i*t.size
			if row+t.size > uintptr(len(buf)) {
				break
			}
			pid := int(*(*uint32)(unsafe.Pointer(&buf[row+t.pidAt])))
			if _, mine := pids[pid]; !mine {
				continue
			}
			// The port is in network byte order in the DWORD's low 16 bits.
			raw := *(*uint32)(unsafe.Pointer(&buf[row+t.portAt]))
			out[pid] = append(out[pid], int(raw&0xff)<<8|int(raw>>8&0xff))
		}
	}
	return out, true
}

// tcpTable reads one address family's listener table, growing the buffer
// while sockets open between the size query and the read.
func tcpTable(af uintptr) ([]byte, bool) {
	size := uint32(tcpTableInitialBufferSize)
	for range maxTcpTableTries {
		buf := make([]byte, size)
		r, _, _ := getExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, af, tcpTableOwnerPidListener, 0)
		switch r {
		case 0:
			return buf[:size], true
		case errorInsufficientBuffer:
			continue
		default:
			return nil, false
		}
	}
	return nil, false
}
