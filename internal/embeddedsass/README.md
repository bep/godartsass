
* Install protobuf: https://github.com/protocolbuffers/protobuf
* Install the Go plugin: go get -u google.golang.org/protobuf/cmd/protoc-gen-go
* Download the correct version of the proto file: https://github.com/sass/embedded-protocol/blob/master/embedded_sass.proto
* protoc --go_out=. embedded_sass.proto
* Fix package name in embedded_sass.pb.go (TODO(bep)) 