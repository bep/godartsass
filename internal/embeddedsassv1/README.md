
* Install protobuf: https://github.com/protocolbuffers/protobuf
* Install the Go plugin: go get -u google.golang.org/protobuf/cmd/protoc-gen-go
* protoc --go_opt=Membedded_sass_v1.proto=github.com/bep/godartsass/internal/embeddedsassv1 --go_opt=paths=source_relative --go_out=. embedded_sass_v1.proto
