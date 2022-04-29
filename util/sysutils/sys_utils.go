package sysutils

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

type Closer interface {
	Close()
}

// CurrentDirectory 获取程序运行路径
func CurrentDirectory() string {
	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		fmt.Errorf(err.Error())
	}
	return strings.Replace(dir, "\\", "/", -1)
}

func WaitCloseSignals() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	<-signals
}

func WaitCloseSignalsAndRelease(closer Closer) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGQUIT, syscall.SIGTERM, syscall.SIGINT)
	<-signals
	closer.Close()
}

func Exit(code int, msg string) {
	if code != 0 {
		if msg != "" {
			fmt.Fprintf(os.Stderr, "%s\n", msg)
		}
		os.Exit(code)
	}

	if msg != "" {
		fmt.Fprintf(os.Stdout, "%v\n", msg)
	}
	os.Exit(0)
}
