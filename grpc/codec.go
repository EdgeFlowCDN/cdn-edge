package grpc

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/encoding"
)

const codecName = "json"

func init() {
	encoding.RegisterCodec(&JSONCodec{})
}

// JSONCodec is a gRPC codec that uses JSON encoding instead of protobuf.
type JSONCodec struct{}

func (c *JSONCodec) Name() string { return codecName }

func (c *JSONCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (c *JSONCodec) Unmarshal(data []byte, v interface{}) error {
	if data == nil {
		return nil
	}
	if bp, ok := v.(*[]byte); ok {
		*bp = append((*bp)[:0], data...)
		return nil
	}
	return json.Unmarshal(data, v)
}

func (c *JSONCodec) String() string {
	return fmt.Sprintf("json codec")
}
