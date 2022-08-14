package httpd

// response结构体就代表服务端的响应对象
// 绑定些与客户端交互的方法，供用户使用
type response struct {
	c *conn
}

// 
type ResponseWriter interface {
	Write([]byte)(n int,err error)
}

func setupResponse(c *conn) *response {
	return &response{c:c}
}

func (w *response) Write(b []byte) (n int, err error) {
	return w.c.bufw.Write(b)
}

