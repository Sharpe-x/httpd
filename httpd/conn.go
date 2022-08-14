package httpd

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"runtime"
)

// 负责http协议的解析

type conn struct {
	svr *Server  // 引用服务器对象
	rwc net.Conn // 底层tcp连接
	// 每一次写入都会进行一次系统调用、一次IO操作，势必会极大降低应用程序的性能。
	// 可以对用户写入数据进行缓存，缓存不下时再发送就能较少IO次数，从而提升效率。
	// bufio.Writer的底层会分配一个缓存切片，我们对bufio.Writer写入时会优先往这个切片中写入，
	// 如果缓存满了，则将切片中缓存的数据发送到最底层的writer中，因此可以保证每次写入的大小都是大于或等于缓存切片的大小
	bufr *bufio.Reader //带缓存的writer 读数据时直接操作bufr，bufr进而读取io.LimitedReader，进而读取tcp连接。
	// 对于HTTP协议来说，一个请求报文分为三部分：请求行、首部字段以及报文主体，一个post请求的报文如下：

	/*
		POST / HTTP/1.1\r\n						   	#请求行
		Content-Type: text/plain\r\n				#2~7行首部字段，首部字段为k-v对
		User-Agent: PostmanRuntime/7.28.0\r\n
		Host: 127.0.0.1:8080\r\n
		Accept-Encoding: gzip, deflate, br\r\n
		Connection: keep-alive\r\n
		Content-Length: 18\r\n
		\r\n
		hello,I am client!							#报文主体
	*/
	//首部字段部分是由一个个key-value对组成，每一对之间通过\r\n分割，
	// 首部字段与报文主体之间则是利用两个连续的CRLF即\r\n\r\n作为分界。
	// 首部字段到底有多少个key-value对于服务端程序来说是无法预知的，因此我们想正确解析出所有的首部字段，我们必须一直解析到出现两个连续的\r\n为止。

	// 对于一个正常的http请求报文，其首部字段总长度不会超过1MB，所以直接不加限制的读到空行完全可行，但问题是无法保证所有的客户端都没有恶意。
	//如果其发送了一个首部字段无限长的http请求，导致服务器无限解析最终用掉了所有内存直至程序崩溃。
	// 因此我们应该为我们的reader限制最大读取量，这是第一个改进，改进用到了标准库的io.LimitedReader。
	// 首部字段的每个key-value都占用一行(\r\n是换行符)，为了方便解析，我们的reader应该有ReadLine方法。这是第二个改进，改进用到了标准库的bufio.Reader。

	lr   *io.LimitedReader
	bufw *bufio.Writer// 是对lr 的封装 写数据时直接操作bufw，bufw进而写入到tcp连接。
}

func newConn(rwc net.Conn, svr *Server) *conn {
	lr := &io.LimitedReader{R: rwc, N: 1 << 20}
	return &conn{
		svr:  svr,
		rwc:  rwc,
		bufw: bufio.NewWriterSize(rwc, 4<<10), // 缓存大小4KB
		lr:   lr,                              // 为conn增加了lr字段，它是一个io.LimitedReader，它包含一个属性N代表能够在这个reader上读取的最多字节数，如果在此reader上读取的总字节数超过了上限，则接下来对这个reader的读取都会返回io.EOF，从而有效终止读取过程，避免首部字段的无限读。
		bufr: bufio.NewReaderSize(lr, 4<<10),  // 它是一个bufio.Reader，其底层的reader为上述的LimitedReader。对于一个io.Reader接口而言，它是无法提供ReadLine方法的，而将其封装程bufio.Reader后，就可以使用这个方法。
	}
}

func (c *conn) serve() {
	defer func() {
		if err := recover(); err != nil {
			log.Printf("panic recovered,err: %v\n", err)
			var trace [4096]byte
			n := runtime.Stack(trace[:], false)
			fmt.Printf("panic stack is %s:\n",string(trace[:n]))
		}
		c.close()
	}()

	for { //http1.1支持keep-alive长连接，所以一个连接中可能读出个请求，因此实用for循环读取
		// 对于HTTP 1.0来说，客户端为了获取服务端的每一个资源，都需要为每一个请求进行TCP连接的建立，
		// 因此每一个请求都需要等待2个RTT(三次握手+服务端的返回)的延时。而往往一个html网页中往往引用了多个css或者js文件，每一个请求都要经历TCP的三次握手，其带来的代价无疑是昂贵的。
		// 因此在HTTP 1.1中进行了巨大的改进，即如果将要请求的资源在同一台服务器上，则我只需要建立一个TCP连接，所有的HTTP请求都通过这个连接传输，平均下来可以减少一半的传播时延。
		//如果客户端的请求头中包含connection: keep-alive字段，则我们的服务器应该有义务保证长连接的维持，并持续从中读取HTTP请求，因此这里我们使用for循环。

		req, err := c.readRequest() //解析出Request
		if err != nil {
			handleError(err, c) // 将错误单独交给handleErr处理
			// readRequest可能会出现各种错误，如用户连接的断开、请求报文格式错误、服务器系统故障、使用了不支持的http版本、使用了不支持的协议等等错误。
			//对于有些错误如客户端连接断开或者使用了不支持的协议，我们服务端不应该进行回复。
			// 但对于一些错误如使用了不支持的http版本，我们应该返回505状态码；
			// 对于请求报文过大的错误，我们应该返回413状态码。因此在handleErr中，我们应该对err进行分类处理。
			// 我们这里只进行对err的打印
			return
		}

		res := c.setupResponse() // //设置response

		// 有了用户关心的Request和response之后，传入用户提供的回调函数即可
		c.svr.Handler.ServeHTTP(res, req)

		// 写入操作都将直接操纵bufw，其缓存的默认大小为4KB。
		// 在一个请求处理结束后，bufw的缓存切片中还缓存有部分数据，我们需要调用Flush保证数据全部发送。
		if err = c.bufw.Flush(); err != nil {
			return
		}
	}

}

func (c *conn) readRequest() (*Request, error) {
	return readRequest(c)
}

func (c *conn) setupResponse() *response {
	return setupResponse(c)
}

func (c *conn) close() {
	c.rwc.Close()
}

func handleError(err error, c *conn) {
	fmt.Println(err)
}
