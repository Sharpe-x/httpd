package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	port := ":8001"
	args := os.Args
	if len(args) == 1 {
		fmt.Printf("Listening on http://0.0.0.0%s\n", port)
	} else {
		port = ":" + args[1]
		fmt.Printf("Listening on http://0.0.0.0%s\n", port)
	}

	m := http.NewServeMux()
	srv := &http.Server{
		Addr:         port,
		Handler:      m,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	m.HandleFunc("/time", timeHandler)
	m.HandleFunc("/", myHandler)

	panic(srv.ListenAndServe())

}

func timeHandler(w http.ResponseWriter, r *http.Request) {
	t := time.Now().Format(time.RFC1123)
	Body := "The current time is: "
	fmt.Fprintf(w, "<h1 align=\"center\">%s</h1>", Body)
	fmt.Fprintf(w, "<h2 align=\"center\"%s</h2>n", t)
	fmt.Fprintf(w, "Serving: %s\n", r.URL.Path)
	fmt.Printf("Served time for :%s\n", r.Host)
}

func myHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Serving: %s\n", r.URL.Path)
}
