package httpd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
)

const bufSize = 4096

type MultipartReader struct {
	// bufr是对Body的封装，方便我们预查看Body上的数据，从而确定part之间边界
	// 每个part共享这个bufr，但只有Body的读取指针指向哪个part的报文，
	// 哪个part才能在bufr上读取数据，此时其他part是无效的
	bufr *bufio.Reader
	// 记录bufr的读取过程中是否出现io.EOF错误，如果发生了这个错误，
	// 说明Body数据消费完毕，表单报文也消费完，不需要再产生下一个part
	occurEofErr          bool
	crlfDashBoundaryDash []byte  // \r\n--boundary--
	crlfDashBoundary     []byte  //\r\n--boundary，分隔符
	dashBoundary         []byte  //--boundary
	dashBoundaryDash     []byte  //--boundary--
	curPart              *Part   //当前解析到了哪个part
	crlf                 [2]byte //用于消费掉\r\n
}

type Part struct {
	Header Header // 存取当前part的首部
	mr     *MultipartReader
	// 下两者见前面的part报文
	formName string
	fileName string // 当该part传输文件时，fileName不为空
	closed   bool   // part是否关闭
	//如果它不为空，我们对Part的Read则优先交给substituteReader处理，主要是为了方便引入io.LimiteReader来凝练我们的代码。
	// substituteReader不为nil的时机，就是已经能够确定这个part还剩下多少数据可读了。
	substituteReader io.Reader // 替补Reader
	parsed           bool      // 是否已经解析过formName以及fileName
}

func (p *Part) Close() (err error) {
	if p.closed {
		return nil
	}
	_, err = io.Copy(ioutil.Discard, p)
	p.closed = true
	return err
}
func (p *Part) readHeader() (err error) {
	p.Header, err = readHeader(p.mr.bufr)
	return
}

func (p *Part) Read(buf []byte) (n int, err error) {
	// part已经关闭后，直接返回io.EOF错误
	if p.closed {
		return 0, io.EOF
	}
	//
	// 不为nil时，优先让substituteReader读取
	if p.substituteReader != nil {
		return p.substituteReader.Read(buf)
	}

	bufr := p.mr.bufr
	var peek []byte
	//如果已经出现EOF错误，说明Body没数据了，这时只需要关心bufr还剩余已缓存的数据
	if p.mr.occurEofErr {
		peek, _ = bufr.Peek(bufr.Buffered()) // 将最后缓存数据取出
	} else {
		//bufSize即bufr的缓存大小，强制触发Body的io，填满bufr缓存
		peek, err = bufr.Peek(bufSize)
		// //出现EOF错误，代表Body数据读完了，我们利用递归跳转到另一个if分支
		if err == io.EOF {
			p.mr.occurEofErr = true
			return p.Read(buf)
		}
		if err != nil {
			return 0, err
		}
	}

	//在peek出的数据中找boundary
	index := bytes.Index(peek, p.mr.crlfDashBoundary)
	//两种情况：
	//1.即||前的条件，index!=-1代表在peek出的数据中找到分隔符，也就代表顺利找到了该part的Read指针终点，
	//	给该part限制读取长度即可。
	//2.即||后的条件，在前文的multipart报文，是需要boudary来标识报文结尾，然后已经出现EOF错误,
	//  即在没有多余报文的情况下，还没有发现结尾标识，说明客户端没有将报文发送完整，就关闭了链接，
	//  这时让substituteReader = io.LimitReader(-1)，逻辑上等价于eofReader即可
	if index != -1 || (index == -1 && p.mr.occurEofErr) {
		p.substituteReader = io.LimitReader(bufr, int64(index))
		return p.substituteReader.Read(buf)
	}

	// //以下则是在peek出的数据中没有找到分隔符的情况，说明peek出的数据属于当前的part
	//  不能一次把所有的bufSize都当作消息主体读出，还需要减去分隔符的最长子串的长度。
	maxRead := bufSize - len(p.mr.crlfDashBoundary) + 1
	if maxRead > len(buf) {
		maxRead = len(buf)
	}
	return bufr.Read(buf[:maxRead])
}

// 获取FormName
func (p *Part) FormName() string {
	if !p.parsed {
		p.parseFormData()
	}
	return p.formName
}

// 获取FileName
func (p *Part) FileName() string {
	if !p.parsed {
		p.parseFormData()
	}
	return p.fileName
}

func (p *Part) parseFormData() {
	p.parsed = true
	cd := p.Header.Get("Content-Disposition")
	ss := strings.Split(cd, ";")
	if len(ss) == 1 || strings.ToLower(ss[0]) != "form-data" {
		return
	}
	for _, s := range ss {
		key, value := getKV(s)
		switch key {
		case "name":
			p.formName = value
		case "filename":
			p.fileName = value
		}
	}
}

func getKV(s string) (key string, value string) {
	ss := strings.Split(s, "=")
	if len(ss) != 2 {
		return
	}
	return strings.TrimSpace(ss[0]), strings.Trim(ss[1], `"`)
}

func NewMultipartReader(r io.Reader, boundary string) *MultipartReader {
	b := []byte("\r\n--" + boundary + "--")
	return &MultipartReader{
		bufr:                 bufio.NewReaderSize(r, bufSize), //将io.Reader封装成bufio.Reader
		crlfDashBoundaryDash: b,
		crlfDashBoundary:     b[:len(b)-2],
		dashBoundary:         b[2 : len(b)-2],
		dashBoundaryDash:     b[2:],
	}
}

func (mr *MultipartReader) NextPart() (p *Part, err error) {
	if mr.curPart != nil {
		// 将当前的Part关闭掉，即消费掉当前part数据，好让body的读取指针指向下一个part
		if err = mr.curPart.Close(); err != nil {
			return
		}
		if err = mr.discardCRLF(); err != nil {
			return
		}
	}

	// 下一行就是boundary 分割
	line, err := mr.readLine()
	if err != nil {
		return
	}

	// 到multipart报文的结尾了，直接返回
	if bytes.Equal(line, mr.dashBoundaryDash) {
		return nil, io.EOF
	}
	if !bytes.Equal(line, mr.dashBoundary) {
		err = fmt.Errorf("want delimiter %s, but got %s", mr.dashBoundary, line)
		return
	}

	// 这时Body已经指向了下一个part的报文
	p = new(Part)
	p.mr = mr

	// 要将part的首部信息预解析，好让part指向消息主体
	if err = p.readHeader(); err != nil {
		return
	}
	mr.curPart = p
	return
}

// 消费掉\r\n
func (mr *MultipartReader) discardCRLF() (err error) {
	if _, err = io.ReadFull(mr.bufr, mr.crlf[:]); err == nil {
		if mr.crlf[0] != '\r' && mr.crlf[1] != '\n' {
			err = fmt.Errorf("expect crlf, but got %s", mr.crlf)
		}
	}
	return
}

// 读一行
func (mr *MultipartReader) readLine() ([]byte, error) {
	return readLine(mr.bufr)
}
