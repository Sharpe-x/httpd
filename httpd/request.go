// httpd 代表客户端的HTTP请求，由框架从字节流中解析http报文从而生成的结构。
package httpd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"strconv"
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

	contentType string //
	boundary    string //
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
	r.Header, err = readHeader(c.bufr)
	if err != nil {
		return
	}

	const noLimit = (1 << 63) - 1
	r.conn.lr.N = noLimit // Body的读取无需进行读取字节数限制
	r.setupBody()         // 设置Body
	r.parseContentType()
	return
}

func (r *Request) parseContentType() {
	ct := r.Header.Get("Content-Type")

	index := strings.IndexByte(ct, ';')
	if index == -1 {
		r.contentType = ct
		return
	}

	if index == len(ct)-1 {
		return
	}

	ss := strings.Split(ct[index+1:], "=")
	if len(ss) < 2 || strings.TrimSpace(ss[0]) != "boundary" {
		return
	}
	r.contentType, r.boundary = ct[:index], strings.Trim(ss[1], `"`)
}

func (r *Request) MultipartReader() (*MultipartReader, error) {
	if r.boundary == "" {
		return nil,errors.New("no boundary detected")
	}
	return NewMultipartReader(r.Body, r.boundary),nil
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

func readHeader(bufr *bufio.Reader) (Header, error) {
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

// 为了提高性能我们将POST表单的解析权交给用户，为此我们给Request结构体封装一个Body字段，作为IO的接口。
// 报文主体就是用于携带客户端的额外信息，由于报文主体中能包含任何信息，更是不限长度，所以http协议就不能像首部字段一样以某个字符如CRLF为边界，来标记报文主体的范围。那么客户端是怎么保证服务端能够完整的不多不少的读出报文主体数据呢？
// 其实很简单，我们只要在首部字段中用一项标记报文主体长度，就解决了问题。就以上述报文为例，首部字段中包含一个Content-Length字段
// 除了使用Content-Length之外，http还可以使用chunk编码的方式
// http报文的头部部分很短，上一章中框架将这部分读取并解析后直接交给用户，既省时也省力。
// 但问题是http的报文主体是不限长度的，框架无脑将这些字节数据读出来，是很糟糕的设计。
// 最明智的做法是，将这个解析的主动权交给用户，框架只提供方便用户读取解析报文主体的接口而已。
// 并且不需要指定长度就能将报文主体不多不少读出

// 要保证Body达到我们期望的行为，这就意味着Body提供的Read方法能够保证以下两点：

// 对Body读取的这个指针一开始应该指向报文主体的开头，也就是说不能将报文主体前面的首部字段读出。规定了读取的起始。
// 多个http的请求相继在tcp连接上传输，当前http请求的Body就应该只能读取到当前请求的报文主体，即只能读取Content-Length长度的数据。规定了读取的结束。
// 如果单纯保证第一点，完全可以用上一文中conn结构体的bufr字段作为Body，因为我们已经将首部字段从bufr中读出，下一次对bufr的读取自然会从报文主体开始。
//但这样做，第二点就无法满足。在go语言中，对一个io.Reader的读取，如果返回io.EOF错误代表我们将这个Reader中的所有数据读取完了。
// ioutil.ReadAll就是利用了这个特点，如果不出现一些异常错误，它会不停的读取数据直至出现io.EOF。而一个网络连接net.Conn，只有在对端主动将连接关闭后，对net.Conn的Read才会返回io.EOF错误。
func (r *Request) setupBody() {

	if r.Method != "POST" && r.Method != "PUT" { // POST 和 PUT外的方法不允许设置包文主体
		r.Body = new(eofReader)
	} else if r.chunked() {
		r.Body = &chunkReader{
			bufr: r.conn.bufr,
		}
		// 为了防止资源的浪费，有些客户端在发送完http首部之后，发送body数据前，会先通过发送Expect: 100-continue查询服务端是否希望接受body数据，服务端只有回复了HTTP/1.1 100 Continue客户端才会再次发送body。因此我们也要处理这种情况
		r.fixExpectContinueReader()
	} else if cl := r.Header.Get("Content-Length"); cl != "" {
		contentLength, err := strconv.ParseInt(cl, 10, 64)
		if err != nil {
			r.Body = new(eofReader)
			return
		}
		// 允许Body最多读取contentLength的数据
		r.Body = io.LimitReader(r.conn.bufr, contentLength)
		r.fixExpectContinueReader()
	} else {
		r.Body = new(eofReader)
	}
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

func (r *Request) chunked() bool {
	te := r.Header.Get("Transfer-Encoding")
	return te == "chunked"
}

type expectContinueReader struct {
	wroteContinue bool // 是否已经发送过100 continue
	r             io.Reader
	w             *bufio.Writer
}

func (er *expectContinueReader) Read(p []byte) (n int, err error) {
	//第一次读取前发送100 continue
	// 一旦发现客户端的请求报文的首部中存在Expect: 100-continue，那么我们在第一次读取body时，也就意味希望接受报文主体，expectContinueReader会自动发送HTTP/1.1 100 Continue
	if !er.wroteContinue {
		er.w.WriteString("HTTP/1.1 100 Continue\r\n\r\n")
		er.w.Flush()
		er.wroteContinue = true
	}
	return er.r.Read(p)
}

func (r *Request) fixExpectContinueReader() {
	if r.Header.Get("Expect") != "100-continue" {
		return
	}
	r.Body = &expectContinueReader{
		r: r.Body,
		w: r.conn.bufw,
	}
}

// 如果用户在Handler的回调函数中没有去读取Body的数据，就意味着处理同一个socket连接上的下一个http报文时，
// Body未消费的数据会干扰下一个http报文的解析。所以我们的框架还需要在Handler结束后，将当前http请求的数据给消费掉。给Request增加一个finishRequest方法，以后的一些善尾工作都将交给它

func (r *Request) finishRequest() (err error) {
	//
	if err = r.conn.bufw.Flush(); err != nil {
		return
	}
	_, err = io.Copy(ioutil.Discard, r.Body)
	return err
}
