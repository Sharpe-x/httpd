package httpd

type Header map[string][]string

func (h Header) Add(key,val string) {
	h[key] = append(h[key],val)
}

func (h Header) Set(key,val string) {
	h[key] = []string{val}
}

func (h Header) Get(key string) string {
	if val,ok := h[key];ok && len(val) > 0 {
		return val[0]
	}
	return ""
}

func(h Header) Del(key string) {
	delete(h, key)
}