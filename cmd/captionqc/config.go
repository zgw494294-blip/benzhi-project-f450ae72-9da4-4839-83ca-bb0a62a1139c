package main

import (
	"fmt"
	"net"
	"strings"
)

func validateAddr(addr string) error {
	if addr == "" {
		return fmt.Errorf("监听地址不能为空")
	}
	host, port, e := net.SplitHostPort(addr)
	if e != nil {
		return e
	}
	if host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("仅允许回环地址")
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("端口不能为空")
	}
	return nil
}
