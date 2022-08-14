package main

import (
	"bytes"
	"fmt"
	"httpd/httpd"
	"io"
)

type myHandler struct {}

func (*myHandler) ServeHTTP(w httpd.ResponseWriter, r *httpd.Request) {
	//fmt.Println("hello httpd")
	buff := &bytes.Buffer{}

	// 测试Request的解析
	fmt.Fprintf(buff,"[query]name=%s\n",r.Query("name"))
	fmt.Fprintf(buff,"[query]token=%s\n",r.Query("token"))
	fmt.Fprintf(buff,"[cookie]foo1=%s\n",r.Cookie("foo1"))
	fmt.Fprintf(buff,"[cookie]foo2=%s\n",r.Cookie("foo2"))
	fmt.Fprintf(buff,"[Header]User-Agent=%s\n",r.Header.Get("User-Agent"))
	fmt.Fprintf(buff,"[Header]Proto=%s\n",r.Proto)
	fmt.Fprintf(buff,"[Header]Method=%s\n",r.Method)
	fmt.Fprintf(buff,"[Addr]Addr=%s\n",r.RemoteAddr)
	fmt.Fprintf(buff,"[Request]%+v\n",r)

	//手动发送响应报文
	io.WriteString(w, "HTTP/1.1 200 OK\r\n")
	io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n",buff.Len()))
	io.WriteString(w,"\r\n")
	io.Copy(w,buff)	//将buff缓存数据发送给客户端
}

func main() {
	svr := httpd.Server{
		Addr: "127.0.0.1:8088",
		Handler:new(myHandler),
	}

	panic(svr.ListenAndServe())
}
