// Host-only byte bridge to the single synthetic Docker candidate. Docker25 does
// not publish an internal-only bridge's ports. This preserves the container's
// lack of external networking and streams actual HTTP/SSE without rewriting it.
package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

func main() {
	container := flag.String("container", "", "exact synthetic container ID")
	port := flag.Int("port", 0, "127.0.0.1 listen port")
	logPath := flag.String("log", "", "private metadata-only log")
	flag.Parse()
	var logger io.Writer = os.Stdout
	if *logPath != "" {
		file, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			panic(err)
		}
		defer func() { _ = file.Close() }()
		logger = file
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(*container) || *port < 1024 || *port > 65535 {
		panic("invalid isolated bridge target")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(*port))
	if err != nil {
		panic(err)
	}
	defer func() { _ = listener.Close() }()
	_, _ = fmt.Fprintf(logger, "synthetic loopback byte bridge ready port=%d container=%s\n", *port, *container)
	slots := make(chan struct{}, 64)
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			go func() {
				defer func() { <-slots }()
				defer func() { _ = connection.Close() }()
				command := exec.Command(`C:\Program Files\Docker\Docker\resources\bin\docker.exe`, "--host", "npipe:////./pipe/docker_engine", "exec", "--interactive", *container, "nc", "-w", "60", "127.0.0.1", "8080")
				in, err := command.StdinPipe()
				if err != nil {
					return
				}
				out, err := command.StdoutPipe()
				if err != nil {
					return
				}
				command.Stderr = io.Discard
				if command.Start() != nil {
					return
				}
				go func() { _, _ = io.Copy(in, connection); _ = in.Close() }()
				_, _ = io.Copy(connection, out)
				_ = connection.Close()
				_ = command.Wait()
			}()
		default:
			_ = connection.Close()
			_, _ = fmt.Fprintln(logger, "synthetic loopback capacity reached")
		}
	}
}
