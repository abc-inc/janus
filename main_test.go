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
	"bytes"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"testing"

	. "github.com/stretchr/testify/require"
	"golang.org/x/net/nettest"
)

func Test_ctxResponseWriter_WriteHeader(t *testing.T) {
	r := httptest.NewRecorder()
	w := ctxResponseWriter{ResponseWriter: r}
	w.WriteHeader(http.StatusTeapot)
	Equal(t, http.StatusTeapot, w.status)
	Equal(t, http.StatusTeapot, r.Code)
}

func Test_handleFileUpload(t *testing.T) {
	a := app{ServerRoot: ".", EnableUpload: false}
	h := handleFileUpload(a)
	HTTPBodyContains(t, h, http.MethodPost, "http://localhost/", nil, "")
}

func Test_handleRequest_FileUpload(t *testing.T) {
	// prepare request
	postData :=
		`--xxx
Content-Disposition: form-data; name="file"; filename="file"
Content-Type: text/plain

data
--xxx--
`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "http://localhost/", bytes.NewBufferString("test"))
	r.Header.Set("Content-Type", `multipart/form-data; boundary=xxx`)
	r.Body = ioutil.NopCloser(bytes.NewBufferString(postData))

	// initialize handler
	a := app{ServerRoot: "tmp", EnableUpload: true}
	h := logHandler(handleRequest(a))

	// take care of filesystem
	_ = os.MkdirAll("tmp", 0700)
	p := path.Join("tmp", "file")
	defer func() { _ = os.Remove(p) }()

	// do request
	h.ServeHTTP(w, r)
	Equal(t, "file uploaded successfully.\n", w.Body.String())

	// validate upload
	d, err := ioutil.ReadFile(p)
	NoError(t, err)
	Equal(t, "data", string(d))
}

func Test_handleUploadPage_UploadDisabled(t *testing.T) {
	a := app{ServerRoot: ".", EnableUpload: false}
	HTTPBodyContains(t, handleRequest(a), http.MethodGet, "http://localhost/",
		map[string][]string{"upload": {""}}, `<a href="main.go">main.go</a>`)
}

func Test_handleUploadPage_UploadEnabled(t *testing.T) {
	a := app{ServerRoot: ".", EnableUpload: true}
	exp := `<form action="http://localhost" enctype="multipart/form-data" method="POST">`
	HTTPBodyContains(t, handleRequest(a), http.MethodGet, "http://localhost/",
		map[string][]string{"upload": {""}}, exp)
}

func Test_loadConfigDefault(t *testing.T) {
	a := loadConfig()
	Equal(t, false, a.EnableUpload)
	Equal(t, ":8080", a.ListenAddress)
	Equal(t, "/", a.Prefix)
	Equal(t, ".", a.ServerRoot)
}

func Test_loadConfigParams(t *testing.T) {
	a := loadConfig("-d", "/tmp", "-l", "lo:8081", "-p", "test", "-u")
	Equal(t, true, a.EnableUpload)
	Equal(t, "lo:8081", a.ListenAddress)
	Equal(t, "/test", a.Prefix)
	Equal(t, "/tmp", a.ServerRoot)
}

func Test_resolveIP(t *testing.T) {
	iface, _ := nettest.LoopbackInterface()
	ips, _ := iface.Addrs()
	ip := ips[0].(*net.IPNet).IP.String()

	tests := []struct{ name, listen, exp, err string }{
		{"<empty>", "", "", "invalid listen address"},
		{":", ":", ":", ""},
		{"str", "3128", "", "invalid listen address"},
		{":port", ":3128", ":3128", ""},
		{"iface:", iface.Name + ":", ip + ":", ""},
		{"iface:port", iface.Name + ":3128", ip + ":3128", ""},
		{"host:port", "xxx:3128", "xxx:3128", ""},
		{"::", "::", "", "invalid listen address"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ip, err := resolveIP(test.listen)
			Equal(t, test.exp, ip)
			if err == nil {
				Empty(t, test.err)
			} else {
				Equal(t, test.err, err.Error())
			}
		})
	}
}

func Test_renderError(t *testing.T) {
	w := httptest.NewRecorder()
	renderError(w, io.ErrUnexpectedEOF, "test", http.StatusInternalServerError)
	Equal(t, http.StatusInternalServerError, w.Code)
}
