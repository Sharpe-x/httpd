package httpd

// server.go只负责WEB服务器的启动逻辑

import "net"

type Handler interface {
	ServeHTTP(w ResponseWriter, r *Request)
}

// 启动一个服务器其必须项只有Addr以及Handler
// Server结构体中还可以加入很多字段如读取或写入超时时间、能接受的最大报文大小等控制信息，但为了专注于一个框架最核心的实现，我们忽略这些细节内容。
type Server struct {
	Addr    string  // 监听地址
	Handler Handler // 处理http请求的回调函数
}

// ListenAndServe方法中展现的是go语言socket编程的写法，
// 大致意思是在Addr上监听TCP连接，将得到的TCP连接rwc(ReadWriteCloser)以及s进行封装得到conn结构体。
// 接着调用conn.serve()方法，开启goroutine处理请求。
func (s *Server) ListenAndServe() error {
	l, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	for {
		rwc, err := l.Accept()
		if err != nil {
			continue // 其他连接还要继续
		}
		conn := newConn(rwc, s)
		go conn.serve()
	}

}
