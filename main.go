package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
)

var (
	flagConfig string
	flagListen []string
	flagRemote []string
	flagHelp   bool
	flagPProf  bool
	flagCheck  bool

	config Config
)

const helpMessage = `Usage:
	traffics -l [listen] -r [remote] -c [config] -h
Options:
	-l [listen] : set a listen configuration
	-r [remote] : set a remote configuration
	-c [config] : set the config file path
	--check : check config only (dry-run)
	-h/--help : print help message

Example:
	# Start a forward server from local 9500 to 1.2.3.4:48000
	traffics -l "tcp+udp://:9500?remote=example" -r "example://1.2.3.4:48000"
	
	# start from a config file (this will ignore the command line options like -l and -r)
	traffics -c config.json

See README.md to get full documentation.
`

func main() {
	standardLogger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	err := parseFlags()
	if err != nil {
		standardLogger.Error("parse option failed", slog.String("error", err.Error()))
		return
	}
	if flagHelp || (len(flagListen) == 0 && len(flagRemote) == 0 && flagConfig == "") {
		fmt.Print(helpMessage)
		return
	}
	if err := initConfig(); err != nil {
		standardLogger.Error("parse config and command line flags failed", slog.String("error", err.Error()))
		return
	}

	debug.FreeOSMemory()
	runtime.GC()
	// pprof
	if flagPProf && !flagCheck {
		go func() {
			llen, err := net.Listen("tcp", ":0")
			if err != nil {
				standardLogger.Error("start pprof listener failed", slog.String("error", err.Error()))
				os.Exit(1)
			}
			standardLogger.Info("new pprof server started", slog.String("address", llen.Addr().String()))
			ppe := http.Serve(llen, nil)
			if err != nil {
				standardLogger.Error("start pprof failed", slog.String("error", ppe.Error()))
			}
			os.Exit(0)
		}()
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tf, err := NewTraffics(config)
	if err != nil {
		standardLogger.Error("create new traffics failed", slog.String("error", err.Error()))
		return
	}
	if flagCheck {
		return
	}

	err = tf.Start(rootCtx)
	if err != nil {
		standardLogger.Error("start traffics failed", slog.String("error", err.Error()))
		return
	}
	ch := make(chan os.Signal)
	signal.Notify(ch, unix.SIGINT, os.Interrupt, unix.SIGSTOP, unix.SIGKILL, unix.SIGTERM)

	<-ch
	cancel()
	tf.Close()
}

func initConfig() error {
	var internalConfig = NewConfig()

	if flagConfig != "" {
		var (
			bs  []byte
			err error
		)
		if flagConfig == "-" {
			bs, err = io.ReadAll(os.Stdin)
		} else {
			bs, err = os.ReadFile(flagConfig)
		}

		if err != nil {
			return fmt.Errorf("read config file failed: %w", err)
		}

		err = json.Unmarshal(bs, &internalConfig)
		if err != nil {
			return fmt.Errorf("parse config file failed: %w", err)
		}
	}

	for _, k := range flagListen {
		bind := NewDefaultBind()
		if err := bind.Parse(k); err != nil {
			return fmt.Errorf("parse '%s' failed: %w", k, err)
		}
		internalConfig.Binds = append(internalConfig.Binds, bind)
	}
	for _, k := range flagRemote {
		remote := NewDefaultRemote()
		if err := remote.Parse(k); err != nil {
			return fmt.Errorf("parse '%s' failed: %w", k, err)
		}
		internalConfig.Remote = append(internalConfig.Remote, remote)
	}

	if len(internalConfig.Binds) == 0 || len(internalConfig.Remote) == 0 {
		return errors.New("no available bind/remote found")
	}

	config = internalConfig

	return nil
}

func parseFlags() error {
	args := os.Args[1:]
	i := 0

	var requireValue = func() string {
		i++
		if i >= len(args) {
			return ""
		}
		return args[i]
	}

	for ; i < len(args); i++ {
		var key = args[i]
		var value string
		switch key {
		case "-l", "-r", "-c":
			value = requireValue()
			if value == "" {
				return fmt.Errorf("%s option required at least one value after", key)
			}
			break
		case "--help", "-h":
			flagHelp = true
			return nil // returned
		case "--pprof":
			flagPProf = true
			continue
		case "--check":
			flagCheck = true
			continue
		default:
			return fmt.Errorf("unknwon option %s", key)
		}

		switch key {
		case "-r":
			flagRemote = append(flagRemote, value)
			break
		case "-l":
			flagListen = append(flagListen, value)
			break
		case "-c":
			flagConfig = value
			break
		}

	}
	return nil
}
