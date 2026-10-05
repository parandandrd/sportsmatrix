//go:build tools
// +build tools

package tools

import (
	_ "github.com/go-bridget/twirp-swagger-gen/cmd/twirp-swagger-gen"
	_ "github.com/twitchtv/twirp/protoc-gen-twirp"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
