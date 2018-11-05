package main

/*
 * postfile.go
 * Saves the contents of post requests to files
 * By J. Stuart McMurray
 * Created 20160926
 * Last Modified 20160926
 */

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/fcgi"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
)

// LOCK locks the output directory, to avoid file clobbering
var LOCK = &sync.Mutex{}

func main() {
	var (
		plaintext = flag.Bool(
			"http",
			false,
			"Serve plaintext HTTP",
		)
		laddr = flag.String(
			"l",
			"0.0.0.0:4433",
			"Listen `address`",
		)
		cert = flag.String(
			"c",
			"cert.pem",
			"TLS `certificate` file",
		)
		key = flag.String(
			"k",
			"key.pem",
			"TLS `key` file",
		)
		dir = flag.String(
			"dir",
			"posts",
			"POSTed files `directory`",
		)
		serveFCGI = flag.Bool(
			"fcgi",
			false,
			"Serve FastCGI and take the listen address as a "+
				"path to a unix socket",
		)
	)
	flag.Usage = func() {
		fmt.Fprintf(
			os.Stderr,
			`Usage: %v [options]

Accepts POST requests via HTTPS (or plaintext HTTP with -http), and logs the
contents to a file named after the IP address and path.

Options:
`,
			os.Args[0],
		)
		flag.PrintDefaults()
	}
	flag.Parse()

	/* Get original cwd in case we have a relative socket */
	opwd, err := os.Getwd()
	if nil != err {
		log.Fatalf("Unable to get working directory: %v", err)
	}

	/* Be in the output directory */
	if err := os.MkdirAll(*dir, 0700); nil != err {
		log.Fatalf("Unable to make directory %q: %v", *dir, err)
	}
	if err := os.Chdir(*dir); nil != err {
		log.Fatalf("Unable to cd to %v: %v", *dir, err)
	}

	/* Add the one handler */
	http.HandleFunc("/", handle)

	/* Come up with a TLS or plaintext listener */
	var l net.Listener
	if *plaintext {
		l, err = net.Listen("tcp", *laddr)
	} else if *serveFCGI {
		/* If the path is relative, make it relative to the original
		working directory. */
		if !filepath.IsAbs(*laddr) {
			*laddr = filepath.Join(opwd, *laddr)
		}

		/* Listen on a unix socket for fcgi */
		l, err = net.Listen("unix", *laddr)
		if nil != err {
			log.Fatalf("Unable to listen: %v", err)
		}
		/* Make sure the socket is closed */
		if ul, ok := l.(*net.UnixListener); ok {
			ul.SetUnlinkOnClose(true)
		}
		/* Remove the socket when the progrm terminates, maybe */
		ch := make(chan os.Signal)
		go func() {
			s := <-ch
			if err := os.Remove(*laddr); nil != err {
				log.Fatalf(
					"Unable to remove socket after %v: %v",
					s,
					err,
				)
			}
			log.Fatalf("Caught %v and removed socket", s)
		}()
		signal.Notify(ch, os.Interrupt)
	} else {
		pair, err := tls.LoadX509KeyPair(*cert, *key)
		if nil != err {
			log.Fatalf(
				"Unable to load keypair from %v and %v: %v",
				*cert,
				*key,
				err,
			)
		}
		log.Printf("Loaded keypair from %v and %v", *cert, *key)
		/* Listen with TLS */
		l, err = tls.Listen("tcp", *laddr, &tls.Config{
			Certificates: []tls.Certificate{pair},
		})
	}
	if nil != err {
		log.Fatalf("Unable to listen on %v: %v", *laddr, err)
	}
	log.Printf("Listening for requests on %v", l.Addr())

	/* Handle FastCGI */
	if *serveFCGI {
		log.Fatalf("Error: %v", fcgi.Serve(l, nil))
	}

	/* Handle HTTPS calls */
	log.Fatalf("Error: %v", http.Serve(l, nil))
}

/* handle writes POST data to files */
func handle(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	/* Request string */
	rs := fmt.Sprintf(
		"[%v %v %v %v Host:%q UA:%q]",
		r.RemoteAddr,
		r.Method,
		r.URL,
		r.Proto,
		r.Host,
		r.Header.Get("User-Agent"),
	)

	/* Redirect non-POST requests to the requestor */
	if http.MethodPost != r.Method {
		log.Printf("%v Invalid method", rs)
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	/* Open the file for writing */
	f, err := openFile(r)
	if nil != err {
		log.Printf("%v Unable to open file: %v", rs, err)
		http.Error(w, "open", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	/* Copy data to file */
	n, err := io.Copy(f, r.Body)
	if nil != err {
		log.Printf(
			"%v Error after writing %v bytes to %q: %v",
			rs,
			n,
			f.Name(),
			err,
		)
		http.Error(w, "write", http.StatusInternalServerError)
		return
	}

	log.Printf("%v Wrote %v bytes to %q: %v", rs, n, f.Name(), err)

	/* Return the number of bytes written */
	fmt.Fprintf(w, "%v", n)
}

/* openFile opens a file for this request */
func openFile(r *http.Request) (*os.File, error) {
	LOCK.Lock()
	defer LOCK.Unlock()
	var (
		err  error
		name string
		num  int
	)

	/* Keep trying until we find a name */
	for name = makeName(r, num); nil == err ||
		!os.IsNotExist(err); name = makeName(r, num) {
		_, err = os.Stat(name)
		num++
	}

	/* Open the file */
	return os.OpenFile(
		name,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL,
		0600,
	)
}

/* makeName makes a name from the given request and number */
func makeName(r *http.Request, num int) string {
	return fmt.Sprintf(
		"%s_%s_%06v",
		r.RemoteAddr,
		strings.Replace(
			strings.TrimPrefix(
				filepath.Clean(r.URL.Path),
				"/",
			),
			"/",
			"_",
			-1,
		),
		num,
	)
}
