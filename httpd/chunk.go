package httpd

import (
	"bufio"
	"errors"
	"io"
	"strconv"
)

// 解决http传输中的chunk编码问题
// 除了通过Content-Length通知对端报文主体的长度外，http1.1引入了新的编码方式——chunk编码(分块传输编码)，顾名思义就是会将报文主体分块后进行传输。
// 利用Content-Length存在一个问题：我们需要事先知道待发送的报文主体长度，而有些时候我们是希望数据边产生边发送，
// 根本无从知道将要发送多少的数据。因此http1.1相较http1.0除了长连接之外的另一大改进就是引入了chunk编码，客户端需要在请求头部设置Transfer-Encoding: chunked。
// HTTP/1.1 200 OK\r\n
// Content-Type: text/plain\r\n
// Transfer-Encoding: chunked\r\n
// \r\n

// # 以下为body
// 17\r\n							#chunk size
// hello, this is chunked \r\n		#chunk data
// D\r\n							#chunk size
// data sent by \r\n				#chunk data
// 7\r\n							#chunk size
// client!\r\n						#chunk data
// 0\r\n\r\n

// chunk编码格式可以概括为[chunk size][\r\n][chunk data][\r\n][chunk size][\r\n][chunk data][\r\n][chunk size=0][\r\n][\r\n]。

// 每一分块中包含两部分：

// 第一部分为chunk size，代表该块chunk data的长度，利用16进制表示。
// 第二部分为chunk data，该区域存储有效载荷，实际欲传输的数据存储在这部分。
// chunk size与chunk data之间都利用\r\n作为分割，通过0\r\n\r\n来标记报文主体的结束。

// 抽象出一个chunkReader结构体，当客户端利用chunk传输报文主体时，我们将Body设置成chunkReader即可。那么这个chunkReader需要满足什么功能呢？

// 依旧是满足上述Body的两点：规定Body读取的起始以及结尾。起始已经满足，重点考虑结尾的设计，我们这就不能使用LimitReader了，既然chunk编码的结束标志是0\r\n\r\n，那么我们的Read方法在碰到0\r\n\r\n时返回io.EOF错误即可，不允许继续向下读，因为后续的字节数据是属于下一个http请求。

// 如果仅做到将报文主体不多不少读出，但读取的数据包含chunk编码的控制信息(chunk size以及CRLF)，而我们只关心chunk data部分，还需要用户手动解码，这也是不可取的。

// 所以我们的chunkReader还需要具有解码chunk的功能，保证用户调用到的Read方法只读到有效载荷(chunk data)：hello, this is chunked data sent by client!。

type chunkReader struct {
	n int // 当前处理的块中还有多少字节未读
	bufr *bufio.Reader 

	done bool // 是否读取完成
	crlf [2]byte // 读取\r\n
}

func (cr *chunkReader) Read(p []byte) (n int,err error) {
	// 报文主体读取完后，不允许再读
	if cr.done {
		return 0,io.EOF
	}

	// 当前这一块读完了，读下一块
	if cr.n == 0 {
		cr.n,err = cr.getChunkSize()
		if err != nil {
			return
		}
	}

	if cr.n == 0 { // 获取到的chunkSize为0，说明读到了chunk报文结尾
		cr.done = true
		err = cr.discardCRLF()         //将最后的CRLF消费掉，防止影响下一个http报文的解析
		return
	}
	//如果当前块剩余的数据大于欲读取的长度
	if len(p) <= cr.n {
		n,err = cr.bufr.Read(p)
		cr.n -= n
		return n,err
	}

	//如果当前块剩余的数据不够欲读取的长度，将剩余的数据全部取出返回
	n, _ = io.ReadFull(cr.bufr, p[:cr.n])
	cr.n = 0
	//记得把每个chunkData后的\r\n消费掉
	if err = cr.discardCRLF(); err != nil {
		return
	}
	return 
}

func (cr *chunkReader) getChunkSize() (size int,err error) {
	line,err := readLine(cr.bufr)
	if err != nil {
		return
	}

	sizeInt64,err := strconv.ParseInt(string(line),10,64)
	if err != nil {
		return
	}
	size = int(sizeInt64)
	return 
}

func (cr *chunkReader) discardCRLF() (err error) {
	if _,err = io.ReadFull(cr.bufr,cr.crlf[:]);err == nil{
		if cr.crlf[0] != '\r' || cr.crlf[1] != '\n' {
			return errors.New("unsupported encoding format of chunk")
		}
	}
	return 
}