package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/fukata/golang-stats-api-handler"
	"github.com/jessevdk/go-flags"
	"github.com/kazeburo/chocon/proxy"
	"github.com/kazeburo/chocon/upstream"
	"github.com/lestrrat-go/apache-logformat"
	"github.com/lestrrat-go/file-rotatelogs"
	"github.com/lestrrat/go-server-starter-listener"
	statsHTTP "go.mercari.io/go-httpstats"
	"go.uber.org/zap"
)

var (
	// Version chocon version
	Version string
)

type cmdOpts struct {
	Listen           string `short:"l" long:"listen" default:"0.0.0.0" description:"address to bind"`
	Port             string `short:"p" long:"port" default:"3000" description:"Port number to bind"`
	LogDir           string `long:"access-log-dir" default:"" description:"directory to store logfiles"`
	LogRotate        int64  `long:"access-log-rotate" default:"30" description:"Number of day before remove logs"`
	Version          bool   `short:"v" long:"version" description:"Show version"`
	KeepaliveConns   int    `short:"c" default:"2" long:"keepalive-conns" description:"maximum keepalive connections for upstream"`
	MaxConnsPerHost  int    `long:"max-conns-per-host" default:"0" description:"maximum connections per host"`
	ReadTimeout      int    `long:"read-timeout" default:"30" description:"timeout of reading request"`
	WriteTimeout     int    `long:"write-timeout" default:"90" description:"timeout of writing response"`
	ProxyReadTimeout int    `long:"proxy-read-timeout" default:"60" description:"timeout of reading response from upstream"`
	Upstream         string `long:"upstream" default:"" description:"upstream server: http://upstream-server/"`
	StatsBufsize     int    `long:"stsize" default:"1000" description:"buffer size for http stats"`
	StatsSpfactor    int    `long:"spfactor" default:"3" description:"sampling factor for http stats"`
}

func addStatsHandler(h http.Handler, mw *statsHTTP.Metrics) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Index(r.URL.Path, "/.api/stats") == 0 {
			stats_api.Handler(w, r)
		} else if strings.Index(r.URL.Path, "/.api/http-stats") == 0 {
			d, err := mw.Data()
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			if err := json.NewEncoder(w).Encode(d); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
		} else {
			h.ServeHTTP(w, r)
		}
	})
}

func wrapLogHandler(h http.Handler, logDir string, logRotate int64, logger *zap.Logger) http.Handler {
	apacheLog, err := apachelog.New(`%h %l %u %t "%r" %>s %b "%v" %D %{X-Chocon-Req}i`)
	if err != nil {
		logger.Fatal("could not create apache logger", zap.Error(err))
	}

	if logDir == "stdout" {
		return apacheLog.Wrap(h, os.Stdout)
	} else if logDir == "" {
		return apacheLog.Wrap(h, os.Stderr)
	} else if logDir == "none" {
		return h
	}

	logFile := logDir
	linkName := logDir
	if !strings.HasSuffix(logDir, "/") {
		logFile += "/"
		linkName += "/"

	}
	logFile += "access_log.%Y%m%d%H%M"
	linkName += "current"

	rl, err := rotatelogs.New(
		logFile,
		rotatelogs.WithLinkName(linkName),
		rotatelogs.WithMaxAge(time.Duration(logRotate)*86400*time.Second),
		rotatelogs.WithRotationTime(time.Second*86400),
	)
	if err != nil {
		logger.Fatal("rotatelogs.New failed", zap.Error(err))
	}

	return apacheLog.Wrap(h, rl)
}

func wrapStatsHandler(h http.Handler, mw *statsHTTP.Metrics) http.Handler {
	return mw.WrapHandleFunc(h)
}

func makeTransport(keepaliveConns int, maxConnsPerHost int, proxyReadTimeout int) http.RoundTripper {
	t := &http.Transport{
		// inherited http.DefaultTransport
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// self-customized values
		MaxIdleConnsPerHost:   keepaliveConns,
		MaxConnsPerHost:       maxConnsPerHost,
		ResponseHeaderTimeout: time.Duration(proxyReadTimeout) * time.Second,
	}
	ticker := time.NewTicker(1 * time.Second)
	// defer ticker.Stop()
	var rlimit syscall.Rlimit
	go func() {
		for {
			select {
			case <-ticker.C:
				err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit)
				if err != nil {
					log.Fatal(err)
				}
				out, _ := exec.Command("/bin/sh", "-c", fmt.Sprintf("lsof -p %v", os.Getpid())).Output()
				// log.Printf("%v", err)
				lines := strings.Split(string(out), "\n")
				fd := len(lines) - 1
				log.Printf("rlimit = %v, open fd: %d", rlimit.Cur, fd)

				if fd > 40 {
					log.Printf("close idle connection")
					t.CloseIdleConnections()
				}
			}
		}
	}()
	return t
}

func printVersion() {
	fmt.Printf(`chocon %s
Compiler: %s %s
`,
		Version,
		runtime.Compiler,
		runtime.Version())
}

func main() {
	opts := cmdOpts{}
	psr := flags.NewParser(&opts, flags.Default)
	_, err := psr.Parse()
	if err != nil {
		os.Exit(1)
	}

	if opts.Version {
		printVersion()
		return
	}

	logger, _ := zap.NewProduction()
	upstream, err := upstream.New(opts.Upstream, logger)
	if err != nil {
		log.Fatal(err)
	}

	transport := makeTransport(opts.KeepaliveConns, opts.MaxConnsPerHost, opts.ProxyReadTimeout)
	var handler http.Handler = proxy.New(&transport, upstream, logger)

	statsChocon, err := statsHTTP.NewCapa(opts.StatsBufsize, opts.StatsSpfactor)
	if err != nil {
		log.Fatal(err)
	}
	handler = addStatsHandler(handler, statsChocon)
	handler = wrapLogHandler(handler, opts.LogDir, opts.LogRotate, logger)
	handler = wrapStatsHandler(handler, statsChocon)

	server := http.Server{
		Handler:      handler,
		ReadTimeout:  time.Duration(opts.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(opts.WriteTimeout) * time.Second,
	}

	l, err := ss.NewListener()
	if l == nil || err != nil {
		// Fallback if not running under Server::Starter
		l, err = net.Listen("tcp", fmt.Sprintf("%s:%s", opts.Listen, opts.Port))
		if err != nil {
			logger.Fatal("Failed to listen to port",
				zap.Error(err),
				zap.String("listen", opts.Listen),
				zap.String("port", opts.Port))
		}
	}
	server.Serve(l)
}
