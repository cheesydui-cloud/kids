//go:build !linux

package server

import "fmt"

func replaceByExchange(dest, src string) error {
	return fmt.Errorf("当前系统不能交换正在运行的程序")
}
