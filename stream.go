package acomm

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	log "github.com/Sirupsen/logrus"
	logx "github.com/mistifyio/mistify-logrus-ext"
)

// NewStreamUnix sets up an ad-hoc unix listner to stream data.
func (t *Tracker) NewStreamUnix(src io.ReadCloser) (*url.URL, error) {
	socketPath, err := generateTempSocketPath()
	if err != nil {
		return nil, err
	}

	ul := NewUnixListener(socketPath)
	if err := ul.Start(); err != nil {
		return nil, err
	}

	t.dsLock.Lock()
	t.dataStreams[socketPath] = ul
	t.dsLock.Unlock()

	go func() {
		defer func() {
			_ = src.Close()

			t.dsLock.Lock()
			delete(t.dataStreams, socketPath)
			t.dsLock.Unlock()
		}()

		conn := ul.NextConn()
		if conn == nil {
			return
		}
		defer ul.Stop(time.Millisecond)
		defer ul.DoneConn(conn)

		if _, err := io.Copy(conn, src); err != nil {
			log.WithFields(log.Fields{
				"socketPath": socketPath,
				"error":      err,
			}).Error("failed to stream data")
			return
		}
	}()

	return ul.URL(), nil
}

// ProxyStreamHTTPURL generates the url for proxying streaming data from a unix
// socket.
func (t *Tracker) ProxyStreamHTTPURL(socketPath string) (*url.URL, error) {
	return url.ParseRequestURI(fmt.Sprintf(t.httpStreamURLFormat, socketPath))
}

// Stream streams data from a URL to a destination writer.
func Stream(dest io.Writer, addr *url.URL) error {
	if addr == nil {
		err := errors.New("missing addr")
		log.WithFields(log.Fields{
			"error": err,
		}).Error(err)
		return err
	}

	switch addr.Scheme {
	case "unix":
		return streamUnix(dest, addr)
	case "http", "https":
		return streamHTTP(dest, addr)
	default:
		err := errors.New("unknown url type")
		log.WithFields(log.Fields{
			"error": err,
			"type":  addr.Scheme,
			"addr":  addr,
		}).Error(err)
		return err
	}
}

// streamUnix streams data from a unix socket to a destination writer.
func streamUnix(dest io.Writer, addr *url.URL) error {
	conn, err := net.Dial("unix", addr.RequestURI())
	if err != nil {
		log.WithFields(log.Fields{
			"addr":  addr,
			"error": err,
		}).Error("failed to connect to stream socket")
		return err
	}
	defer logx.LogReturnedErr(conn.Close,
		log.Fields{"addr": addr},
		"failed to close stream connection",
	)

	if _, err := io.Copy(dest, conn); err != nil {
		log.WithFields(log.Fields{
			"addr":  addr,
			"error": err,
		}).Error("failed to stream data")
		return err
	}
	return nil
}

// streamHTTP streams data from an http connection to a destination writer.
func streamHTTP(dest io.Writer, addr *url.URL) error {
	httpResp, err := http.Get(addr.String())
	if err != nil {
		log.WithFields(log.Fields{
			"addr":  addr,
			"error": err,
		}).Error("failed to GET stream")
		return err
	}
	defer logx.LogReturnedErr(httpResp.Body.Close,
		log.Fields{"addr": addr},
		"failed to close stream response body",
	)

	if _, err := io.Copy(dest, httpResp.Body); err != nil {
		log.WithFields(log.Fields{
			"addr":  addr,
			"error": err,
		}).Error("failed to stream data")
		return err
	}
	return nil
}

// ProxyStreamHandler is an HTTP HandlerFunc for simple proxy streaming.
func (t *Tracker) ProxyStreamHandler(w http.ResponseWriter, r *http.Request) {
	addr, err := url.ParseRequestURI(r.URL.Query().Get("addr"))
	if err != nil {
		http.Error(w, "invalid addr", http.StatusBadRequest)
		return
	}

	if err := Stream(w, addr); err != nil {
		if opErr, ok := err.(*net.OpError); ok {
			// TODO: find out what the result is for not exist and return 404
			fmt.Printf("%+v\n", opErr)
		}
		http.Error(w, "failed to stream data", http.StatusInternalServerError)
		return
	}
}
