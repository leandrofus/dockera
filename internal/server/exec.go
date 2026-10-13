package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gorilla/websocket"
)

// The default command picks the best shell available in the container.
var autoShell = []string{"/bin/sh", "-c", `if command -v bash >/dev/null 2>&1; then exec bash; elif command -v ash >/dev/null 2>&1; then exec ash; else exec sh; fi`}

var upgrader = websocket.Upgrader{ReadBufferSize: 32 << 10, WriteBufferSize: 32 << 10} // default CheckOrigin: same host only

type ctrlMsg struct {
	Type string `json:"type"` // resize
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// containerExec bridges a websocket to an interactive `docker exec`.
// Binary frames carry terminal bytes both ways; text frames are JSON control
// messages from the client ({"type":"resize"}) and the final exit status
// from the server ({"type":"exit","code":N}).
func (s *Server) containerExec(w http.ResponseWriter, r *http.Request) {
	c, err := s.ep(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	q := r.URL.Query()
	cmd := autoShell
	if raw := strings.TrimSpace(q.Get("cmd")); raw != "" {
		cmd = []string{"/bin/sh", "-c", raw}
		if q.Get("raw") == "1" { // exec without a shell (distroless)
			cmd = strings.Fields(raw)
		}
	}
	cols, _ := strconv.Atoi(q.Get("cols"))
	rows, _ := strconv.Atoi(q.Get("rows"))
	ctx := r.Context()
	cfg := map[string]any{
		"AttachStdin": true, "AttachStdout": true, "AttachStderr": true, "Tty": true,
		"Cmd": cmd, "Env": []string{"TERM=xterm-256color", "COLORTERM=truecolor"},
	}
	if u := q.Get("user"); u != "" {
		cfg["User"] = u
	}
	if wd := q.Get("workdir"); wd != "" {
		cfg["WorkingDir"] = wd
	}
	if cols > 0 && rows > 0 {
		cfg["ConsoleSize"] = []int{rows, cols}
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.JSON(ctx, http.MethodPost, "/containers/"+url.PathEscape(r.PathValue("id"))+"/exec", nil, cfg, &created); err != nil {
		writeErr(w, err)
		return
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()
	conn, br, err := c.Hijack(ctx, http.MethodPost, "/exec/"+created.ID+"/start", map[string]any{"Detach": false, "Tty": true})
	if err != nil {
		ws.WriteMessage(websocket.BinaryMessage, []byte("\r\n\x1b[31mexec: "+err.Error()+"\x1b[0m\r\n"))
		return
	}
	defer conn.Close()
	resize := func(cols, rows int) {
		if cols > 0 && rows > 0 {
			c.JSON(ctx, http.MethodPost, "/exec/"+created.ID+"/resize", url.Values{"h": {strconv.Itoa(rows)}, "w": {strconv.Itoa(cols)}}, nil, nil)
		}
	}
	resize(cols, rows)

	done := make(chan struct{})
	go func() { // container -> browser
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() { // browser -> container
		for {
			typ, data, err := ws.ReadMessage()
			if err != nil {
				conn.Close()
				return
			}
			if typ == websocket.TextMessage {
				var m ctrlMsg
				if json.Unmarshal(data, &m) == nil && m.Type == "resize" {
					resize(m.Cols, m.Rows)
				}
				continue
			}
			if _, err := conn.Write(data); err != nil {
				return
			}
		}
	}()
	<-done
	var info struct {
		ExitCode int  `json:"ExitCode"`
		Running  bool `json:"Running"`
	}
	c.Get(ctx, "/exec/"+created.ID+"/json", nil, &info)
	msg, _ := json.Marshal(map[string]any{"type": "exit", "code": info.ExitCode})
	ws.WriteMessage(websocket.TextMessage, msg)
	ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, fmt.Sprint(info.ExitCode)))
}
