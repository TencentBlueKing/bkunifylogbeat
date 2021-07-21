package utils

import (
	"strconv"
	"strings"
)

func GetHostName(host string) string {
	hostAndPort := strings.Split(host, ":")
	return hostAndPort[0]
}

func GetHostPort(host string) (int, error) {
	hostAndPort := strings.Split(host, ":")
	if len(hostAndPort) != 2 {
		return 0, nil
	}
	return strconv.Atoi(hostAndPort[1])
}
