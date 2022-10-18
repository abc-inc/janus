/*
Copyright © 2021 The janus authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/jessevdk/go-flags"
	"github.com/julienschmidt/httprouter"
	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var version = "unknown"

func main() {
	zerolog.DurationFieldInteger = true
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs
	if isatty.IsTerminal(os.Stdout.Fd()) {
		w := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05.000"}
		log.Logger = log.Output(w)
	}

	app := loadConfig(os.Args...)
	if listen, err := resolveIP(app.ListenAddress); err != nil {
		log.Fatal().Str("listen", app.ListenAddress).Err(err).Msg("Cannot resolve IP")
	} else {
		app.ListenAddress = listen
	}

	log.Info().
		Bool("enable-upload", app.EnableUpload).
		Str("listen", app.ListenAddress).
		Uint32("client-body-buffer-size", app.BufferSizeKB).
		Str("prefix", app.Prefix).
		Str("server-root", app.ServerRoot).
		Msg("Starting server")

	p := path.Join(app.Prefix, "/*path")
	h := logHandler(http.StripPrefix(strings.TrimRight(app.Prefix, "/"), handleRequest(app)))

	r := httprouter.New()
	r.Handler(http.MethodGet, p, h)
	r.Handler(http.MethodPost, p, h)

	s := &http.Server{
		Addr:              app.ListenAddress,
		Handler:           r,
		ReadHeaderTimeout: 30 * time.Second,
	}

	log.Fatal().Err(s.ListenAndServe()).Msg("Stopping server")
}

// app holds all application properties.
//
//nolint:lll
type app struct {
	BufferSizeKB  uint32 `short:"b" long:"client-body-buffer-size" description:"total number of kilobytes stored in memory (per upload)" default:"8"`
	ServerRoot    string `short:"d" long:"server-root" description:"root directory to serve" env:"JANUS_SERVER_ROOT" default:"."`
	ListenAddress string `short:"l" long:"listen" description:"host address and port to bind to" env:"JANUS_LISTEN" default:":8080"`
	Prefix        string `short:"p" long:"prefix" description:"prefix for the HTTP URLs" env:"JANUS_PREFIX" default:"/"`
	EnableUpload  bool   `short:"u" long:"enable-upload" description:"enable upload of files by adding \"?upload\"" env:"JANUS_ENABLE_UPLOAD"`
	Version       bool   `short:"v" long:"version" description:"print version information"`
}

// ctxKey is used for looking up Context values in Handlers.
type ctxKey int

const (
	logger ctxKey = iota
)

// ctxResponseWriter captures request time and HTTP status code.
type ctxResponseWriter struct {
	status int
	time   time.Time
	http.ResponseWriter
}

func (w *ctxResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// loadConfig parses the given command line arguments.
// If an argument is undefined, it takes environment variables into consideration.
func loadConfig(args ...string) (app app) {
	p := flags.NewParser(&app, flags.Default)
	if _, err := p.ParseArgs(args); err != nil {
		var fErr *flags.Error
		if errors.As(err, &fErr) && fErr.Type != flags.ErrHelp {
			p.WriteHelp(os.Stderr)
		}
		os.Exit(1)
	} else if app.Version {
		fmt.Println("janus version " + version)
		os.Exit(0)
	}

	if !strings.HasPrefix(app.Prefix, "/") {
		log.Warn().Str("prefix", app.Prefix).Msg("Prefix must begin with '/'")
		app.Prefix = "/" + app.Prefix
	}
	return
}

// resolveIP attempts to resolve the primary IP of the given bind address
// in the form "[iface_or_host]:port".
//
// If the first part is empty or not an interface, the input is returned.
// Otherwise ip:port is returned.
func resolveIP(listen string) (string, error) {
	hp := strings.SplitN(listen, ":", 3)
	if len(hp) != 2 {
		return "", errors.New("invalid listen address")
	}

	iface, err := net.InterfaceByName(hp[0])
	if err != nil {
		// assume it's an IP address
		return listen, nil
	}

	addrs, err := iface.Addrs()
	if err != nil {
		log.Fatal().Str("interface", hp[0]).Err(err).Msg("Cannot resolve IP")
	}

	for _, addr := range addrs {
		if ip := addr.(*net.IPNet).IP.To4(); ip != nil {
			log.Info().Stringer("IP", ip).Str("interface", hp[0]).Msg("Resolving IP for bind address")
			return ip.String() + ":" + hp[1], nil
		}
	}
	return "", errors.New("interface does not have an IPv4 address")
}

// handleRequest processes all requests and delegates them to other handlers.
func handleRequest(a app) http.HandlerFunc {
	upTmpl := template.Must(template.New("upload").Parse(`
<!DOCTYPE html>
<meta charset="UTF-8">
<title>Upload</title>
<form action="http://{{.}}" enctype="multipart/form-data" method="POST">
  <input type="file" name="file" />
  <input type="submit" value="Upload" />
</form>
`))

	upHandler := handleUploadPage(a, upTmpl)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate") // HTTP 1.1
		w.Header().Set("Pragma", "no-cache")                                   // HTTP 1.0
		w.Header().Set("Expires", "0")                                         // Proxies

		if a.EnableUpload {
			if r.Method == http.MethodPost {
				handleFileUpload(a).ServeHTTP(w, r)
				return
			} else if _, ok := r.URL.Query()["upload"]; ok {
				upHandler.ServeHTTP(w, r)
				return
			}
		}

		p := path.Join(a.ServerRoot, r.URL.Path)
		http.ServeFile(w, r, p)
	}
}

// logHandler enriches the Request Context with logging capabilities.
func logHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		crw := &ctxResponseWriter{http.StatusOK, time.Now(), w}
		l := log.Info()
		h.ServeHTTP(crw, r.WithContext(context.WithValue(r.Context(), logger, l)))

		l.
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", crw.status).
			Dur("ms", time.Since(crw.time)).
			Str("host", r.Host).
			Str("client", r.RemoteAddr).
			Msg("Request")
	})
}

// handleUploadPage renders the file upload page.
func handleUploadPage(a app, t *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := path.Join(a.ServerRoot, r.URL.Path)
		if stat, err := os.Stat(p); err != nil || !stat.IsDir() {
			http.Redirect(w, r, path.Dir(r.URL.Path)+"?"+r.URL.RawQuery, http.StatusTemporaryRedirect)
			return
		}

		if err := t.Execute(w, path.Join(r.Host, r.RequestURI)); err != nil {
			renderError(w, err, "upload page not available", http.StatusInternalServerError)
		}
	}
}

// handleFileUpload processes multipart/form-data file upload requests.
func handleFileUpload(a app) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(int64(a.BufferSizeKB * 1024)); err != nil {
			renderError(w, err, "cannot parse multipart form", http.StatusInternalServerError)
			return
		}

		f, h, err := r.FormFile("file")
		if err != nil {
			renderError(w, err, "invalid file", http.StatusBadRequest)
			return
		}

		// https://github.com/golang/go/issues/20253
		// mime/multipart: TempFile file hangs around on disk after usage in multipart/formdata.go
		defer func() {
			if err := f.Close(); err != nil {
				log.Warn().Str("name", h.Filename).Msg("cannot close upload temp file")
			} else if err := r.MultipartForm.RemoveAll(); err != nil {
				log.Warn().Str("name", h.Filename).Msg("cannot delete upload temp file")
			}
		}()

		p := filepath.Join(a.ServerRoot, r.URL.Path, h.Filename)
		newFile, err := os.Create(p)
		if err != nil {
			renderError(w, err, "cannot create destination file", http.StatusInternalServerError)
			return
		}
		defer newFile.Close()

		if _, err := io.Copy(newFile, f); err != nil || newFile.Close() != nil {
			_ = os.Remove(newFile.Name())
			renderError(w, err, "cannot write file", http.StatusInternalServerError)
			return
		}

		if e, ok := r.Context().Value(logger).(*zerolog.Event); ok {
			e.Str("name", h.Filename).Int64("size", h.Size)
		}
		_, _ = renderMsg(w, h.Filename+" uploaded successfully.\n")
	}
}

// renderError sets the HTTP status code and renders an error message.
func renderError(w http.ResponseWriter, err error, m string, status int) {
	log.Err(err).Msg(m)
	w.WriteHeader(status)
	_, _ = renderMsg(w, "Error: "+m+"\n")
}

func renderMsg(w io.Writer, m string) (n int, err error) {
	if n, err = w.Write([]byte(m)); err != nil {
		log.Err(err).Msg("cannot render message")
	}
	return
}
