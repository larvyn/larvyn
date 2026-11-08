package api

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"larvyn/internal/store"
)

// Request is one call. A connection carries one request and its response,
// each a line of JSON.
type Request struct {
	Token  string `json:"token"`
	Caller Caller `json:"caller"`
	// Method and Params: a call by name with a JSON object of params.
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	// Argv: a CLI command as typed ("terminal", "read", "abc", "--lines",
	// "20"), parsed here, where the params' types are known, so the CLI
	// needs no list of commands.
	Argv []string `json:"argv,omitempty"`
	// Help asks for the usage of what Argv names instead of calling it.
	Help bool `json:"help,omitempty"`
}

// Response is a call's result, or why it failed.
type Response struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
	// Usage is set when the command named a group of methods or no method:
	// what the CLI can show instead of a result.
	Usage []Method `json:"usage,omitempty"`
}

// SocketPath is where the app listens. It lives in the state folder, so
// LARVYN_HOME gives a test run its own.
func SocketPath() string { return filepath.Join(store.HomeDir(), "larvyn.sock") }

// TokenPath holds the token a client must send; a new one is written at
// each start.
func TokenPath() string { return filepath.Join(store.HomeDir(), "api.token") }

// maxRequest bounds a request line; params are small.
const maxRequest = 4 << 20

// Server is the listening API.
type Server struct {
	ln    net.Listener
	token string
	wg    sync.WaitGroup
}

// Listen writes a new token and starts serving on the socket. When another
// Larvyn with the same state folder already listens there, it returns an
// error and leaves that one alone.
func Listen() (*Server, error) {
	dir := store.HomeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := SocketPath()
	if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
		c.Close()
		return nil, errors.New("another Larvyn is already listening on " + path)
	}
	// A socket left by a run that crashed.
	_ = os.Remove(path)
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(b)
	if err := writePrivate(TokenPath(), []byte(token)); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// Only this user may connect (unix sockets check the file's mode; on
	// Windows the folder in AppData is the user's own).
	_ = os.Chmod(path, 0o600)
	s := &Server{ln: ln, token: token}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// writePrivate writes a file only its owner can read, replacing any old one.
func writePrivate(path string, data []byte) error {
	tmp := path + ".tmp"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Close stops listening and removes the socket and the token.
func (s *Server) Close() error {
	err := s.ln.Close()
	s.wg.Wait()
	_ = os.Remove(SocketPath())
	_ = os.Remove(TokenPath())
	return err
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go s.handle(c)
	}
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	// Reading the request must not hang a connection forever; the call
	// itself may take long (wait methods), so the deadline is lifted after.
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReaderSize(c, 64<<10)
	line, err := readLine(r, maxRequest)
	if err != nil {
		writeResponse(c, Response{Error: "bad request: " + err.Error()})
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		writeResponse(c, Response{Error: "bad request: " + err.Error()})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
		writeResponse(c, Response{Error: "invalid token: is this the Larvyn that wrote " + TokenPath() + "?"})
		return
	}
	writeResponse(c, Handle(req))
}

// Handle runs a request that passed the token check.
func Handle(req Request) Response {
	var res any
	var err error
	if req.Method != "" {
		res, err = Call(req.Method, req.Caller, req.Params, nil, nil)
	} else {
		words := leadingWords(req.Argv)
		name, left, ok := Resolve(words)
		if ok && req.Help {
			return Response{Usage: []Method{*lookup(name)}}
		}
		if !ok {
			if len(words) == 0 {
				return Response{Usage: Methods()}
			}
			if g := Group(strings.Join(words, ".")); len(g) > 0 {
				return Response{Usage: g}
			}
			return Response{Error: fmt.Sprintf("unknown command %q (larvyn help lists them)", strings.Join(words, " "))}
		}
		// The words Resolve took for the name: "terminal read" or "terminal.read".
		rest := req.Argv[len(words)-len(left):]
		var args []string
		var flags map[string]string
		args, flags, err = parseArgv(lookup(name), rest)
		if err == nil {
			res, err = Call(name, req.Caller, nil, args, flags)
		}
	}
	if err != nil {
		return Response{Error: err.Error()}
	}
	b, err := json.Marshal(res)
	if err != nil {
		return Response{Error: "result: " + err.Error()}
	}
	return Response{Result: b}
}

func writeResponse(c net.Conn, r Response) {
	b, _ := json.Marshal(r)
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	_, _ = c.Write(append(b, '\n'))
}

func readLine(r *bufio.Reader, max int) ([]byte, error) {
	var out []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if len(out) > max {
			return nil, errors.New("request too large")
		}
		if !isPrefix {
			return out, nil
		}
	}
}

// Dial sends one request to the running app, filling in the token, and
// returns its response.
func Dial(req Request) (Response, error) {
	tok, err := os.ReadFile(TokenPath())
	if err != nil {
		return Response{}, errors.New("Larvyn is not running (no " + TokenPath() + ")")
	}
	req.Token = strings.TrimSpace(string(tok))
	c, err := net.DialTimeout("unix", SocketPath(), 3*time.Second)
	if err != nil {
		return Response{}, errors.New("Larvyn is not running: " + err.Error())
	}
	defer c.Close()
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return Response{}, err
	}
	line, err := readLine(bufio.NewReaderSize(c, 64<<10), 64<<20)
	if err != nil {
		return Response{}, fmt.Errorf("no answer from Larvyn: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}
