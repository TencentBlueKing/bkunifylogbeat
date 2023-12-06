package json

import (
	"github.com/TencentBlueKing/bkmonitor-datalink/pkg/libgse/output/gse"
	sonicjson "github.com/bytedance/sonic"
)

func init() {
	gse.MarshalFunc = sonicjson.Marshal
}

func Unmarshal(data []byte, v interface{}) error {
	return sonicjson.Unmarshal(data, v)
}

func Marshal(v interface{}) ([]byte, error) {
	return sonicjson.Marshal(v)
}
