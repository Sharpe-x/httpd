// httpd 代表客户端的HTTP请求，由框架从字节流中解析http报文从而生成的结构。
package httpd


// Request结构体就代表了客户端提交的http请求，我们使用readRequest函数从http连接上解析出这个对象
type Request struct {}


func readRequest(c *conn) (*Request,error) {
	return nil,nil
}
