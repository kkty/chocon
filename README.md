# chocon

**chocon** is a simple proxy server for persisting connections between upstream servers.

# Requirements

**chocon** requires Go1.11.3 or later.

# Installation

```
go get -u github.com/kazeburo/chocon
```

# Build

```
make bundle
make
```

# Run

```
chocon
```

# Usage

```
$ chocon -h
Usage:
chocon [OPTIONS]

Application Options:
-l, --listen=             address to bind (default: 0.0.0.0)
-p, --port=               Port number to bind (default: 3000)
--access-log-dir=     directory to store logfiles
--access-log-rotate=  Number of day before remove logs (default: 30)
-v, --version             Show version
--pid-file=           filename to store pid. disabled by default
-c, --keepalive-conns=    maximum keepalive connections for upstream (default: 2)
--max-conns-per-host= maximum connections per host (default: 0)
--read-timeout=       timeout of reading request (default: 30)
--write-timeout=      timeout of writing response (default: 90)
--proxy-read-timeout= timeout of reading response from upstream (default: 60)
--shutdown-timeout=   timeout to wait for all connections to be closed. (default: 1h)
--upstream=           upstream server: http://upstream-server/
--stsize=             buffer size for http stats (default: 1000)
--spfactor=           sampling factor for http stats (default: 3)
--insecure            disable certificate verifications (only for debugging)

Help Options:
-h, --help                Show this help message
```
