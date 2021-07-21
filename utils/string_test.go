package utils

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestStringUtils(t *testing.T) {
	hostname := "127.0.0.1:8080"
	host := GetHostName(hostname)
	assert.Equal(t, host, "127.0.0.1")
	hostname = "127.0.0.1"
	host = GetHostName(hostname)
	assert.Equal(t, host, "127.0.0.1")
	hostname = ""
	host = GetHostName(hostname)
	assert.Equal(t, host, "")
}
