package main

import (
	"fmt"
	"httpd/httpd"
)

type myHandler struct {}

func (*myHandler) ServeHTTP(w httpd.ResponseWriter, r *httpd.Request) {
	fmt.Println("hello httpd")
}

func main() {
	svr := httpd.Server{
		Addr: "127.0.0.1:8088",
		Handler:new(myHandler),
	}

	panic(svr.ListenAndServe())
}