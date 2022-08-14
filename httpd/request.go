// httpd 代表客户端的HTTP请求，由框架从字节流中解析http报文从而生成的结构。
package httpd

import (
	"bufio"
	"fmt"
	"io"
	"net/url"
	"strings"
)

// Request结构体就代表了客户端提交的http请求，我们使用readRequest函数从http连接上解析出这个对象
type Request struct {
	// 为Request结构体增加相应的属性，就应该从http请求报文出发，看看我们需要保存哪些信息。一段http请求报文：
	/*  GET /index?namPOSTe=gu HTTP/1.1\r\n			#请求行
	Content-Type: text/plain\r\n				#此处至报文主体为首部字段
	User-Agent: PostmanRuntime/7.28.0\r\n
	Host: 127.0.0.1:8080\r\n
	Accept-Encoding: gzip, deflate, br\r\n
	Connection: keep-alive\r\n
	Cookie: uuid=12314753; tid=1BDB9E9; HOME=1\r\n
	Content-Length: 18\r\n
	\r\n
	hello,I am client!							#报文主体
	*/

	// 请求行(第一行)分别由方法Method、请求路径URL以及协议版本Proto组成。将这三者加入到Request结构即可。
	Method string
	URL    *url.URL
	Proto  string

	// 首部字段由一个个键值对组成，我们的头部信息就存放在此处。Header存储
	Header Header

	// 报文主体部分，相较于前面两个更为复杂，可能具有不同的编码方式，长度也可能特别大。平时前端提交的form表单就放置在报文主体部分。仅只有POST和PUT请求允许携带报文主体。
	Body io.Reader // 用于读取报文主体

	// 像cookie以及queryString(如上面的URL中的?name=gufeijun)，是日常开发经常使用到的部分，为了方便用户的获取，我们分别用cookies以及queryString这两个map去保存解析后的字段
	// Request结构中的cookie以及queryString字段都是私有属性，
	// 因为只希望用户具有查询的权限，而不能够进行删除或者修改。为了让用户去查询这个私有字段，需要绑定相应的公共方法，这就是封装的思想。
	// 除了安全性之外，利用公有方法的方式也能让我们的控制更加灵活，可以实现懒加载(lazy load)，从而提升性能。
	// gin框架中每一个gin.Context中都会有一个叫做keys的map，用于在HandlerChain中传输数据，用户可以调用Set方法存数据，Get方法取数据。显而易见，为了实现这个功能，
	// 我们需要在操作keys这个map之前，就为其make分配内存。问题就出现在，如果我在生成一个gin.Context之初就为这个map进行初始化，但如果用户的Handler中并未使用这个功能怎么办？这个为keys初始化的时间是不是白白浪费了？
	//所以gin采用了比较高明的方式，在用户使用Set方法时，Set方法会先检测keys这个map是否为nil，如果为nil，这时我们才为其初始化。这样懒加载就能减少一些不必要的开销。

	cookies     map[string]string // 存储cookie
	queryString map[string]string // 存querySting

	RemoteAddr string // 客户端地址
	RequestURI string // 字符串形式的url
	conn       *conn  // 产生此request 的http连接
}

func readRequest(c *conn) (r *Request, err error) {
	r = new(Request)

	r.conn = c
	r.RemoteAddr = c.rwc.RemoteAddr().String()

	// 读取请求行
	line, err := readLine(c.bufr)
	if err != nil {
		return
	}

	// 按空格分割就得到了三个属性
	_, err = fmt.Sscanf(string(line), "%s%s%s", &r.Method, &r.RequestURI, &r.Proto)
	if err != nil {
		return
	}

	// 将字符串形式的uri 变成url.URL
	r.URL, err = url.ParseRequestURI(r.RequestURI)
	if err != nil {
		return
	}

	// 解析queryString
	r.parseQuery()
	// 读取header
	r.Header, err = readerHeader(c.bufr)
	if err != nil {
		return
	}

	const noLimit = (1 << 63) - 1
	r.conn.lr.N = noLimit // Body的读取无需进行读取字节数限制
	r.setupBody()         // 设置Body

	return
}

// bufio.Reader具有ReadLine方法，其存在三个返回参数line []byte, isPrefix bool, err error，line和err都很好理解，
// 但为什么还多出了一个isPrefix参数呢？这是因为ReadLine会借助到bufio.Reader的缓存切片
// 如果一行大小超过了缓存的大小，这也会无法达到读出一行的要求，这时isPrefix会设置成true，代表只读取了一部分。
func readLine(bufr *bufio.Reader) ([]byte, error) {
	p, isPrefix, err := bufr.ReadLine()
	if err != nil {
		return p, err
	}

	var l []byte
	for isPrefix {
		l, isPrefix, err = bufr.ReadLine()
		if err != nil {
			break
		}
		p = append(p, l...)
	}

	return p, err
}

func (r *Request) parseQuery() {
	// name=gu&token=1234
	r.queryString = parseQuery(r.URL.RawQuery)
}

func parseQuery(rawQuery string) map[string]string {
	parts := strings.Split(rawQuery, "&")
	queries := make(map[string]string, len(parts))
	for _, v := range parts {
		index := strings.IndexByte(v, '=')
		if index == -1 || index == len(v)-1 {
			continue
		}
		queries[strings.TrimSpace(v[:index])] = strings.TrimSpace(v[index+1:])
	}
	return queries
}

func readerHeader(bufr *bufio.Reader) (Header, error) {
	header := make(Header)

	for {
		line, err := readLine(bufr)
		if err != nil {
			return nil, err
		}

		//如果读到/r/n/r/n，代表报文首部的结束
		if len(line) == 0 {
			break
		}

		lineStr := string(line)
		index := strings.IndexByte(lineStr, ':')
		if index == -1 || index == len(lineStr)-1 {
			continue
		}
		//header.Add(lineStr[:index],strings.TrimSpace(lineStr[index+1:]))
		k, v := lineStr[:index], strings.TrimSpace(lineStr[index+1:])
		header[k] = append(header[k], v)
	}

	return header, nil
}

type eofReader struct{}

// 实现了io.Reader接口
func (e *eofReader) Read([]byte) (n int, err error) {
	return 0, io.EOF
}

func (r *Request) setupBody() {
	r.Body = new(eofReader)
}

/*我们给域名生成的cookie，一旦颁发给用户浏览器之后，浏览器在访问我们域名下的后端接口时都会在请求报文中将这个cookie带上，要是后端接口不关系客户端的cookie，而框架无脑全部提前解析，这就做了徒工。

所以也需要将Cookie的解析滞后，不是在readRequest中解析，而是在用户接口有需求，调用Cookie方法第一次查询时再进行解析。这就是为什么readRequest中没有解析cookie代码的原因。

接下来为Request绑定两个公有方法Query以及Cookie，分别用于查询queryString以及cookie：*/

func (r *Request) Query(name string) string {
	fmt.Println(r)
	return r.queryString[name]
}

func (r *Request) Cookie(name string) string {
	if r.cookies == nil {
		r.parseCookies()
	}
	return r.cookies[name]
}

func (r *Request) parseCookies() {
	if r.cookies != nil {
		return
	}

	r.cookies = make(map[string]string)
	rawCookies, ok := r.Header["Cookie"]
	if !ok {
		return
	}

	for _, cookie := range rawCookies {
		//example(line): uuid=12314753; tid=1BDB9E9; HOME=1
		kvs := strings.Split(strings.TrimSpace(cookie), ";")
		if len(kvs) == 1 && kvs[0] == "" {
			continue
		}

		for i := 0; i < len(kvs); i++ {
			index := strings.IndexByte(kvs[i], '=')
			if index == -1 {
				continue
			}
			r.cookies[strings.TrimSpace(kvs[i][:index])] = strings.TrimSpace(kvs[i][index+1:])
		}

	}
}
