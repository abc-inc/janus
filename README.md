# Janus

*Janus* is a no-configuration, single binary file server.

## Why *Janus*?

In ancient Roman religion, Janus (/ˈdʒeɪnəs/ JAY-nəs; Latin: IANVS (Iānus), pronounced [ˈjaːnʊs]) is the god of gates, transitions, doorways, passages, frames and more.
He is usually depicted as having two faces, since he looks to the future and to the past.

Therefore, it is the ideal name for a lightweight file server, which supports downloads and uploads.

## Installation

```shell script
go get -v github.com/abc-inc/janus
```

## Simple Usage & Directory Listing

*Janus* requires no configuration file.
All options are set via command line flags and/or environment variables.
A list of options can be displayed by invoking `janus -h`:

```
Usage:
  janus [OPTIONS]

Application Options:
  -b, --client-body-buffer-size= total number of kilobytes stored in memory (per upload) (default: 8)
  -d, --server-root=             root directory to serve (default: .) [$JANUS_SERVER_ROOT]
  -l, --listen=                  host address and port to bind to (default: :8080) [$JANUS_LISTEN]
  -p, --prefix=                  prefix for the HTTP URLs (default: /) [$JANUS_PREFIX]
  -u, --enable-upload            enable upload of files by adding "?upload" [$JANUS_ENABLE_UPLOAD]
  -v, --version                  print version information

Help Options:
  -h, --help           Show this help message
```

For example, the following command starts *Janus* serving the current directory (and restricts access to localhost only):

```shell script
janus -l localhost:8080
```

When accessing a file, *Janus* will stream its content.
Additionally, it supports directory listing e.g.,

```shell script
curl http://localhost:8080/
```

outputs

```html
<pre>
<a href=".gitignore">.gitignore</a>
<a href="LICENSE">LICENSE</a>
<a href="Makefile">Makefile</a>
<a href="NOTES.md">NOTES.md</a>
<a href="README.md">README.md</a>
<a href="go.mod">go.mod</a>
<a href="go.sum">go.sum</a>
<a href="main.go">main.go</a>
<a href="main_test.go">main_test.go</a>
</pre>
```

The `listen` argument also supports interface names in addition to IP addresses and hostnames.
The following example starts *Janus* listening on the IP of `eth0` at port `8081`:

```
janus -l eth0:8081
2020-11-01 12:34:56 INF Resolving IP for bind address IP=192.168.0.250 interface=eth0
2020-11-01 12:34:56 INF Starting server enable-upload=false listen=192.168.0.1:8081 prefix=/ server-root=.
```

## Upload

For security reasons file upload is disabled by default.
When enabled, `Janus` accepts HTTP POST requests (multipart/form-data) and stores the file in the directory where the URL is pointing to.

The following example starts `Janus` and configures it to take files sent to `http://<HOST>:8080/files` and saves them in the `uploads` directory.

```shell script
janus -d uploads -p /files -u
```

Files can be uploaded via the built-in web interface by appending `?upload` to a directory e.g., `http://192.168.0.250:8080/files/images?upload`, or by sending an HTTP POST request e.g.,

```shell script
curl -F file=@logo.png http://localhost:8080/files/images/
```

The uploaded file will be saved as `uploads/images/logo.png`.

## Alternatives

* https://github.com/syntaqx/serve

  *Janus* is heavily inspired by *serve*, which is an excellent file server with more features such as Basic authentication and TLS.
