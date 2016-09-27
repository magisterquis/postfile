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
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gorilla/handlers"
)

/* LOCK locks the output directory, to avoid file clobbering */
var LOCK = &sync.Mutex{}

func main() {
	var (
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
			"POST `directory`",
		)
	)
	flag.Usage = func() {
		fmt.Fprintf(
			os.Stderr,
			`Usage: %v [options]

Accepts POST requests via HTTPS, and logs the contents to a file named after
the IP address.

Options:
`,
			os.Args[0],
		)
		flag.PrintDefaults()
	}
	flag.Parse()

	/* Be in the output directory */
	if err := os.Chdir(*dir); nil != err {
		log.Fatalf("Unable to cd to %v: %v", *dir, err)
	}

	/* Add the one handler */
	http.Handle("/", handlers.CombinedLoggingHandler(
		os.Stdout,
		http.HandlerFunc(handle),
	))

	/* Load certificates */
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
	l, err := tls.Listen("tcp", *laddr, &tls.Config{
		Certificates: []tls.Certificate{pair},
	})
	if nil != err {
		log.Fatalf("Unable to listen with TLS on %v: %v", *laddr, err)
	}
	log.Printf("Listening for HTTPS connections on %v", l.Addr())

	/* Handle HTTPS calls */
	log.Fatalf("Unable to serve HTTPS: %v", http.Serve(l, nil))
}

/* handle writes POST data to files */
func handle(w http.ResponseWriter, r *http.Request) {
	/* Redirect non-POST requests to the requestor */
	if strings.ToLower("POST") != r.Method {
		http.Redirect(w, r, "https://127.0.0.1", http.StatusFound)
		return
	}
	/* Open the file for writing */
	f := openFile(r)
	defer f.Close()
	/* Write connection information to it */
	fmt.Fprintf(f, "POST %v %v\n", r.URL, r.Proto)
	r.Header.Write(f)
	fmt.Fprintf(f, "\n")
	io.Copy(f, r.Body)
}

/* openFile opens a file for this request */
func openFile(r *http.Request) *os.File {
	LOCK.Lock()
	defer LOCK.Unlock()
	var (
		err  error
		name string
		num  int
	)
	/* Keep trying until we find a name */
	for name = makeName(r, num); nil == err ||
		os.ErrNotExist != err; name = makeName(r, num) {
		_, err = os.Stat(name)
		num++
	}
	/* Open the file */
	f, err := os.OpenFile(
		name,
		os.O_WRONLY|os.O_APPEND|os.O_CREATE|os.O_EXCL,
		0600,
	)
	if nil != err {
		log.Fatalf("Unable to open %v: %v", name, err)
	}
	return f
}

/* makeName makes a name from the given request and number */
func makeName(r *http.Request, num int) string {
	return fmt.Sprintf("%v_%06v", r.RemoteAddr, num)
}
