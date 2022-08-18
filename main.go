package main

import (
	"bytes"
	"fmt"
	"httpd/httpd"
	"io"
	"io/ioutil"
	"log"
	"os"
)

type MyHandler struct{}

func (*MyHandler) ServeHTTP(w httpd.ResponseWriter, r *httpd.Request) {
	//fmt.Println("hello httpd")
	buff := &bytes.Buffer{}

	// 测试Request的解析
	fmt.Fprintf(buff, "[query]name=%s\n", r.Query("name"))
	fmt.Fprintf(buff, "[query]token=%s\n", r.Query("token"))
	fmt.Fprintf(buff, "[cookie]foo1=%s\n", r.Cookie("foo1"))
	fmt.Fprintf(buff, "[cookie]foo2=%s\n", r.Cookie("foo2"))
	fmt.Fprintf(buff, "[Header]User-Agent=%s\n", r.Header.Get("User-Agent"))
	fmt.Fprintf(buff, "[Header]Proto=%s\n", r.Proto)
	fmt.Fprintf(buff, "[Header]Method=%s\n", r.Method)
	fmt.Fprintf(buff, "[Addr]Addr=%s\n", r.RemoteAddr)
	fmt.Fprintf(buff, "[Request]%+v\n", r)

	//手动发送响应报文
	io.WriteString(w, "HTTP/1.1 200 OK\r\n")
	io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n", buff.Len()))
	io.WriteString(w, "\r\n")
	io.Copy(w, buff) //将buff缓存数据发送给客户端
}

type EchoHandler struct{}

func (*EchoHandler) ServeHTTP(w httpd.ResponseWriter, r *httpd.Request) {
	buf, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}

	const prefix = "you message:"
	io.WriteString(w, "HTTP/1.1 200 OK\r\n")
	io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n", len(buf)+len(prefix)))
	io.WriteString(w, "\r\n")
	io.WriteString(w, prefix)
	w.Write(buf)
}

type formHandler struct{}

func (*formHandler) ServeHTTP(w httpd.ResponseWriter, r *httpd.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		log.Println(err)
		return
	}

	var part *httpd.Part
label:
	for {
		part, err = mr.NextPart()
		if err != nil {
			break
		}
		switch part.FileName() {
		case "":
			fmt.Printf("FormName=%s,FormData:\n", part.FormName())
			if _, err = io.Copy(os.Stdout, part); err != nil {
				break label
			}
			fmt.Println()

		default:
			// 打印文件信息
			fmt.Printf("FormName=%s, FileName=%s\n", part.FormName(), part.FileName())
			var file *os.File
			if file, err = os.Create(part.FileName()); err != nil {
				break label
			}
			if _, err = io.Copy(file, part); err != nil {
				file.Close()
				break label
			}
			file.Close()
		}
	}
	if err != io.EOF {
		fmt.Println(err)
	}
	// 发送响应报文
	io.WriteString(w, "HTTP/1.1 200 OK\r\n")
	io.WriteString(w, fmt.Sprintf("Content-Length: %d\r\n", 0))
	io.WriteString(w, "\r\n")

}

func main() {
	svr := httpd.Server{
		Addr:    "127.0.0.1:8088",
		Handler: new(formHandler),
	}

	panic(svr.ListenAndServe())
}
