// SPDX-License-Identifier: Apache-2.0
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zyvorai/ota/internal/relay"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:8090", "listen address")
	dir := flag.String("cache", "/var/lib/zyvor-relay", "content-addressed cache directory")
	token := flag.String("token", "", "bearer token required on every request")
	origins := flag.String("origin-hosts", "", "comma-separated hosts this relay may fetch for a client-supplied artifact URL")
	flag.Parse()
	if *token == "" {
		log.Fatal("token required; peers are authenticated")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		log.Fatal(err)
	}
	var hosts []string
	for _, h := range strings.Split(*origins, ",") {
		h = strings.TrimSpace(h)
		if h != "" {
			hosts = append(hosts, h)
		}
	}
	s := &relay.Server{Dir: *dir, Token: *token, OriginHosts: hosts}
	srv := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
